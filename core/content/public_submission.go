package content

import (
	"errors"
	"strings"
	"sync"
	"time"
	"unicode"

	"go-press/core/user"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var (
	ErrPublicSubmissionUnavailable = errors.New("public submission is unavailable for this content type")
	ErrPublicSubmissionForbidden   = errors.New("public submission permission denied")
	ErrPublicSubmissionInvalid     = errors.New("invalid public submission")
	ErrPublicSubmissionRateLimited = errors.New("public submission rate limit exceeded")
	ErrPublicSubmissionNotFound    = errors.New("public submission not found")
)

const (
	publicSubmissionTitleMax   = 240
	publicSubmissionBodyMax    = 100000
	publicSubmissionExcerptMax = 1000
	publicSubmissionPerMinute  = 3
	publicSubmissionPerDay     = 20
)

// PublicSubmissionInput contains user-authored fields accepted by the generic
// public authoring workflow. Author, type and editorial status are never read
// from an HTTP form; callers pass the authenticated user and configured type
// separately.
type PublicSubmissionInput struct {
	ContentType        string
	UserID             uint
	Title              string
	Slug               string
	Content            string
	Excerpt            string
	Meta               map[string]string
	PublishImmediately bool
}

// PublicSubmissionService enforces active-account, content-type policy, RBAC,
// ownership, editorial status and rate limits for theme-owned public forms.
type PublicSubmissionService struct {
	repo      *Repository
	users     *user.Repository
	registry  *Registry
	rbac      *user.RBAC
	now       func() time.Time
	userLocks [64]sync.Mutex
}

func NewPublicSubmissionService(repo *Repository, users *user.Repository, registry *Registry, rbac *user.RBAC) *PublicSubmissionService {
	return &PublicSubmissionService{repo: repo, users: users, registry: registry, rbac: rbac, now: time.Now}
}

func (s *PublicSubmissionService) CreateOwn(c *gin.Context, input PublicSubmissionInput) (*Content, error) {
	account, def, err := s.authorize(c, input.UserID, input.ContentType, "create")
	if err != nil {
		return nil, err
	}
	if err := validatePublicSubmission(input); err != nil {
		return nil, err
	}
	lock := &s.userLocks[account.ID%uint(len(s.userLocks))]
	lock.Lock()
	defer lock.Unlock()
	if err := s.checkRateLimit(account.ID, input.ContentType); err != nil {
		return nil, err
	}

	slug := publicSubmissionSlug(input.Slug)
	if slug == "" {
		slug = publicSubmissionSlug(input.Title)
	}
	if slug == "" {
		return nil, ErrPublicSubmissionInvalid
	}
	// Public submissions do not carry an extension-specific translation record,
	// so uniqueness must be global. Request-scoped uniqueness could otherwise
	// create duplicate canonical slugs when a scope hides an existing row.
	slug, err = s.repo.EnsureUniqueSlug(input.ContentType, slug, 0)
	if err != nil {
		return nil, err
	}

	status, publishedAt := publicSubmissionState(def.PublicSubmission.DefaultStatus, input.PublishImmediately, s.now().UTC())
	item := &Content{
		Type:          input.ContentType,
		Status:        status,
		Title:         strings.TrimSpace(input.Title),
		Slug:          slug,
		Content:       strings.TrimSpace(input.Content),
		Excerpt:       strings.TrimSpace(input.Excerpt),
		AuthorID:      account.ID,
		CommentStatus: "open",
		PublishedAt:   publishedAt,
	}
	if err := s.repo.CreateWithMeta(c.Request.Context(), item, input.Meta); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *PublicSubmissionService) UpdateOwn(c *gin.Context, id uint, input PublicSubmissionInput) (*Content, error) {
	account, def, err := s.authorize(c, input.UserID, input.ContentType, "update_own")
	if err != nil {
		return nil, err
	}
	if !def.PublicSubmission.AllowUpdateOwn {
		return nil, ErrPublicSubmissionForbidden
	}
	if err := validatePublicSubmission(input); err != nil {
		return nil, err
	}
	item, err := s.repo.FindByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPublicSubmissionNotFound
	}
	if err != nil {
		return nil, err
	}
	if item.Type != input.ContentType || item.AuthorID != account.ID || item.Status == StatusTrash {
		return nil, ErrPublicSubmissionNotFound
	}

	slug := publicSubmissionSlug(input.Slug)
	if slug == "" {
		slug = publicSubmissionSlug(input.Title)
	}
	if slug == "" {
		return nil, ErrPublicSubmissionInvalid
	}
	slug, err = s.repo.EnsureUniqueSlug(input.ContentType, slug, item.ID)
	if err != nil {
		return nil, err
	}
	item.Title = strings.TrimSpace(input.Title)
	item.Slug = slug
	item.Content = strings.TrimSpace(input.Content)
	item.Excerpt = strings.TrimSpace(input.Excerpt)
	item.Status, item.PublishedAt = publicSubmissionState(def.PublicSubmission.DefaultStatus, input.PublishImmediately, s.now().UTC())
	if err := s.repo.Update(item); err != nil {
		return nil, err
	}
	for key, value := range input.Meta {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if err := s.repo.SaveMeta(item.ID, key, value); err != nil {
			return nil, err
		}
	}
	return item, nil
}

