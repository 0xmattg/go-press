package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsSameOrigin(t *testing.T) {
	cases := []struct {
		name    string
		method  string
		host    string
		origin  string
		referer string
		want    bool
	}{
		{name: "matching origin", host: "example.com", origin: "https://example.com", want: true},
		{name: "mismatched origin", host: "example.com", origin: "https://evil.example", want: false},
		{name: "matching referer only", host: "example.com", referer: "https://example.com/admin/users", want: true},
		{name: "mismatched referer only", host: "example.com", referer: "https://evil.example/x", want: false},
		{name: "no headers is lenient", host: "example.com", want: true},
		{name: "origin wins over referer", host: "example.com", origin: "https://example.com", referer: "https://evil.example", want: true},
		{name: "case-insensitive host", host: "Example.COM", origin: "https://example.com", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "https://"+tc.host+"/admin/users", nil)
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.referer != "" {
				req.Header.Set("Referer", tc.referer)
			}
			if got := IsSameOrigin(req); got != tc.want {
				t.Fatalf("IsSameOrigin() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsStateChangingMethod(t *testing.T) {
	safe := []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace}
	for _, m := range safe {
		if IsStateChangingMethod(m) {
			t.Errorf("%s should be treated as safe", m)
		}
	}
	unsafe := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, m := range unsafe {
		if !IsStateChangingMethod(m) {
			t.Errorf("%s should be treated as state-changing", m)
		}
	}
}
