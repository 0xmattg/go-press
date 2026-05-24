package admin

import (
	"crypto/rand"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go-press/config"
	"go-press/core/content"
	coreMedia "go-press/core/media"
	"go-press/core/option"
	"go-press/core/taxonomy"
	"go-press/core/user"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Service provides admin business logic using GoPress core subsystems.
type Service struct {
	db           *gorm.DB
	contentRepo  *content.Repository
	taxRepo      *taxonomy.Repository
	userRepo     *user.Repository
	mediaRepo    *coreMedia.Repository
	options      *option.Store
	auth         *user.Auth
	rbac         *user.RBAC
	siteName     string
	siteTimezone string
	config       config.CMSConfig
	registry     *content.Registry

	mediaVariantJobMu sync.Mutex
	mediaVariantJob   MediaVariantJob
}

// MediaVariantJob tracks the in-memory status of a media variant rebuild task.
type MediaVariantJob struct {
	Running    bool
	Completed  bool
	Force      bool
	OKCount    int
	FailCount  int
	Error      string
	StartedAt  time.Time
	FinishedAt time.Time
}

// NewService creates an admin Service.
func NewService(
	db *gorm.DB,
	contentRepo *content.Repository,
	taxRepo *taxonomy.Repository,
	userRepo *user.Repository,
	mediaRepo *coreMedia.Repository,
	options *option.Store,
	auth *user.Auth,
	rbac *user.RBAC,
	siteName string,
	siteTimezone string,
	cfg config.CMSConfig,
	registry *content.Registry,
) *Service {
	return &Service{
		db:           db,
		contentRepo:  contentRepo,
		taxRepo:      taxRepo,
		userRepo:     userRepo,
		mediaRepo:    mediaRepo,
		options:      options,
		auth:         auth,
		rbac:         rbac,
		siteName:     siteName,
		siteTimezone: strings.TrimSpace(siteTimezone),
		config:       cfg,
		registry:     registry,
	}
}

func (s *Service) SiteName() string {
	if s.options != nil {
		if name := strings.TrimSpace(s.options.Get("site_name")); name != "" {
			return name
		}
	}
	if name := strings.TrimSpace(s.siteName); name != "" {
		return name
	}
	return "GoPress"
}

// ==================== Auth ====================

func (s *Service) Login(username, password string) (*user.User, string, error) {
	token, u, err := s.auth.Login(username, password)
	if err != nil {
		return nil, "", fmt.Errorf("invalid username or password")
	}
	return u, token, nil
}

func (s *Service) CreateUser(username, email, password, displayName, role string) error {
	hash, err := user.HashPassword(password)
	if err != nil {
		return err
	}
	u := &user.User{
		Username:     username,
		Email:        email,
		PasswordHash: hash,
		DisplayName:  displayName,
		Role:         role,
		IsActive:     true,
	}
	return s.userRepo.Create(u)
}

func (s *Service) UpdateUserPassword(id uint, newPassword string) error {
	u, err := s.userRepo.FindByID(id)
	if err != nil {
		return err
	}
	hash, err := user.HashPassword(newPassword)
	if err != nil {
		return err
	}
	u.PasswordHash = hash
	return s.userRepo.Update(u)
}

// ==================== Content CRUD ====================

func (s *Service) ListContent(contentType, orderField, orderDir string) ([]content.Content, error) {
	return content.NewQuery(s.db).
		Type(contentType).
		OrderBy(orderField, orderDir).
		Get()
}

// ListContentScoped is like ListContent but also applies any request-scoped
// content filters registered via content.AddContentScope (e.g. the multilang
// plugin's language-based filter). When no scopes are present, the result is
// identical to ListContent.
func (s *Service) ListContentScoped(c *gin.Context, contentType, orderField, orderDir string) ([]content.Content, error) {
	db := content.ScopedDB(c, s.db)
	return content.NewQuery(db).
		Type(contentType).
		OrderBy(orderField, orderDir).
		Get()
}

func (s *Service) GetContent(id uint) (*content.Content, error) {
	return s.contentRepo.FindByID(id)
}

func (s *Service) CreateContent(c *content.Content) error {
	return s.contentRepo.Create(c)
}

func (s *Service) UpdateContent(c *content.Content) error {
	return s.contentRepo.Update(c)
}

func (s *Service) DeleteContent(id uint) error {
	return s.contentRepo.Delete(id)
}

// ReorderContent assigns sort_order = 1..N to the given IDs in sequence,
// inside a single transaction. The type filter protects against stray IDs
// from other content types accidentally hijacking sort slots.
func (s *Service) ReorderContent(contentType string, ids []uint) error {
	if contentType == "" || len(ids) == 0 {
		return nil
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		for i, id := range ids {
			if err := tx.Model(&content.Content{}).
				Where("id = ? AND type = ?", id, contentType).
				Update("sort_order", i+1).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ==================== Generic View Model Builders ====================

// ToDynamicContentView converts a content.Content to a DynamicContentView
// using the ContentTypeDef to load relevant meta fields and taxonomies.
func (s *Service) ToDynamicContentView(c content.Content, typeDef *content.ContentTypeDef) DynamicContentView {
	view := DynamicContentView{
		ID:          c.ID,
		Title:       c.Title,
		AuthorID:    c.AuthorID,
		AuthorName:  "-",
		Slug:        c.Slug,
		Content:     c.Content,
		Excerpt:     c.Excerpt,
		ImageURL:    c.ImageURL,
		Status:      c.Status,
		SortOrder:   c.SortOrder,
		PublishedAt: c.PublishedAt,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
		Meta:        make(map[string]string),
		Taxonomies:  make(map[string][]TaxonomyItemView),
	}

	// Load meta fields defined in the ContentTypeDef + gallery_images
	needsMeta := len(typeDef.MetaFields) > 0 || hasSupport(typeDef.Supports, "thumbnail")
	if needsMeta {
		meta, _ := s.contentRepo.GetMeta(c.ID)
		for _, mf := range typeDef.MetaFields {
			view.Meta[mf.Key] = meta[mf.Key]
		}
		// Always load gallery_images for types that support thumbnails
		if hasSupport(typeDef.Supports, "thumbnail") {
			view.Meta["gallery_images"] = meta["gallery_images"]
		}
	}

	// Load taxonomy data for each taxonomy associated with this type
	for _, taxName := range typeDef.Taxonomies {
		items, _ := s.taxRepo.GetContentTaxonomies(c.ID, taxName)
		taxViews := make([]TaxonomyItemView, len(items))
		for i, t := range items {
			taxViews[i] = TaxonomyItemView{ID: t.ID, Name: t.Term.Name, Slug: t.Term.Slug}
		}
		view.Taxonomies[taxName] = taxViews
	}

	return view
}

// ToDynamicContentViews batch-converts content items.
func (s *Service) ToDynamicContentViews(items []content.Content, typeDef *content.ContentTypeDef) []DynamicContentView {
	views := make([]DynamicContentView, len(items))
	authorIDs := make(map[uint]struct{})
	for i, c := range items {
		views[i] = s.ToDynamicContentView(c, typeDef)
		if c.AuthorID > 0 {
			authorIDs[c.AuthorID] = struct{}{}
		}
	}

	if len(authorIDs) == 0 {
		return views
	}

	ids := make([]uint, 0, len(authorIDs))
	for id := range authorIDs {
		ids = append(ids, id)
	}

	var users []user.User
	if err := s.db.Select("id", "username", "display_name").Where("id IN ?", ids).Find(&users).Error; err != nil {
		return views
	}

	authorNames := make(map[uint]string, len(users))
	for _, u := range users {
		name := strings.TrimSpace(u.DisplayName)
		if name == "" {
			name = strings.TrimSpace(u.Username)
		}
		if name == "" {
			name = "-"
		}
		authorNames[u.ID] = name
	}

	for i := range views {
		if views[i].AuthorID == 0 {
			continue
		}
		if name, ok := authorNames[views[i].AuthorID]; ok {
			views[i].AuthorName = name
		}
	}

	// Fallback: if author_id points to a missing user (e.g. old/stale account),
	// show creator username from audit logs rather than "-".
	unresolvedByContentID := make(map[uint]int)
	unresolvedContentIDs := make([]uint, 0)
	for i := range views {
		if views[i].AuthorID == 0 || views[i].AuthorName != "-" {
			continue
		}
		unresolvedByContentID[views[i].ID] = i
		unresolvedContentIDs = append(unresolvedContentIDs, views[i].ID)
	}
	if len(unresolvedContentIDs) > 0 {
		var logs []AuditLog
		s.db.Select("resource_id", "username", "created_at").
			Where("action = ? AND resource = ? AND resource_id IN ?", "create", typeDef.Name, unresolvedContentIDs).
			Order("created_at DESC").
			Find(&logs)

		for _, lg := range logs {
			idx, ok := unresolvedByContentID[lg.ResourceID]
			if !ok || strings.TrimSpace(lg.Username) == "" {
				continue
			}
			views[idx].AuthorName = strings.TrimSpace(lg.Username)
			delete(unresolvedByContentID, lg.ResourceID)
		}
	}

	return views
}

// ToTaxonomyItemViews converts taxonomy.Taxonomy slice to generic views.
func (s *Service) ToTaxonomyItemViews(items []taxonomy.Taxonomy) []TaxonomyItemView {
	views := make([]TaxonomyItemView, len(items))
	for i, t := range items {
		views[i] = TaxonomyItemView{ID: t.ID, Name: t.Term.Name, Slug: t.Term.Slug}
	}
	return views
}

// ==================== Taxonomy ====================

func (s *Service) ListTaxonomy(taxType string) ([]taxonomy.Taxonomy, error) {
	return s.taxRepo.ListByTaxonomy(taxType)
}

func (s *Service) CreateTaxonomyTerm(name, slug, taxType string) error {
	term := &taxonomy.Term{Name: name, Slug: slug}
	if err := s.taxRepo.CreateTerm(term); err != nil {
		return err
	}
	tax := &taxonomy.Taxonomy{TermID: term.ID, Taxonomy: taxType}
	return s.taxRepo.CreateTaxonomy(tax)
}

func (s *Service) UpdateTaxonomyTerm(taxID uint, name, slug string) error {
	tax, err := s.taxRepo.GetTaxonomy(taxID)
	if err != nil {
		return err
	}
	tax.Term.Name = name
	tax.Term.Slug = slug
	return s.db.Save(&tax.Term).Error
}

func (s *Service) DeleteTaxonomyTerm(taxID uint) error {
	return s.taxRepo.DeleteTaxonomy(taxID)
}

// ==================== Settings ====================

var settingLabelMap = map[string]string{
	"site_name":           "field.site_name",
	"site_description":    "field.site_description",
	"site_icon":           "field.site_icon",
	"site_language":       "field.site_language",
	"site_timezone":       "field.site_timezone",
	"powered_by_gopress":  "field.powered_by_gopress",
	"admin_language":      "field.admin_language",
	"active_theme":        "field.active_theme",
	"admin_email":         "field.admin_email",
	"footer_text":         "Footer Text",
	"company_name":        "Company Name",
	"company_description": "Company Description",
	"company_email":       "Company Email",
	"company_phone":       "Company Phone",
	"company_whatsapp":    "WhatsApp",
	"company_address":     "Company Address",
	"company_hours":       "Business Hours",
	"company_year":        "Company Year",
	"social_facebook":     "Facebook Link",
	"social_x":            "X / Twitter Link",
	"social_youtube":      "YouTube Link",
	"social_linkedin":     "LinkedIn Link",
	"social_wechat":       "WeChat Link or QR Code URL",
}

var settingDescriptionMap = map[string]string{
	"active_theme":       "help.active_theme",
	"admin_language":     "help.admin_language",
	"powered_by_gopress": "help.powered_by_gopress",
	"site_language":      "help.site_language",
	"site_timezone":      "help.site_timezone",
	"site_icon":          "help.site_icon",
	"demo_imported":      "help.demo_imported",
}

func (s *Service) settingDisplayLabel(key, lang string) string {
	if label, ok := settingLabelMap[key]; ok {
		return adminT(lang, label)
	}

	switch {
	case strings.HasPrefix(key, "demo_imported_"):
		themeSlug := strings.TrimPrefix(key, "demo_imported_")
		return adminT(lang, "field.demo_imported_status", s.humanizeSettingKey(themeSlug))
	case strings.HasPrefix(key, "plugin_active_"):
		pluginName := strings.TrimPrefix(key, "plugin_active_")
		return adminT(lang, "field.plugin_active_status", s.humanizeSettingKey(pluginName))
	default:
		return s.humanizeSettingKey(key)
	}
}

func (s *Service) settingDescription(key string) string {
	if desc, ok := settingDescriptionMap[key]; ok {
		return desc
	}
	if strings.HasPrefix(key, "demo_imported_") {
		return settingDescriptionMap["demo_imported"]
	}
	return ""
}

func (s *Service) isSettingReadOnly(key string) bool {
	if key == "active_theme" {
		return true
	}
	if strings.HasPrefix(key, "demo_imported_") {
		return true
	}
	return false
}

func (s *Service) humanizeSettingKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "Unnamed Setting"
	}

	normalized := strings.NewReplacer("_", " ", "-", " ").Replace(key)
	parts := strings.Fields(normalized)
	if len(parts) == 0 {
		return key
	}

	for i, p := range parts {
		lower := strings.ToLower(p)
		switch lower {
		case "id":
			parts[i] = "ID"
		case "url":
			parts[i] = "URL"
		case "api":
			parts[i] = "API"
		case "seo":
			parts[i] = "SEO"
		case "cms":
			parts[i] = "CMS"
		case "ip":
			parts[i] = "IP"
		case "db":
			parts[i] = "DB"
		case "cdn":
			parts[i] = "CDN"
		case "oauth":
			parts[i] = "OAuth"
		default:
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}

	return strings.Join(parts, " ")
}

// coreSettingKeys defines the options that appear on the site-wide Settings page.
// Theme-specific options (company_*, social_*, footer_*, home_*, etc.) belong in
// the theme settings page and are excluded here.
var coreSettingKeys = map[string]bool{
	"active_theme":       true,
	"site_name":          true,
	"site_description":   true,
	"site_icon":          true,
	"site_language":      true,
	"site_timezone":      true,
	"powered_by_gopress": true,
	"admin_language":     true,
	"admin_email":        true,
}

// isCoreOrSystemSetting returns true if the key should be shown on the
// site-wide Settings page (core keys + system-managed keys such as
// demo_imported_* and plugin_active_*).
func isCoreOrSystemSetting(key string) bool {
	if coreSettingKeys[key] {
		return true
	}
	if strings.HasPrefix(key, "demo_imported_") ||
		strings.HasPrefix(key, "plugin_active_") {
		return true
	}
	return false
}

func (s *Service) AdminLanguage() string {
	if s.options == nil {
		return defaultAdminLanguage
	}
	return normalizeAdminLanguage(s.options.GetDefault("admin_language", defaultAdminLanguage))
}

func (s *Service) GetAllSettings(lang string) []SettingItemView {
	lang = normalizeAdminLanguage(lang)
	var opts []option.Option
	s.db.Order("name ASC").Find(&opts)

	// Ensure essential keys always appear even if missing from DB.
	essentialDefaults := map[string]string{
		"site_name":          "",
		"site_description":   "",
		"site_icon":          "",
		"site_language":      "",
		"site_timezone":      s.defaultSiteTimezone(),
		"powered_by_gopress": "1",
		"admin_language":     defaultAdminLanguage,
		"admin_email":        "",
	}
	existing := make(map[string]bool, len(opts))
	for _, o := range opts {
		existing[o.Name] = true
	}
	for key, value := range essentialDefaults {
		if !existing[key] {
			opts = append(opts, option.Option{Name: key, Value: value})
		}
	}

	var views []SettingItemView
	for _, o := range opts {
		if !isCoreOrSystemSetting(o.Name) {
			continue
		}
		desc := s.settingDescription(o.Name)
		if desc != "" {
			desc = adminT(lang, desc)
		}
		views = append(views, SettingItemView{
			Key:         o.Name,
			Label:       s.settingDisplayLabel(o.Name, lang),
			Description: desc,
			ReadOnly:    s.isSettingReadOnly(o.Name),
			Value:       o.Value,
			Group:       settingGroup(o.Name),
			InputType:   settingInputType(o.Name),
			Options:     settingOptions(o.Name),
		})
	}
	return views
}

func settingGroup(key string) string {
	switch key {
	case "admin_language", "admin_email":
		return "admin"
	}
	return "site"
}

func settingInputType(key string) string {
	if key == "admin_language" || key == "site_timezone" {
		return "select"
	}
	if key == "site_icon" {
		return "media"
	}
	if key == "powered_by_gopress" {
		return "checkbox"
	}
	return "text"
}

func settingOptions(key string) []SettingOptionView {
	if key == "admin_language" {
		return adminSupportedLanguages()
	}
	if key == "site_timezone" {
		return supportedTimezones()
	}
	return nil
}

func (s *Service) defaultSiteTimezone() string {
	if s == nil {
		return config.DefaultTimezoneName()
	}
	if tz := strings.TrimSpace(s.siteTimezone); tz != "" {
		return tz
	}
	return config.DefaultTimezoneName()
}

func (s *Service) SiteTimezone() string {
	if s == nil {
		return config.DefaultTimezoneName()
	}
	if s.options != nil {
		if tz := strings.TrimSpace(s.options.Get("site_timezone")); tz != "" && config.IsValidTimezone(tz) {
			return tz
		}
	}
	if tz := strings.TrimSpace(s.siteTimezone); tz != "" && config.IsValidTimezone(tz) {
		return tz
	}
	return config.DefaultTimezoneName()
}

func (s *Service) SiteLocation() *time.Location {
	loc, _ := config.LoadTimezone(s.SiteTimezone())
	return loc
}

func (s *Service) ParseAdminDateTimeInput(value string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02T15:04", value, s.SiteLocation())
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

func (s *Service) FormatAdminDateTime(t time.Time) string {
	return t.In(s.SiteLocation()).Format("2006-01-02 15:04")
}

func (s *Service) FormatAdminDateTimeInput(t time.Time) string {
	return t.In(s.SiteLocation()).Format("2006-01-02T15:04")
}

func (s *Service) UpdateSetting(key, value string) {
	s.options.Set(key, value)
}

func (s *Service) CreateSetting(key, value string) {
	s.options.Set(key, value)
}

// GetOption returns a single option value by key.
func (s *Service) GetOption(key string) string {
	return s.options.Get(key)
}

// GetAllOptions returns all options as a map.
func (s *Service) GetAllOptions() map[string]string {
	return s.options.All()
}

// ==================== Media ====================

func (s *Service) UploadFile(file *multipart.FileHeader, userID uint) (*coreMedia.Media, error) {
	maxSize := int64(s.config.UploadMaxSizeMB) * 1024 * 1024
	if file.Size > maxSize {
		return nil, fmt.Errorf("file size exceeds limit (%d MB)", s.config.UploadMaxSizeMB)
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	allowedExts := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
		".webp": true, ".svg": true, ".pdf": true, ".doc": true,
		".docx": true, ".xls": true, ".xlsx": true,
	}
	if !allowedExts[ext] {
		return nil, fmt.Errorf("unsupported file type: %s", ext)
	}

	filename := randomFilename(ext)
	dateDir := time.Now().Format("2006/01")
	uploadPath := filepath.Join(s.config.UploadDir, dateDir)
	if err := os.MkdirAll(uploadPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create upload directory: %w", err)
	}

	fullPath := filepath.Join(uploadPath, filename)
	src, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	dst, err := os.Create(fullPath)
	if err != nil {
		return nil, err
	}

	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return nil, err
	}
	if err := dst.Close(); err != nil {
		return nil, err
	}

	urlPath := fmt.Sprintf("/static/uploads/%s/%s", dateDir, filename)
	mimeType := detectMimeType(fullPath, file.Header.Get("Content-Type"))
	width, height := 0, 0
	if isResizableMime(mimeType) {
		if w, h, err := coreMedia.GetImageDimensions(fullPath); err == nil {
			width, height = w, h
		}
	}
	media := &coreMedia.Media{
		Filename:     filename,
		OriginalName: file.Filename,
		MimeType:     mimeType,
		Size:         file.Size,
		Path:         urlPath,
		Width:        width,
		Height:       height,
		UploadedBy:   userID,
	}
	if err := s.mediaRepo.Create(media); err != nil {
		os.Remove(fullPath)
		return nil, err
	}
	if width > 0 && height > 0 && isResizableMime(mimeType) {
		if err := s.generateAndStoreVariants(media, fullPath, true); err != nil {
			// Variants are an optimization layer. Keep the uploaded original even
			// when derivative generation fails, so content editing is not blocked.
			fmt.Printf("warning: failed to generate variants for %s: %v\n", media.Path, err)
		}
	}
	return media, nil
}

func (s *Service) DeleteMediaFile(id uint) error {
	media, err := s.mediaRepo.FindByID(id)
	if err != nil {
		return err
	}
	variants, _ := s.mediaRepo.ListVariants(id)
	// Convert URL path back to physical path using config.UploadDir
	// URL is /static/uploads/{dateDir}/{file}, strip prefix to get relative path
	if filePath, ok := s.mediaDiskPath(media.Path); ok {
		os.Remove(filePath)
	}
	for _, v := range variants {
		if path, ok := s.mediaDiskPath(v.Path); ok {
			os.Remove(path)
		}
	}
	_ = s.mediaRepo.DeleteVariants(id)
	return s.mediaRepo.Delete(id)
}

// RegenerateMediaVariants rebuilds responsive image derivatives for existing media.
func (s *Service) RegenerateMediaVariants(force bool) (int, int, error) {
	items, err := s.mediaRepo.ListAllImages()
	if err != nil {
		return 0, 0, err
	}
	okCount, failCount := 0, 0
	canGenerateWebP := coreMedia.WebPEncoderAvailable()
	for i := range items {
		m := &items[i]
		if !isResizableMime(m.MimeType) {
			continue
		}
		if !force {
			if variants, err := s.mediaRepo.ListVariants(m.ID); err == nil && mediaVariantsComplete(m, variants, canGenerateWebP) {
				continue
			}
		}
		fullPath, ok := s.mediaDiskPath(m.Path)
		if !ok {
			failCount++
			continue
		}
		if _, err := os.Stat(fullPath); err != nil {
			failCount++
			continue
		}
		if err := s.generateAndStoreVariants(m, fullPath, force); err != nil {
			failCount++
			continue
		}
		okCount++
	}
	return okCount, failCount, nil
}

// StartMediaVariantRegeneration starts a rebuild task in the background.
func (s *Service) StartMediaVariantRegeneration(force bool) (MediaVariantJob, bool) {
	s.mediaVariantJobMu.Lock()
	if s.mediaVariantJob.Running {
		job := s.mediaVariantJob
		s.mediaVariantJobMu.Unlock()
		return job, false
	}
	s.mediaVariantJob = MediaVariantJob{
		Running:   true,
		Force:     force,
		StartedAt: time.Now(),
	}
	job := s.mediaVariantJob
	s.mediaVariantJobMu.Unlock()

	go func() {
		okCount, failCount, err := s.RegenerateMediaVariants(force)
		s.mediaVariantJobMu.Lock()
		defer s.mediaVariantJobMu.Unlock()
		s.mediaVariantJob.Running = false
		s.mediaVariantJob.Completed = true
		s.mediaVariantJob.OKCount = okCount
		s.mediaVariantJob.FailCount = failCount
		s.mediaVariantJob.FinishedAt = time.Now()
		if err != nil {
			s.mediaVariantJob.Error = err.Error()
		} else {
			s.mediaVariantJob.Error = ""
		}
	}()

	return job, true
}

// MediaVariantJobStatus returns the latest media variant rebuild task status.
func (s *Service) MediaVariantJobStatus() MediaVariantJob {
	s.mediaVariantJobMu.Lock()
	defer s.mediaVariantJobMu.Unlock()
	return s.mediaVariantJob
}

func (s *Service) generateAndStoreVariants(media *coreMedia.Media, fullPath string, force bool) error {
	if force {
		variants, _ := s.mediaRepo.ListVariants(media.ID)
		for _, v := range variants {
			if path, ok := s.mediaDiskPath(v.Path); ok {
				os.Remove(path)
			}
		}
		_ = s.mediaRepo.DeleteVariants(media.ID)
	}
	if media.Width == 0 || media.Height == 0 {
		if w, h, err := coreMedia.GetImageDimensions(fullPath); err == nil {
			media.Width, media.Height = w, h
			_ = s.mediaRepo.UpdateDimensions(media.ID, w, h)
		}
	}
	variants, err := coreMedia.GenerateResponsiveVariants(fullPath, media.Path, media.ID)
	if err != nil {
		return err
	}
	for i := range variants {
		if err := s.mediaRepo.UpsertVariant(&variants[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) mediaDiskPath(publicPath string) (string, bool) {
	relPath := strings.TrimPrefix(publicPath, "/static/uploads/")
	if relPath == publicPath || relPath == "" {
		return "", false
	}
	base, err := filepath.Abs(s.config.UploadDir)
	if err != nil {
		return "", false
	}
	fullPath, err := filepath.Abs(filepath.Join(s.config.UploadDir, relPath))
	if err != nil {
		return "", false
	}
	if fullPath != base && !strings.HasPrefix(fullPath, base+string(os.PathSeparator)) {
		return "", false
	}
	return fullPath, true
}

func detectMimeType(path, fallback string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	}
	f, err := os.Open(path)
	if err != nil {
		if fallback != "" {
			return fallback
		}
		return "application/octet-stream"
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n > 0 {
		detected := http.DetectContentType(buf[:n])
		if detected != "application/octet-stream" {
			return detected
		}
	}
	if fallback != "" {
		return fallback
	}
	return "application/octet-stream"
}

func isResizableMime(mimeType string) bool {
	return mimeType == "image/jpeg" || mimeType == "image/png"
}

func mediaVariantsComplete(media *coreMedia.Media, variants []coreMedia.MediaVariant, requireWebP bool) bool {
	if len(variants) == 0 {
		return false
	}
	if !requireWebP {
		return true
	}
	if media == nil || media.Width <= 0 {
		return false
	}
	for _, v := range variants {
		if v.Format == "webp" && v.Name == "full" && v.Width >= media.Width {
			return true
		}
	}
	return false
}

// UpdateMediaMeta updates SEO metadata for a media item.
func (s *Service) UpdateMediaMeta(id uint, altText, title, caption string) error {
	return s.mediaRepo.UpdateMeta(id, altText, title, caption)
}

// ==================== Audit ====================

func (s *Service) LogAction(userID uint, username, action, resource string, resourceID uint, details, ip string) {
	entry := &AuditLog{
		UserID:     userID,
		Username:   username,
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Details:    details,
		IPAddress:  ip,
	}
	s.db.Create(entry)
}

func (s *Service) ListRecentAuditLogs(limit int) []AuditLog {
	var items []AuditLog
	s.db.Order("created_at DESC").Limit(limit).Find(&items)
	return items
}

// ==================== Dashboard ====================

func (s *Service) GetDashboardStats() *DashboardStats {
	stats := &DashboardStats{}

	allTypes := s.registry.AllTypes()
	sort.Slice(allTypes, func(i, j int) bool {
		if allTypes[i].MenuOrder != allTypes[j].MenuOrder {
			return allTypes[i].MenuOrder < allTypes[j].MenuOrder
		}
		return allTypes[i].Name < allTypes[j].Name
	})

	for _, typeDef := range allTypes {
		var count int64
		s.db.Model(&content.Content{}).Where("type = ? AND deleted_at IS NULL", typeDef.Name).Count(&count)
		stats.ContentStats = append(stats.ContentStats, ContentTypeStats{
			TypeDef: typeDef,
			Count:   count,
		})
	}

	s.db.Model(&user.User{}).Count(&stats.UserCount)
	s.db.Model(&coreMedia.Media{}).Count(&stats.MediaCount)

	return stats
}

// ==================== Users ====================

func (s *Service) GetRoles() []string {
	return []string{user.RoleSuperAdmin, user.RoleEditor, user.RoleAuthor, user.RoleContributor, user.RoleSubscriber}
}

// ==================== Helpers ====================

func randomFilename(ext string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x%s", b, ext)
}

// AdminSlug computes the admin URL slug for a content type or taxonomy name.
func AdminSlug(name string) string {
	slug := strings.ReplaceAll(name, "_", "-")
	if strings.HasSuffix(slug, "y") && !strings.HasSuffix(slug, "ey") {
		return slug[:len(slug)-1] + "ies"
	}
	if strings.HasSuffix(slug, "s") || strings.HasSuffix(slug, "x") ||
		strings.HasSuffix(slug, "ch") || strings.HasSuffix(slug, "sh") {
		return slug + "es"
	}
	return slug + "s"
}