func (s *PublicSubmissionService) TrashOwn(c *gin.Context, contentType string, userID, id uint) error {
	account, def, err := s.authorize(c, userID, contentType, "delete_own")
	if err != nil {
		return err
	}
	if !def.PublicSubmission.AllowDeleteOwn {
		return ErrPublicSubmissionForbidden
	}
	item, err := s.repo.FindByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrPublicSubmissionNotFound
	}
	if err != nil {
		return err
	}
	if item.Type != contentType || item.AuthorID != account.ID || item.Status == StatusTrash {
		return ErrPublicSubmissionNotFound
	}
	return s.repo.Trash(item.ID)
}

func (s *PublicSubmissionService) authorize(c *gin.Context, userID uint, contentType, action string) (*user.User, *ContentTypeDef, error) {
	requestAccount := user.CurrentUser(c)
	if s == nil || s.repo == nil || s.users == nil || s.registry == nil || s.rbac == nil || userID == 0 || requestAccount == nil || requestAccount.ID != userID || !requestAccount.IsActive {
		return nil, nil, ErrPublicSubmissionForbidden
	}
	def := s.registry.GetType(contentType)
	if def == nil || !def.PublicSubmission.Enabled {
		return nil, nil, ErrPublicSubmissionUnavailable
	}
	account, err := s.users.FindByID(userID)
	if err != nil || account == nil || !account.IsActive {
		return nil, nil, ErrPublicSubmissionForbidden
	}
	if !def.PublicSubmission.AllowsRole(account.Role) || !s.rbac.Can(account.Role, contentType, action) {
		return nil, nil, ErrPublicSubmissionForbidden
	}
	return account, def, nil
}

func (s *PublicSubmissionService) checkRateLimit(userID uint, contentType string) error {
	now := s.now()
	minute, err := s.repo.CountRecentByAuthorAndType(userID, contentType, now.Add(-time.Minute))
	if err != nil {
		return err
	}
	if minute >= publicSubmissionPerMinute {
		return ErrPublicSubmissionRateLimited
	}
	daily, err := s.repo.CountRecentByAuthorAndType(userID, contentType, now.Add(-24*time.Hour))
	if err != nil {
		return err
	}
	if daily >= publicSubmissionPerDay {
		return ErrPublicSubmissionRateLimited
	}
	return nil
}

func validatePublicSubmission(input PublicSubmissionInput) error {
	title := strings.TrimSpace(input.Title)
	body := strings.TrimSpace(input.Content)
	if title == "" || body == "" || len([]rune(title)) > publicSubmissionTitleMax || len([]rune(body)) > publicSubmissionBodyMax || len([]rune(strings.TrimSpace(input.Excerpt))) > publicSubmissionExcerptMax {
		return ErrPublicSubmissionInvalid
	}
	return nil
}

func publicSubmissionStatus(status string) string {
	if status == StatusDraft {
		return StatusDraft
	}
	return StatusPending
}

func publicSubmissionState(defaultStatus string, publishImmediately bool, now time.Time) (string, *time.Time) {
	if publishImmediately {
		published := now.UTC()
		return StatusPublished, &published
	}
	return publicSubmissionStatus(defaultStatus), nil
}

func publicSubmissionSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	dash := false
	count := 0
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
			count++
			if count >= 120 {
				break
			}
			continue
		}
		dash = true
	}
	return strings.Trim(b.String(), "-")
}
