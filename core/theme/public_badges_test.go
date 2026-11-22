// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package theme

import (
	"bytes"
	"html/template"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/0xmattg/go-press/core/hook"
	"github.com/0xmattg/go-press/core/user"
)

func userBadgesFunc(t *testing.T, bus *hook.Bus) func(interface{}, interface{}) []user.PublicBadge {
	t.Helper()
	b := &BaseTheme{}
	b.InitBase(&hookTestApp{bus: bus}, "", "", nil)
	fn, ok := b.BaseFuncMap()["userBadges"].(func(interface{}, interface{}) []user.PublicBadge)
	if !ok {
		t.Fatalf("userBadges has type %T", b.BaseFuncMap()["userBadges"])
	}
	return fn
}

func TestUserBadgesAskExtensionsOncePerMemberAndRequest(t *testing.T) {
	bus := hook.New()
	calls := map[uint]int{}
	bus.AddFilter(hook.UserPublicBadges, func(value interface{}, args ...interface{}) interface{} {
		id, _ := args[0].(uint)
		if _, ok := args[1].(*gin.Context); !ok {
			t.Errorf("args[1] = %T, want the request context", args[1])
		}
		calls[id]++
		list := value.([]user.PublicBadge)
		if id == 7 {
			list = append(list,
				user.PublicBadge{Source: "market", Key: "developer", Label: "Developer <b>", URL: "/market/developers/lin", Level: user.BadgeLevelVerified},
				user.PublicBadge{Source: "market", Key: "elsewhere", Label: "Elsewhere", URL: "javascript:alert(1)"},
			)
		}
		return list
	}, 10)
	badges := userBadgesFunc(t, bus)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/forums", nil)
	data := map[string]interface{}{"Ctx": c}
	got := badges(data, uint(7))
	if len(got) != 2 || got[0].Label != "Developer <b>" || got[1].URL != "" {
		t.Fatalf("badges = %+v", got)
	}
	// Other integer types and the context itself work; the second and third
	// lookups of member 7 come from the request cache.
	if len(badges(c, 7)) != 2 || len(badges(data, int64(7))) != 2 || calls[7] != 1 {
		t.Fatalf("member 7 asked %d times", calls[7])
	}
	if len(badges(c, uint(8))) != 0 || len(badges(c, uint(8))) != 0 || calls[8] != 1 {
		t.Fatalf("member 8 asked %d times", calls[8])
	}
	// No member, no context, or an ID of another type: no filter call.
	for _, id := range []interface{}{0, uint(0), -3, "7", nil} {
		if len(badges(c, id)) != 0 {
			t.Fatalf("id %v got badges", id)
		}
	}
	if len(badges(nil, uint(7))) != 0 || len(calls) != 2 {
		t.Fatalf("calls = %v", calls)
	}

	// A new request asks again.
	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Request = httptest.NewRequest("GET", "/forums", nil)
	badges(c2, uint(7))
	if calls[7] != 2 {
		t.Fatalf("member 7 asked %d times across two requests", calls[7])
	}

	// Labels are escaped by the theme's template.
	tmpl := template.Must(template.New("x").Funcs(template.FuncMap{"userBadges": badges}).Parse(
		`{{range userBadges .Ctx 7}}<a href="{{.URL}}" data-level="{{.Level}}">{{.Label}}</a>{{end}}`))
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Developer &lt;b&gt;") || strings.Contains(out.String(), "javascript:") {
		t.Fatalf("rendered %s", out.String())
	}
}

func TestUserBadgesWithoutHooksIsEmpty(t *testing.T) {
	if got := CommonFuncMap()["userBadges"].(func(interface{}, interface{}) []user.PublicBadge)(nil, 7); got != nil {
		t.Fatalf("default userBadges = %+v", got)
	}
}
