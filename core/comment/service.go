package comment

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"go-press/core/content"
	"go-press/core/hook"
	"go-press/core/user"

	"gorm.io/gorm"
)

var (
	ErrInvalidBody       = errors.New("comment body must be between 2 and 4000 characters")
	ErrTargetUnavailable = errors.New("comment target is not a published comment-enabled content item")
	ErrCommentsClosed    = errors.New("comments are closed for this content item")
	ErrInvalidParent     = errors.New("parent comment does not belong to this content item")
	ErrReplyDepth        = errors.New("only one reply level is supported")
	ErrUserUnavailable   = errors.New("comment author is unavailable")
	ErrRateLimited       = errors.New("comment rate limit exceeded")
	ErrInvalidStatus     = errors.New("invalid comment status")
)

type CreateInput struct {
	ContentID uint
	UserID    uint
	ParentID  *uint
	Body      string
}

type serviceStore interface {
	Create(*Comment) error
	FindByID(uint) (*Comment, error)
	FindPublishedTarget(uint, time.Time) (*content.Content, error)
	FindActiveUser(uint) (*user.User, error)
	ListVisibleForContent(uint, uint) ([]Comment, error)
	CountApprovedForContent(uint) (int64, error)
	CountRecentByUser(uint, time.Time) (int64, error)
	CountByUser(uint) (int64, error)
	RecentByUser(uint, int) ([]Comment, error)
	AdminList(string, int, int) (*Page, error)
	UpdateStatus(uint, string) error
	DeleteByContentIDs(*gorm.DB, []uint) error
}

// Service owns comment validation and moderation independently from any theme.
type Service struct {
	repo      *Repository
	store     serviceStore
	registry  *content.Registry
	hooks     *hook.Bus
	now       func() time.Time
	minuteMax int64
	dailyMax  int64
	userLocks [64]sync.Mutex
}

func NewService(db *gorm.DB, hooks *hook.Bus, registry *content.Registry) *Service {
	repo := NewRepository(db)
	return &Service{
		repo: repo, store: repo, registry: registry, hooks: hooks,
		now: time.Now, minuteMax: 5, dailyMax: 100,
	}
}

func (s *Service) Repository() *Repository { return s.repo }

func (s *Service) Create(ctx context.Context, input CreateInput) (*Comment, error) {
	body := strings.TrimSpace(input.Body)
	length := utf8.RuneCountInString(body)
	if length < MinBodyRunes || length > MaxBodyRunes {
		return nil, ErrInvalidBody
	}
	if input.ContentID == 0 || input.UserID == 0 {
		return nil, ErrTargetUnavailable
	}

	now := s.now().UTC()
	target, err := s.store.FindPublishedTarget(input.ContentID, now)
	if err != nil {
		return nil, ErrTargetUnavailable
	}
	if s.registry == nil || !typeSupportsComments(s.registry.GetType(target.Type)) {
		return nil, ErrTargetUnavailable
	}
	if target.CommentStatus != "open" {
		return nil, ErrCommentsClosed
	}

	author, err := s.store.FindActiveUser(input.UserID)
	if err != nil {
		return nil, ErrUserUnavailable
	}

	if input.ParentID != nil {
		parent, err := s.store.FindByID(*input.ParentID)
		if err != nil || parent.ContentID != input.ContentID || parent.Status != StatusApproved {
			return nil, ErrInvalidParent
		}
		if parent.ParentID != nil {
			return nil, ErrReplyDepth
		}
	}

	lock := &s.userLocks[input.UserID%uint(len(s.userLocks))]
	lock.Lock()
	defer lock.Unlock()
	if limited, err := s.rateLimited(input.UserID, now); err != nil {
		return nil, err
	} else if limited {
		return nil, ErrRateLimited
	}

	item := &Comment{
		ContentID: input.ContentID,
		UserID:    input.UserID,
		ParentID:  input.ParentID,
		Body:      body,
		Status:    StatusPending,
	}
	if err := s.store.Create(item); err != nil {
		return nil, err
	}
	item.Author = *author
	item.Target = *target
	if s.hooks != nil {
		if ctx == nil {
			ctx = context.Background()
		}
		s.hooks.DoAction(ctx, hook.CommentCreated, item)
	}
	return item, nil
}

