package core

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go-press/core/content"
	"go-press/core/hook"
	coreMedia "go-press/core/media"
	"go-press/core/option"
	"go-press/core/taxonomy"
	"go-press/pkg/dbprefix"
	"go-press/pkg/logger"

	"github.com/BurntSushi/toml"
	"gorm.io/gorm"
)

// SeedData represents the declarative seed.toml structure.
type SeedData struct {
	// Admin is retained only so older seed files still parse. Demo imports must
	// never create or reset users; installer owns the initial administrator.
	Admin      SeedAdmin      `toml:"admin"`
	Settings   []SeedSetting  `toml:"settings"`
	Contents   []SeedContent  `toml:"contents"`
	Taxonomies []SeedTaxonomy `toml:"taxonomies"`
	// Categories and Tags are the legacy shorthand for the built-in category
	// and tag taxonomies. New seed files can use Taxonomies for any registry-
	// declared taxonomy without teaching core about a content domain.
	Categories []SeedTaxonomy `toml:"categories"`
	Tags       []SeedTaxonomy `toml:"tags"`
}

type SeedAdmin struct {
	Username    string `toml:"username"`
	Email       string `toml:"email"`
	Password    string `toml:"password"`
	DisplayName string `toml:"display_name"`
	Role        string `toml:"role"`
}

type SeedSetting struct {
	Key   string `toml:"key"`
	Value string `toml:"value"`
}

type SeedContent struct {
	Type        string `toml:"type"`
	Title       string `toml:"title"`
	Slug        string `toml:"slug"`
	Content     string `toml:"content"`
	Description string `toml:"description"`
	Excerpt     string `toml:"excerpt"`
	ImageURL    string `toml:"image_url"`
	SortOrder   int    `toml:"sort_order"`
	// Category and Tags are retained for backwards-compatible post/page seeds.
	// Taxonomies is the generic taxonomy-name → term-slugs mapping.
	Category   string              `toml:"category"`
	Tags       []string            `toml:"tags"`
	Taxonomies map[string][]string `toml:"taxonomies"`
	Meta       map[string]string   `toml:"meta"`
}

type SeedTaxonomy struct {
	Taxonomy string `toml:"taxonomy"`
	Name     string `toml:"name"`
	Slug     string `toml:"slug"`
}

type preparedSeedData struct {
	data         SeedData
	taxonomyDefs []SeedTaxonomy
}

// coreOptionKeys lists option keys that must survive a demo-data import.
var coreOptionKeys = []string{
	"active_theme", "site_name", "site_description", "site_language",
	"admin_language", "admin_email", "powered_by_gopress",
}

