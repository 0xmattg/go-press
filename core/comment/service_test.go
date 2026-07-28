package comment

import (
	"context"
	"errors"
	"testing"
	"time"

	"go-press/core/content"
	"go-press/core/hook"
	"go-press/core/user"

	"gorm.io/gorm"
)

type fakeStore struct {
	post     content.Content
	user     user.User
	comments []Comment
	nextID   uint
}

func (f *fakeStore) Create(item *Comment) error {
	f.nextID++
	item.ID = f.nextID
	item.CreatedAt = time.Now().UTC()
	f.comments = append(f.comments, *item)
	return nil
}

func (f *fakeStore) FindByID(id uint) (*Comment, error) {
	for i := range f.comments {
		if f.comments[i].ID == id {
			item := f.comments[i]
			item.Author = f.user
			item.Target = f.post
			return &item, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeStore) FindPublishedTarget(id uint, now time.Time) (*content.Content, error) {
	if f.post.ID != id || f.post.Status != content.StatusPublished || (f.post.PublishedAt != nil && f.post.PublishedAt.After(now)) {
		return nil, gorm.ErrRecordNotFound
	}
	item := f.post
	return &item, nil
}

func (f *fakeStore) FindActiveUser(id uint) (*user.User, error) {
	if f.user.ID != id || !f.user.IsActive {
		return nil, gorm.ErrRecordNotFound
	}
	item := f.user
	return &item, nil
}

func (f *fakeStore) ListVisibleForContent(contentID, viewerID uint) ([]Comment, error) {
	var out []Comment
	for _, item := range f.comments {
		if item.ContentID != contentID || (item.Status != StatusApproved && !(item.Status == StatusPending && item.UserID == viewerID)) {
			continue
		}
		item.Author = f.user
		item.Target = f.post
		out = append(out, item)
	}
	return out, nil
}

func (f *fakeStore) CountApprovedForContent(contentID uint) (int64, error) {
	var count int64
	for _, item := range f.comments {
		if item.ContentID == contentID && item.Status == StatusApproved {
			count++
		}
	}
	return count, nil
}

func (f *fakeStore) CountRecentByUser(userID uint, since time.Time) (int64, error) {
	var count int64
	for _, item := range f.comments {
		if item.UserID == userID && !item.CreatedAt.Before(since) {
			count++
		}
	}
	return count, nil
}

func (f *fakeStore) CountByUser(userID uint) (int64, error) {
	var count int64
	for _, item := range f.comments {
		if item.UserID == userID && (item.Status == StatusPending || item.Status == StatusApproved) {
			count++
		}
	}
	return count, nil
}

func (f *fakeStore) RecentByUser(userID uint, limit int) ([]Comment, error) {
	var out []Comment
	for _, item := range f.comments {
		if item.UserID == userID {
			item.Target = f.post
			out = append(out, item)
		}
	}
	return out, nil
}

func (f *fakeStore) AdminList(string, int, int) (*Page, error) { return &Page{}, nil }

func (f *fakeStore) UpdateStatus(id uint, status string) error {
	for i := range f.comments {
		if f.comments[i].ID == id {
			f.comments[i].Status = status
			return nil
		}
	}
	return gorm.ErrRecordNotFound
}

func (f *fakeStore) DeleteByContentIDs(*gorm.DB, []uint) error { return nil }

func newTestService() (*Service, *fakeStore) {
	now := time.Now().Add(-time.Minute).UTC()
	store := &fakeStore{
		post: content.Content{ID: 10, Type: "post", Status: content.StatusPublished, Title: "Post", Slug: "post", CommentStatus: "open", PublishedAt: &now},
		user: user.User{ID: 20, Username: "reader", DisplayName: "Reader", Role: user.RoleSubscriber, IsActive: true},
	}
	registry := content.NewRegistry()
	registry.RegisterType(content.ContentTypeDef{Name: "post", Supports: []string{"comments"}})
	service := &Service{store: store, registry: registry, hooks: hook.New(), now: time.Now, minuteMax: 5, dailyMax: 100}
	return service, store
}

func TestCreateAndListCommentVisibility(t *testing.T) {
	service, store := newTestService()
	item, err := service.Create(context.Background(), CreateInput{ContentID: store.post.ID, UserID: store.user.ID, Body: "A useful note."})
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != StatusPending {
		t.Fatalf("status = %q, want pending", item.Status)
	}
	public, err := service.ListVisible(store.post.ID, 0)
	if err != nil || len(public) != 0 {
		t.Fatalf("anonymous list = %+v, err=%v", public, err)
	}
	owner, err := service.ListVisible(store.post.ID, store.user.ID)
	if err != nil || len(owner) != 1 || !owner[0].IsOwn {
		t.Fatalf("owner list = %+v, err=%v", owner, err)
	}
	if _, err := service.Moderate(context.Background(), item.ID, StatusApproved); err != nil {
		t.Fatal(err)
	}
	public, err = service.ListVisible(store.post.ID, 0)
	if err != nil || len(public) != 1 {
		t.Fatalf("approved list = %+v, err=%v", public, err)
	}
}

func TestReplyMustBelongToPostAndStopsAtOneLevel(t *testing.T) {
	service, store := newTestService()
	top, err := service.Create(context.Background(), CreateInput{ContentID: store.post.ID, UserID: store.user.ID, Body: "Top-level comment"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Moderate(context.Background(), top.ID, StatusApproved); err != nil {
		t.Fatal(err)
	}
	reply, err := service.Create(context.Background(), CreateInput{ContentID: store.post.ID, UserID: store.user.ID, ParentID: &top.ID, Body: "Direct reply"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Moderate(context.Background(), reply.ID, StatusApproved); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(context.Background(), CreateInput{ContentID: store.post.ID, UserID: store.user.ID, ParentID: &reply.ID, Body: "Nested reply"}); !errors.Is(err, ErrReplyDepth) {
		t.Fatalf("nested reply error = %v, want ErrReplyDepth", err)
	}

	store.post.ID = 11
	if _, err := service.Create(context.Background(), CreateInput{ContentID: store.post.ID, UserID: store.user.ID, ParentID: &top.ID, Body: "Wrong post"}); !errors.Is(err, ErrInvalidParent) {
		t.Fatalf("cross-post reply error = %v, want ErrInvalidParent", err)
	}
}

func TestCreateRejectsClosedOrUnavailableTarget(t *testing.T) {
	service, store := newTestService()
	store.post.CommentStatus = "closed"
	if _, err := service.Create(context.Background(), CreateInput{ContentID: store.post.ID, UserID: store.user.ID, Body: "Closed post"}); !errors.Is(err, ErrCommentsClosed) {
		t.Fatalf("closed post error = %v", err)
	}
	store.post.CommentStatus = "open"
	store.post.Status = content.StatusDraft
	if _, err := service.Create(context.Background(), CreateInput{ContentID: store.post.ID, UserID: store.user.ID, Body: "Draft post"}); !errors.Is(err, ErrTargetUnavailable) {
		t.Fatalf("draft post error = %v", err)
	}
}

func TestCreateRejectsContentTypeWithoutCommentCapability(t *testing.T) {
	service, store := newTestService()
	service.registry = content.NewRegistry()
	service.registry.RegisterType(content.ContentTypeDef{Name: store.post.Type, Supports: []string{"title", "content"}})
	if _, err := service.Create(context.Background(), CreateInput{ContentID: store.post.ID, UserID: store.user.ID, Body: "Unsupported target"}); !errors.Is(err, ErrTargetUnavailable) {
		t.Fatalf("unsupported target error = %v", err)
	}
}

func TestCreateAppliesPerUserRateLimit(t *testing.T) {
	service, store := newTestService()
	service.minuteMax = 1
	if _, err := service.Create(context.Background(), CreateInput{ContentID: store.post.ID, UserID: store.user.ID, Body: "First comment"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(context.Background(), CreateInput{ContentID: store.post.ID, UserID: store.user.ID, Body: "Second comment"}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("rate limit error = %v", err)
	}
}
