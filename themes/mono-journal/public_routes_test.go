package monojournal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/0xmattg/go-press/core/comment"
	"github.com/0xmattg/go-press/core/user"

	"github.com/gin-gonic/gin"
)

type fakePublicComments struct{ created bool }

func (f *fakePublicComments) Create(_ context.Context, input comment.CreateInput) (*comment.Comment, error) {
	f.created = true
	return &comment.Comment{ID: 1, ContentID: input.ContentID, UserID: input.UserID, Status: comment.StatusPending}, nil
}

func (f *fakePublicComments) CountByUser(uint) (int64, error)                { return 0, nil }
func (f *fakePublicComments) RecentByUser(uint, int) ([]comment.View, error) { return nil, nil }

type fakePublicAuthorizer struct{ allowed bool }

func (f fakePublicAuthorizer) CanPublicUser(*gin.Context, string, string) bool { return f.allowed }

func TestCommentCreateRequiresLoginAndCapability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name       string
		account    *user.User
		allowed    bool
		wantStatus int
		wantCreate bool
	}{
		{name: "anonymous redirected", wantStatus: http.StatusSeeOther},
		{name: "authenticated without capability rejected", account: &user.User{ID: 2, Role: "limited", IsActive: true}, wantStatus: http.StatusForbidden},
		{name: "subscriber allowed", account: &user.User{ID: 3, Role: user.RoleSubscriber, IsActive: true}, allowed: true, wantStatus: http.StatusSeeOther, wantCreate: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			comments := &fakePublicComments{}
			handler := &Handler{comments: comments, authorizer: fakePublicAuthorizer{allowed: tc.allowed}}
			router := gin.New()
			router.POST("/comments", func(c *gin.Context) {
				if tc.account != nil {
					c.Set(user.CtxKeyPublicUser, tc.account)
				}
				handler.CommentCreate(c)
			})
			form := url.Values{"content_id": {"9"}, "body": {"A useful response"}, "return_to": {"/blog/post"}}
			req := httptest.NewRequest(http.MethodPost, "/comments", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.wantStatus)
			}
			if comments.created != tc.wantCreate {
				t.Fatalf("created = %v, want %v", comments.created, tc.wantCreate)
			}
		})
	}
}

func TestCommentCreateRejectsCrossOrigin(t *testing.T) {
	comments := &fakePublicComments{}
	handler := &Handler{comments: comments, authorizer: fakePublicAuthorizer{allowed: true}}
	router := gin.New()
	router.POST("/comments", func(c *gin.Context) {
		c.Set(user.CtxKeyPublicUser, &user.User{ID: 3, Role: user.RoleSubscriber, IsActive: true})
		handler.CommentCreate(c)
	})
	form := url.Values{"content_id": {"9"}, "body": {"A useful response"}, "return_to": {"/blog/post"}}
	req := httptest.NewRequest(http.MethodPost, "http://example.test/comments", strings.NewReader(form.Encode()))
	req.Host = "example.test"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://attacker.test")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden || comments.created {
		t.Fatalf("status=%d created=%v", recorder.Code, comments.created)
	}
}

func TestProfileRequiresLoginAndOwnProfileCapability(t *testing.T) {
	for _, tc := range []struct {
		name       string
		account    *user.User
		wantStatus int
	}{
		{name: "anonymous redirected", wantStatus: http.StatusSeeOther},
		{name: "permissionless account rejected", account: &user.User{ID: 4, Role: "limited", IsActive: true}, wantStatus: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := &Handler{authorizer: fakePublicAuthorizer{allowed: false}}
			router := gin.New()
			router.GET("/profile", func(c *gin.Context) {
				if tc.account != nil {
					c.Set(user.CtxKeyPublicUser, tc.account)
				}
				handler.Profile(c)
			})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/profile", nil))
			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.wantStatus)
			}
		})
	}
}
