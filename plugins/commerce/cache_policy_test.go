package commerce

import (
	"net/http"
	"net/http/httptest"
	"testing"

	corecache "github.com/0xmattg/go-press/core/cache"

	"github.com/gin-gonic/gin"
)

func TestCommerceCachePolicyMarksPersonalizedRequestsPrivate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name       string
		path       string
		cartCookie bool
		want       bool
	}{
		{name: "public store", path: "/store"},
		{name: "store with cart", path: "/store", cartCookie: true, want: true},
		{name: "cart", path: "/cart", want: true},
		{name: "checkout completion", path: "/checkout/complete/ORDER-1", want: true},
		{name: "tracking", path: "/order-tracking", want: true},
		{name: "account detail", path: "/my-account/orders/ORDER-1", want: true},
		{name: "similar public path", path: "/cartoon"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.cartCookie {
				c.Request.AddCookie(&http.Cookie{Name: cartCookie, Value: "cart-token"})
			}
			commerceCachePolicy()(c)
			if got := corecache.IsPageCacheBypassed(c); got != tc.want {
				t.Fatalf("bypass = %v, want %v", got, tc.want)
			}
			if tc.want && recorder.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatalf("Cache-Control = %q", recorder.Header().Get("Cache-Control"))
			}
		})
	}
}
