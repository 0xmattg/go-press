package commerce

import (
	"strings"

	corecache "go-press/core/cache"

	"github.com/gin-gonic/gin"
)

// commerceCachePolicy declares request privacy before Core's page-cache
// middleware runs. A cart cookie personalizes the mini-cart on every storefront
// page, while checkout/account routes are private even before a cart exists.
// Core only sees the generic bypass marker and never hard-codes Commerce paths.
func commerceCachePolicy() gin.HandlerFunc {
	return func(c *gin.Context) {
		_, cartErr := c.Cookie(cartCookie)
		if cartErr == nil || isCommercePrivatePath(c.Request.URL.Path) {
			corecache.BypassPageCache(c)
			c.Header("Cache-Control", "private, no-store")
		}
		c.Next()
	}
}

func isCommercePrivatePath(path string) bool {
	for _, prefix := range []string{"/cart", "/checkout", "/order-tracking", "/my-account"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
