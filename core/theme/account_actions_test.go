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

// hookTestApp is dataTestApp with a hook bus.
type hookTestApp struct {
	dataTestApp
	bus *hook.Bus
}

func (a *hookTestApp) HookBus() *hook.Bus { return a.bus }

func accountActionsFunc(t *testing.T, bus *hook.Bus) func(interface{}) []user.AccountAction {
	t.Helper()
	b := &BaseTheme{}
	b.InitBase(&hookTestApp{bus: bus}, "", "", nil)
	fn, ok := b.BaseFuncMap()["accountActions"].(func(interface{}) []user.AccountAction)
	if !ok {
		t.Fatalf("accountActions has type %T", b.BaseFuncMap()["accountActions"])
	}
	return fn
}

func memberContext(signedIn bool) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/profile", nil)
	if signedIn {
		c.Set(user.CtxKeyPublicUser, &user.User{ID: 7, Username: "alex", IsActive: true})
	}
	return c
}

func TestAccountActionsAskExtensionsForSignedInMembersOnly(t *testing.T) {
	bus := hook.New()
	calls := 0
	bus.AddFilter(hook.UserAccountActions, func(value interface{}, args ...interface{}) interface{} {
		calls++
		c, _ := args[0].(*gin.Context)
		account := user.CurrentUser(c)
		list := value.([]user.AccountAction)
		return append(list,
			user.AccountAction{ID: "market", Title: "Hello " + account.Username, Text: "<b>bold</b>", Label: "Open", URL: "/market/developer", Badge: "New", Tone: user.AccountToneSuccess, Order: 20},
			user.AccountAction{ID: "evil", Title: "Elsewhere", Label: "Go", URL: "https://evil.test/"},
			user.AccountAction{ID: "orders", Title: "Orders", Label: "View", URL: "/account/orders", Order: 10},
		)
	}, 10)
	fn := accountActionsFunc(t, bus)

	if got := fn(memberContext(false)); got != nil || calls != 0 {
		t.Fatalf("guest: %+v, filter called %d times", got, calls)
	}
	if got := fn(nil); got != nil || calls != 0 {
		t.Fatalf("no context: %+v", got)
	}
	// Page data carrying the context works like the context itself.
	got := fn(gin.H{"Ctx": memberContext(true)})
	if calls != 1 || len(got) != 2 || got[0].ID != "orders" || got[1].Title != "Hello alex" {
		t.Fatalf("member: %+v (calls %d)", got, calls)
	}

	// Templates escape the text.
	tpl := template.Must(template.New("x").Funcs(template.FuncMap{"accountActions": fn}).Parse(
		`{{range accountActions .Ctx}}<a href="{{.URL}}">{{.Title}}: {{.Text}}</a>{{end}}`))
	var out bytes.Buffer
	if err := tpl.Execute(&out, gin.H{"Ctx": memberContext(true)}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "&lt;b&gt;bold&lt;/b&gt;") || strings.Contains(out.String(), "evil.test") {
		t.Fatalf("rendered: %s", out.String())
	}
}

func TestAccountActionsIgnoreFiltersReturningOtherTypes(t *testing.T) {
	bus := hook.New()
	bus.AddFilter(hook.UserAccountActions, func(interface{}, ...interface{}) interface{} {
		return template.HTML("<p>not a list</p>")
	}, 10)
	if got := accountActionsFunc(t, bus)(memberContext(true)); got != nil {
		t.Fatalf("got %+v", got)
	}
	if got := accountActionsFunc(t, nil)(memberContext(true)); got != nil {
		t.Fatalf("without a hook bus: %+v", got)
	}
	// The default map and the package checker know the function too.
	if CommonFuncMap()["accountActions"] == nil || TemplateCheckFuncs()["accountActions"] == nil {
		t.Fatal("accountActions missing from the default or check function maps")
	}
}
