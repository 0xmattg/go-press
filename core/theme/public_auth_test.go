package theme

import (
	"net/http/httptest"
	"reflect"
	"testing"

	"go-press/core/user"

	"github.com/gin-gonic/gin"
)

func TestCurrentUserTemplateHelperReturnsSafeView(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(user.CtxKeyPublicUser, &user.User{
		ID: 7, Username: "reader", PasswordHash: "must-not-leak", DisplayName: "Reader",
	})
	fn := CommonFuncMap()["currentUser"].(func(*gin.Context) *user.PublicUserView)
	view := fn(c)
	if view == nil || view.ID != 7 || view.DisplayName != "Reader" {
		t.Fatalf("currentUser view = %#v", view)
	}
	if _, ok := reflect.TypeOf(*view).FieldByName("PasswordHash"); ok {
		t.Fatal("template user view exposes PasswordHash")
	}
}
