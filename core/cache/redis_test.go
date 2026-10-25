package cache

import (
	"strings"
	"testing"
)

func TestRedisKeyPrefixIsolatesNamespaces(t *testing.T) {
	if got := redisKeyPrefix(""); got != "gopress:" {
		t.Fatalf("legacy prefix = %q", got)
	}
	if got := redisKeyPrefix("site-a"); got != "gopress:site-a:" {
		t.Fatalf("namespaced prefix = %q", got)
	}
	// DeleteByPrefix/Flush scan "<prefix>*": one site's pattern must never
	// match another site's keys, even when one namespace prefixes the other.
	otherKey := redisKeyPrefix("site-ab") + pageCachePrefix + "k"
	if strings.HasPrefix(otherKey, redisKeyPrefix("site-a")) {
		t.Fatalf("site-a flush pattern would match %q", otherKey)
	}
}