// ForceSeedFromFile clears existing content, taxonomy, media, and option data,
// then re-seeds from the given TOML file. It intentionally preserves users;
// installer owns administrator creation and credentials.
func (e *Engine) ForceSeedFromFile(path string) error {
	// Parse and validate the complete seed before reading or deleting any live
	// data. A malformed file, an undefined term, or an inactive extension must
	// leave the current site untouched.
	prepared, err := prepareSeedFile(path)
	if err != nil {
		return err
	}
	if err := validateSeedRegistry(prepared, e.Registry); err != nil {
		return fmt.Errorf("seed is incompatible with the active content registry: %w", err)
	}
	if e.DB == nil || e.Options == nil {
		return fmt.Errorf("seed storage is unavailable")
	}

	// Preserve core options before clearing.
	preserved := make(map[string]string)
	for _, key := range coreOptionKeys {
		if v := e.Options.Get(key); v != "" {
			preserved[key] = v
		}
	}
	// Also preserve demo_imported_* and plugin_active_* flags.
	allOpts := e.Options.All()
	for k, v := range allOpts {
		if strings.HasPrefix(k, "demo_imported_") || strings.HasPrefix(k, "plugin_active_") {
			preserved[k] = v
		}
	}

	logger.Info("Force-reseed requested — replacing existing data")
	if err := e.DB.Transaction(func(tx *gorm.DB) error {
		for _, table := range forceSeedClearTables() {
			if err := tx.Exec("DELETE FROM " + table).Error; err != nil {
				return fmt.Errorf("clear table %s: %w", table, err)
			}
		}
		if err := e.writeSeedData(tx, prepared); err != nil {
			return err
		}
		if err := restoreSeedOptions(tx, preserved); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	// Reload from DB so memory cache reflects the committed seed state.
	e.Options.LoadAll()
	e.emitSeedCompleted()
	logSeedCompleted(prepared)
	return nil
}

func forceSeedClearTables() []string {
	return []string{
		dbprefix.Table("term_relationships"),
		dbprefix.Table("content_meta"),
		dbprefix.Table("contents"),
		dbprefix.Table("taxonomies"),
		dbprefix.Table("terms"),
		dbprefix.Table("options"),
		dbprefix.Table("media"),
	}
}

// SeedFromFile reads a seed.toml file and populates the database.
func (e *Engine) SeedFromFile(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		logger.Info("No seed file found, skipping", "path", path)
		return nil
	} else if err != nil {
		return fmt.Errorf("stat seed file: %w", err)
	}

	// Check if already seeded.
	var contentCount int64
	if err := e.DB.Model(&content.Content{}).Count(&contentCount).Error; err != nil {
		return fmt.Errorf("count existing content before seed: %w", err)
	}
	if contentCount > 0 {
		logger.Info("Database already seeded, skipping")
		return nil
	}

	logger.Info("Seeding from file", "path", path)
	prepared, err := prepareSeedFile(path)
	if err != nil {
		return err
	}
	// First-run seeding happens before theme/plugin registration. When a runtime
	// registry is already available, enforce the same contract as demo imports.
	if seedRegistryReady(e.Registry) {
		if err := validateSeedRegistry(prepared, e.Registry); err != nil {
			return fmt.Errorf("seed is incompatible with the active content registry: %w", err)
		}
	}
	if err := e.DB.Transaction(func(tx *gorm.DB) error {
		return e.writeSeedData(tx, prepared)
	}); err != nil {
		return err
	}
	e.emitSeedCompleted()
	logSeedCompleted(prepared)
	return nil
}

func prepareSeedFile(path string) (*preparedSeedData, error) {
	var data SeedData
	if _, err := toml.DecodeFile(path, &data); err != nil {
		return nil, fmt.Errorf("failed to parse seed file: %w", err)
	}
	taxonomyDefs, err := normalizedSeedTaxonomies(data)
	if err != nil {
		return nil, fmt.Errorf("invalid seed taxonomies: %w", err)
	}
	if err := validateSeedContentTaxonomies(data.Contents, taxonomyDefs); err != nil {
		return nil, fmt.Errorf("invalid seed content taxonomies: %w", err)
	}
	return &preparedSeedData{data: data, taxonomyDefs: taxonomyDefs}, nil
}

func (e *Engine) writeSeedData(tx *gorm.DB, prepared *preparedSeedData) error {
	data := prepared.data
	taxonomyDefs := prepared.taxonomyDefs
	contentRepo := content.NewRepository(tx)
	taxonomyRepo := taxonomy.NewRepository(tx)
	mediaRepo := coreMedia.NewRepository(tx)
	downloadedImages := make(map[string]string)

	// 1. Settings → options
	if len(data.Settings) > 0 {
		opts := make([]option.Option, len(data.Settings))
		for i, s := range data.Settings {
			opts[i] = option.Option{Name: s.Key, Value: s.Value, Autoload: true}
		}
		if err := tx.Create(&opts).Error; err != nil {
			return fmt.Errorf("failed to seed settings: %w", err)
		}
	}

	// 2. Taxonomies. A Term slug is globally unique in the current schema, so a
	// term shared by multiple taxonomies reuses one Term row while each taxonomy
	// receives its own Taxonomy row.
	taxonomyIDs := make(map[string]uint, len(taxonomyDefs))
	termIDs := make(map[string]uint, len(taxonomyDefs))
	for _, def := range taxonomyDefs {
		termID, ok := termIDs[def.Slug]
		if !ok {
			term := taxonomy.Term{Name: def.Name, Slug: def.Slug}
			if err := tx.Create(&term).Error; err != nil {
				return fmt.Errorf("failed to create term %q: %w", def.Name, err)
			}
			termID = term.ID
			termIDs[def.Slug] = termID
		}
		tax := taxonomy.Taxonomy{TermID: termID, Taxonomy: def.Taxonomy}
		if err := tx.Create(&tax).Error; err != nil {
			return fmt.Errorf("failed to create taxonomy %q term %q: %w", def.Taxonomy, def.Name, err)
		}
		taxonomyIDs[seedTaxonomyKey(def.Taxonomy, def.Slug)] = tax.ID
	}

	// 3. Contents
	uploadDir := ""
	if e.Config != nil {
		uploadDir = e.Config.CMS.UploadDir
	}
	now := time.Now()
	for _, c := range data.Contents {
		// Use description or content field
		body := c.Content
		if body == "" {
			body = c.Description
		}

		// Download remote image to local uploads directory
		imageURL := c.ImageURL
		if strings.HasPrefix(imageURL, "http://") || strings.HasPrefix(imageURL, "https://") {
			remoteURL := imageURL
			if local, ok := downloadedImages[remoteURL]; ok {
				imageURL = local
			} else if local, err := downloadSeedImage(remoteURL, uploadDir, mediaRepo); err != nil {
				logger.Warn("Failed to download seed image, keeping remote URL",
					"url", remoteURL, "error", err)
			} else {
				imageURL = local
				downloadedImages[remoteURL] = local
			}
		}

		item := content.Content{
			Type:        c.Type,
			Status:      content.StatusPublished,
			Title:       c.Title,
			Slug:        c.Slug,
			Content:     body,
			Excerpt:     c.Excerpt,
			ImageURL:    imageURL,
			SortOrder:   c.SortOrder,
			PublishedAt: &now,
		}
		if err := tx.Create(&item).Error; err != nil {
			return fmt.Errorf("failed to create content %q: %w", c.Title, err)
		}

		// Save meta fields
		for k, v := range c.Meta {
			if err := contentRepo.SaveMeta(item.ID, k, v); err != nil {
				return fmt.Errorf("failed to save meta %q for content %q: %w", k, c.Title, err)
			}
		}

		// Link every declared taxonomy in one replacement operation. This avoids
		// taxonomy-specific branching and makes relationship ordering stable.
		refs := seedContentTaxonomyRefs(c)
		var relationshipIDs []uint
		for _, taxonomyName := range sortedSeedTaxonomyNames(refs) {
			for _, slug := range refs[taxonomyName] {
				relationshipIDs = append(relationshipIDs, taxonomyIDs[seedTaxonomyKey(taxonomyName, slug)])
			}
		}
		if len(relationshipIDs) > 0 {
			if err := taxonomyRepo.SetContentTaxonomies(item.ID, relationshipIDs); err != nil {
				return fmt.Errorf("failed to link taxonomies for content %q: %w", c.Title, err)
			}
		}
	}

	// Keep taxonomy counts consistent for home/category listings immediately
	// after import, regardless of the taxonomy names declared by extensions.
	for _, taxonomyName := range seedTaxonomyNames(taxonomyDefs) {
		if err := taxonomyRepo.UpdateCounts(taxonomyName); err != nil {
			return fmt.Errorf("failed to update taxonomy %q counts: %w", taxonomyName, err)
		}
	}

	return nil
}

func restoreSeedOptions(tx *gorm.DB, preserved map[string]string) error {
	for name, value := range preserved {
		var existing option.Option
		err := tx.Where("name = ?", name).First(&existing).Error
		switch {
		case err == nil:
			if err := tx.Model(&existing).Updates(map[string]interface{}{"value": value, "autoload": true}).Error; err != nil {
				return fmt.Errorf("restore option %q: %w", name, err)
			}
		case errors.Is(err, gorm.ErrRecordNotFound):
			if err := tx.Create(&option.Option{Name: name, Value: value, Autoload: true}).Error; err != nil {
				return fmt.Errorf("restore option %q: %w", name, err)
			}
		default:
			return fmt.Errorf("read option %q for restore: %w", name, err)
		}
	}
	return nil
}

func (e *Engine) emitSeedCompleted() {
	// Let plugins derive their own satellite data from the committed seed. Core
	// publishes the lifecycle event without knowing any extension-owned schema.
	if e.Hooks != nil {
		e.Hooks.DoAction(context.Background(), hook.SeedCompleted)
	}
}

func logSeedCompleted(prepared *preparedSeedData) {
	data := prepared.data
	logger.Info("Seeding completed",
		"settings", len(data.Settings),
		"categories", len(data.Categories),
		"tags", len(data.Tags),
		"taxonomies", len(prepared.taxonomyDefs),
		"contents", len(data.Contents),
	)
}

// normalizedSeedTaxonomies merges the generic declarations with the legacy
// categories/tags shorthands. Duplicate taxonomy+slug declarations collapse to
// one row. Since term slugs are globally unique, conflicting display names for
// the same slug are rejected before any database writes occur.
func normalizedSeedTaxonomies(data SeedData) ([]SeedTaxonomy, error) {
	defs := make([]SeedTaxonomy, 0, len(data.Taxonomies)+len(data.Categories)+len(data.Tags))
	defs = append(defs, data.Taxonomies...)
	for _, def := range data.Categories {
		def.Taxonomy = "category"
		defs = append(defs, def)
	}
	for _, def := range data.Tags {
		def.Taxonomy = "tag"
		defs = append(defs, def)
	}

	seenDefinitions := make(map[string]bool, len(defs))
	termNames := make(map[string]string, len(defs))
	normalized := make([]SeedTaxonomy, 0, len(defs))
	for _, def := range defs {
		def.Taxonomy = strings.TrimSpace(def.Taxonomy)
		def.Name = strings.TrimSpace(def.Name)
		def.Slug = strings.TrimSpace(def.Slug)
		if def.Taxonomy == "" || def.Name == "" || def.Slug == "" {
			return nil, fmt.Errorf("taxonomy, name, and slug are required: %+v", def)
		}
		if existingName, ok := termNames[def.Slug]; ok && existingName != def.Name {
			return nil, fmt.Errorf("term slug %q has conflicting names %q and %q", def.Slug, existingName, def.Name)
		}
		termNames[def.Slug] = def.Name
		key := seedTaxonomyKey(def.Taxonomy, def.Slug)
		if seenDefinitions[key] {
			continue
		}
		seenDefinitions[key] = true
		normalized = append(normalized, def)
	}
	return normalized, nil
}

func validateSeedContentTaxonomies(contents []SeedContent, definitions []SeedTaxonomy) error {
	available := make(map[string]bool, len(definitions))
	for _, def := range definitions {
		available[seedTaxonomyKey(def.Taxonomy, def.Slug)] = true
	}
	for _, item := range contents {
		for taxonomyName, slugs := range seedContentTaxonomyRefs(item) {
			for _, slug := range slugs {
				if !available[seedTaxonomyKey(taxonomyName, slug)] {
					return fmt.Errorf("content %q references undefined %s term %q", item.Title, taxonomyName, slug)
				}
			}
		}
	}
	return nil
}

func seedRegistryReady(registry *content.Registry) bool {
	return registry != nil && len(registry.AllTypes()) > 0
}

// validateSeedRegistry ensures a demo can only create content and taxonomy
// relationships exposed by the active runtime. This turns a disabled/missing
// extension into a clear preflight error instead of persisting orphan rows.
func validateSeedRegistry(prepared *preparedSeedData, registry *content.Registry) error {
	if prepared == nil {
		return fmt.Errorf("prepared seed is nil")
	}
	if !seedRegistryReady(registry) {
		return fmt.Errorf("active content registry is unavailable")
	}

	for _, definition := range prepared.taxonomyDefs {
		if registry.GetTaxonomy(definition.Taxonomy) == nil {
			return fmt.Errorf("taxonomy %q is not registered; activate the theme or module that provides it before importing", definition.Taxonomy)
		}
	}

	for _, item := range prepared.data.Contents {
		typeName := strings.TrimSpace(item.Type)
		typeDef := registry.GetType(typeName)
		if typeDef == nil {
			return fmt.Errorf("content type %q is not registered; activate the theme or module that provides it before importing", typeName)
		}
		for taxonomyName := range seedContentTaxonomyRefs(item) {
			taxonomyDef := registry.GetTaxonomy(taxonomyName)
			if taxonomyDef == nil {
				return fmt.Errorf("taxonomy %q used by content %q is not registered", taxonomyName, item.Title)
			}
			if !seedStringContains(typeDef.Taxonomies, taxonomyName) {
				return fmt.Errorf("content type %q does not declare taxonomy %q", typeName, taxonomyName)
			}
			if !seedStringContains(taxonomyDef.ContentTypes, typeName) {
				return fmt.Errorf("taxonomy %q does not allow content type %q", taxonomyName, typeName)
			}
		}
	}
	return nil
}

func seedStringContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// seedContentTaxonomyRefs returns a normalized, deduplicated copy so decoding a
// seed file never mutates caller-owned maps. Legacy category/tags fields merge
// into their generic equivalents.
func seedContentTaxonomyRefs(item SeedContent) map[string][]string {
	refs := make(map[string][]string, len(item.Taxonomies)+2)
	for taxonomyName, slugs := range item.Taxonomies {
		taxonomyName = strings.TrimSpace(taxonomyName)
		if taxonomyName == "" {
			continue
		}
		refs[taxonomyName] = appendUniqueSeedSlugs(refs[taxonomyName], slugs...)
	}
	if slug := strings.TrimSpace(item.Category); slug != "" {
		refs["category"] = appendUniqueSeedSlugs(refs["category"], slug)
	}
	refs["tag"] = appendUniqueSeedSlugs(refs["tag"], item.Tags...)
	if len(refs["tag"]) == 0 {
		delete(refs, "tag")
	}
	return refs
}

func appendUniqueSeedSlugs(existing []string, values ...string) []string {
	seen := make(map[string]bool, len(existing)+len(values))
	for _, value := range existing {
		seen[value] = true
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		existing = append(existing, value)
	}
	return existing
}

func sortedSeedTaxonomyNames(refs map[string][]string) []string {
	names := make([]string, 0, len(refs))
	for name := range refs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func seedTaxonomyNames(definitions []SeedTaxonomy) []string {
	seen := make(map[string]bool, len(definitions))
	var names []string
	for _, def := range definitions {
		if !seen[def.Taxonomy] {
			seen[def.Taxonomy] = true
			names = append(names, def.Taxonomy)
		}
	}
	sort.Strings(names)
	return names
}

func seedTaxonomyKey(taxonomyName, slug string) string {
	return taxonomyName + "\x00" + slug
}

// downloadSeedImage downloads a remote image to the local uploads directory,
// registers it in the media table, and returns the URL path (e.g. /static/uploads/demo/xxxx.jpg).
func downloadSeedImage(remoteURL, uploadDir string, mediaRepo *coreMedia.Repository) (string, error) {
	demoDir := filepath.Join(uploadDir, "demo")
	if err := os.MkdirAll(demoDir, 0755); err != nil {
		return "", fmt.Errorf("create demo dir: %w", err)
	}

	// Generate random filename
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	filename := fmt.Sprintf("%x.jpg", b)
	fullPath := filepath.Join(demoDir, filename)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(remoteURL)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	// Detect extension from Content-Type
	ct := resp.Header.Get("Content-Type")
	mimeType := "image/jpeg"
	switch {
	case strings.Contains(ct, "png"):
		filename = strings.TrimSuffix(filename, ".jpg") + ".png"
		mimeType = "image/png"
	case strings.Contains(ct, "webp"):
		filename = strings.TrimSuffix(filename, ".jpg") + ".webp"
		mimeType = "image/webp"
	case strings.Contains(ct, "gif"):
		filename = strings.TrimSuffix(filename, ".jpg") + ".gif"
		mimeType = "image/gif"
	}
	fullPath = filepath.Join(demoDir, filename)

	dst, err := os.Create(fullPath)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	written, err := io.Copy(dst, resp.Body)
	if err != nil {
		os.Remove(fullPath)
		return "", err
	}

	urlPath := fmt.Sprintf("/static/uploads/demo/%s", filename)

	// Register in media table
	if mediaRepo != nil {
		origName := filepath.Base(remoteURL)
		if idx := strings.Index(origName, "?"); idx > 0 {
			origName = origName[:idx]
		}
		m := &coreMedia.Media{
			Filename:     filename,
			OriginalName: origName,
			MimeType:     mimeType,
			Size:         written,
			Path:         urlPath,
			AltText:      "",
		}
		if err := mediaRepo.Create(m); err != nil {
			return "", fmt.Errorf("register demo image %q: %w", filename, err)
		}
	}

	logger.Info("Downloaded seed image", "url", remoteURL, "local", urlPath)
	return urlPath, nil
}