func typeSupportsComments(typeDef *content.ContentTypeDef) bool {
	if typeDef == nil {
		return false
	}
	for _, feature := range typeDef.Supports {
		if feature == "comments" {
			return true
		}
	}
	return false
}

func (s *Service) rateLimited(userID uint, now time.Time) (bool, error) {
	minuteCount, err := s.store.CountRecentByUser(userID, now.Add(-time.Minute))
	if err != nil {
		return false, err
	}
	if minuteCount >= s.minuteMax {
		return true, nil
	}
	dailyCount, err := s.store.CountRecentByUser(userID, now.Add(-24*time.Hour))
	if err != nil {
		return false, err
	}
	return dailyCount >= s.dailyMax, nil
}

func (s *Service) ListVisible(contentID, viewerID uint) ([]View, error) {
	items, err := s.store.ListVisibleForContent(contentID, viewerID)
	if err != nil {
		return nil, err
	}
	top := make([]View, 0, len(items))
	topIndex := make(map[uint]int)
	for _, item := range items {
		view := toView(item, viewerID)
		if item.ParentID == nil {
			topIndex[item.ID] = len(top)
			top = append(top, view)
		}
	}
	for _, item := range items {
		if item.ParentID == nil {
			continue
		}
		if idx, ok := topIndex[*item.ParentID]; ok {
			top[idx].Replies = append(top[idx].Replies, toView(item, viewerID))
		}
	}
	return top, nil
}

func (s *Service) CountApproved(contentID uint) (int64, error) {
	return s.store.CountApprovedForContent(contentID)
}

func (s *Service) CountByUser(userID uint) (int64, error) {
	return s.store.CountByUser(userID)
}

func (s *Service) RecentByUser(userID uint, limit int) ([]View, error) {
	items, err := s.store.RecentByUser(userID, limit)
	if err != nil {
		return nil, err
	}
	views := make([]View, 0, len(items))
	for _, item := range items {
		views = append(views, toView(item, userID))
	}
	return views, nil
}

func (s *Service) AdminList(status string, page, perPage int) (*Page, error) {
	return s.store.AdminList(status, page, perPage)
}

func (s *Service) Moderate(ctx context.Context, id uint, status string) (*Comment, error) {
	if !isStatus(status) {
		return nil, ErrInvalidStatus
	}
	item, err := s.store.FindByID(id)
	if err != nil {
		return nil, err
	}
	oldStatus := item.Status
	if oldStatus == status {
		return item, nil
	}
	if err := s.store.UpdateStatus(id, status); err != nil {
		return nil, err
	}
	item.Status = status
	if s.hooks != nil {
		if ctx == nil {
			ctx = context.Background()
		}
		s.hooks.DoAction(ctx, hook.CommentStatusChanged, item, oldStatus, status)
	}
	return item, nil
}

func (s *Service) DeleteByContentIDs(tx *gorm.DB, ids []uint) error {
	return s.store.DeleteByContentIDs(tx, ids)
}

func toView(item Comment, viewerID uint) View {
	displayName := strings.TrimSpace(item.Author.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(item.Author.Username)
	}
	if displayName == "" {
		displayName = "Deleted user"
	}
	return View{
		ID: item.ID, ContentID: item.ContentID, ParentID: item.ParentID,
		Body: item.Body, Status: item.Status, CreatedAt: item.CreatedAt,
		Author:     AuthorView{ID: item.Author.ID, Username: item.Author.Username, DisplayName: displayName, AvatarURL: item.Author.AvatarURL},
		IsOwn:      viewerID > 0 && item.UserID == viewerID,
		TargetType: item.Target.Type, TargetTitle: item.Target.Title, TargetSlug: item.Target.Slug,
	}
}
