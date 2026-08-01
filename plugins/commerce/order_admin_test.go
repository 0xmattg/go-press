package commerce

import (
	"testing"

	"go-press/core/user"
)

// TestShopOrderRBAC verifies the authorization wiring that guards the order back
// office: a plain subscriber is denied every shop_order capability, while the
// editor grants commerce.Activate installs allow them — the exact tuples the
// admin routes check via admin.RequirePermission.
func TestShopOrderRBAC(t *testing.T) {
	rbac := user.NewRBAC()

	// Before activation: no role may touch shop orders (except super_admin, which
	// holds *.* by construction and isn't the concern here).
	for _, action := range []string{"read", "update", "refund"} {
		if rbac.Can(user.RoleSubscriber, "shop_order", action) {
			t.Errorf("subscriber unexpectedly allowed shop_order.%s before activation", action)
		}
		if rbac.Can(user.RoleEditor, "shop_order", action) {
			t.Errorf("editor unexpectedly allowed shop_order.%s before grant", action)
		}
	}

	// Mirror the grants commerce.Activate installs for the editor role.
	for _, capability := range [][2]string{
		{"shop_order", "read"}, {"shop_order", "update"}, {"shop_order", "refund"},
	} {
		rbac.GrantCapability(user.RoleEditor, capability[0], capability[1])
	}

	// Editor now allowed; subscriber still denied (the RequirePermission guard
	// on /admin/commerce/orders* returns 403 for them).
	for _, action := range []string{"read", "update", "refund"} {
		if !rbac.Can(user.RoleEditor, "shop_order", action) {
			t.Errorf("editor should be allowed shop_order.%s after grant", action)
		}
		if rbac.Can(user.RoleSubscriber, "shop_order", action) {
			t.Errorf("subscriber must remain denied shop_order.%s", action)
		}
	}
}
