package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go-press/core/user"

	"github.com/gin-gonic/gin"
)

func TestSettingUpdatePermissionRejectsSubscriberAndAllowsAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &Handler{svc: &Service{rbac: user.NewRBAC()}}

	for _, test := range []struct {
		name string
		role string
		want int
	}{
		{name: "subscriber rejected", role: user.RoleSubscriber, want: http.StatusFound},
		{name: "admin allowed", role: user.RoleSuperAdmin, want: http.StatusNoContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.POST("/admin/settings", func(c *gin.Context) {
				c.Set("admin_role", test.role)
				if !handler.checkPermission(c, "setting", "update") {
					return
				}
				c.Status(http.StatusNoContent)
			})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/admin/settings", nil))
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d", recorder.Code, test.want)
			}
		})
	}
}
