package theme

import "testing"

func TestIsLanguagePrefixableURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "root path", url: "/", want: true},
		{name: "absolute local path", url: "/about", want: true},
		{name: "relative local path", url: "about", want: true},
		{name: "query local path", url: "/blog?category=news", want: true},
		{name: "external https", url: "https://github.com/0xmattg/go-press", want: false},
		{name: "external http", url: "http://example.com", want: false},
		{name: "protocol relative", url: "//cdn.example.com/app.js", want: false},
		{name: "anchor", url: "#features", want: false},
		{name: "query only", url: "?preview=1", want: false},
		{name: "mailto", url: "mailto:hello@example.com", want: false},
		{name: "tel", url: "tel:+15550100", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isLanguagePrefixableURL(tt.url); got != tt.want {
				t.Fatalf("isLanguagePrefixableURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}
