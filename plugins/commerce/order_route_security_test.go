package commerce

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"go-press/core"
	"go-press/core/hook"
	"go-press/core/user"

	"github.com/gin-gonic/gin"
)

func TestOrderAdminRoutesRejectInsufficientRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := user.NewAuth("commerce-route-test-secret", 1, nil)
	rbac := user.NewRBAC()
	// A deliberately read-only subscriber can reach neither mutation class.
	rbac.GrantCapability(user.RoleSubscriber, "shop_order", "read")
	token, err := auth.GenerateToken(&user.User{ID: 9, Username: "reader", Role: user.RoleSubscriber})
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	p := &Plugin{engine: &core.Engine{Auth: auth, RBAC: rbac}}
	r := gin.New()
	p.registerOrderAdminRoutes(r)

	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "mark paid needs update", path: "/admin/commerce/orders/1/mark-paid"},
		{name: "status needs update", path: "/admin/commerce/orders/1/status"},
		{name: "note needs update", path: "/admin/commerce/orders/1/note"},
		{name: "refund needs refund", path: "/admin/commerce/orders/1/refund"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "https://shop.example"+tc.path, nil)
			req.Host = "shop.example"
			req.Header.Set("Origin", "https://shop.example")
			req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", rec.Code)
			}
		})
	}
}

func TestOrderAdminReadRouteRejectsRoleWithoutReadCapability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := user.NewAuth("commerce-route-test-secret", 1, nil)
	rbac := user.NewRBAC()
	token, err := auth.GenerateToken(&user.User{ID: 10, Username: "subscriber", Role: user.RoleSubscriber})
	if err != nil {
		t.Fatal(err)
	}
	p := &Plugin{engine: &core.Engine{Auth: auth, RBAC: rbac}}
	r := gin.New()
	p.registerOrderAdminRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "https://shop.example/admin/commerce/orders", nil)
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestOrderAdminStatusRouteAllowsRoleWithUpdateCapability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := commerceTestDB(t)
	auth := user.NewAuth("commerce-route-test-secret", 1, nil)
	rbac := user.NewRBAC()
	rbac.GrantCapability(user.RoleEditor, "shop_order", "update")
	token, err := auth.GenerateToken(&user.User{ID: 2, Username: "editor", Role: user.RoleEditor})
	if err != nil {
		t.Fatal(err)
	}
	order := Order{Number: "ROUTE-ALLOWED", Status: OrderProcessing, Currency: "USD", GrandTotal: 1000}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	p := &Plugin{engine: &core.Engine{DB: db, Hooks: hook.New(), Auth: auth, RBAC: rbac}}
	r := gin.New()
	p.registerOrderAdminRoutes(r)

	form := url.Values{"event": {EventShip}}
	req := httptest.NewRequest(http.MethodPost, "https://shop.example/admin/commerce/orders/"+
		strconv.FormatUint(uint64(order.ID), 10)+"/status", strings.NewReader(form.Encode()))
	req.Host = "shop.example"
	req.Header.Set("Origin", "https://shop.example")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	var saved Order
	if err := db.First(&saved, order.ID).Error; err != nil || saved.Status != OrderCompleted {
		t.Fatalf("order status = %q, err=%v; want completed", saved.Status, err)
	}
}

func TestOrderAdminRoutesRejectCrossOriginMutationBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := user.NewAuth("commerce-route-test-secret", 1, nil)
	rbac := user.NewRBAC()
	rbac.GrantCapability(user.RoleEditor, "shop_order", "update")
	token, err := auth.GenerateToken(&user.User{ID: 2, Username: "editor", Role: user.RoleEditor})
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	p := &Plugin{engine: &core.Engine{Auth: auth, RBAC: rbac}}
	r := gin.New()
	p.registerOrderAdminRoutes(r)

	req := httptest.NewRequest(http.MethodPost, "https://shop.example/admin/commerce/orders/1/status", nil)
	req.Host = "shop.example"
	req.Header.Set("Origin", "https://evil.example")
	req.AddCookie(&http.Cookie{Name: "admin_token", Value: token})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestCanViewOrderRequiresOwnerOrAccessKey(t *testing.T) {
	p := &Plugin{}
	ownerID := uint(7)
	order := &Order{UserID: &ownerID, AccessKey: "correct-key"}

	ctx := func(rawQuery string, account *user.User) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/checkout/complete/X"+rawQuery, nil)
		if account != nil {
			c.Set(user.CtxKeyPublicUser, account)
		}
		return c
	}

	if !p.canViewOrder(ctx("", &user.User{ID: ownerID}), order) {
		t.Fatal("owner should be allowed without access key")
	}
	if p.canViewOrder(ctx("", &user.User{ID: 8}), order) {
		t.Fatal("different logged-in user must not be allowed")
	}
	if p.canViewOrder(ctx("?key=wrong", nil), order) {
		t.Fatal("wrong guest access key must be rejected")
	}
	if !p.canViewOrder(ctx("?key=correct-key", nil), order) {
		t.Fatal("matching guest access key should be allowed")
	}
}

func TestCheckoutPayRouteRejectsWrongOrderAccessKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := commerceTestDB(t)
	order := Order{
		Number: "PAY-PRIVATE", AccessKey: "correct-key", Status: OrderPending,
		Currency: "USD", GrandTotal: 1000,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	p := &Plugin{engine: &core.Engine{DB: db}}
	r := gin.New()
	p.registerCheckoutRoutes(r)

	for _, rawQuery := range []string{"", "?key=wrong-key"} {
		req := httptest.NewRequest(http.MethodGet, "/checkout/pay/PAY-PRIVATE"+rawQuery, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("query %q status = %d, want 404", rawQuery, rec.Code)
		}
	}
}
