package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go-press/core/user"
)

func TestMenuCreateRejectsSubscriberBeforeMutation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	called := false
	h := &Handler{
		svc: &Service{rbac: user.NewRBAC()},
		menuCallbacks: &MenuCallbacks{
			CreateFn: func(name, location string) error {
				called = true
				return nil
			},
		},
	}

	form := url.Values{"name": {"Primary"}, "location": {"header"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/menus", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("admin_role", user.RoleSubscriber)

	h.MenuCreate(c)

	if called {
		t.Fatal("menu mutation ran without menu.update permission")
	}
	if !c.IsAborted() {
		t.Fatal("request should be aborted when permission is denied")
	}
}
