// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/0xmattg/go-press/core/audit"
	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/hook"
	"github.com/0xmattg/go-press/core/notice"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/updatecheck"
	"github.com/0xmattg/go-press/core/user"

	"github.com/gin-gonic/gin"
)

// noticeFixture is a handler with a database-backed option store, an audit
// log and a filter that provides the notices in provide.
type noticeFixture struct {
	h       *Handler
	log     *audit.Service
	options *option.Store
	provide []notice.Notice
	unhook  func()
}

func newNoticeFixture(t *testing.T, provide ...notice.Notice) *noticeFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	users, log, db := extensionTestDB(t)
	store := option.NewStore(db)
	svc := &Service{rbac: user.NewRBAC(), userRepo: users, db: db, options: store}
	svc.SetAuditService(log)
	f := &noticeFixture{
		h:       NewHandler(svc, content.NewRegistry(), filepath.Join("templates")),
		log:     log,
		options: store,
		provide: provide,
	}
	bus := hook.New()
	handle := bus.AddFilter(hook.AdminNotices, func(value interface{}, args ...interface{}) interface{} {
		list := value.([]notice.Notice)
		if _, ok := args[0].(*gin.Context); !ok {
			t.Errorf("args[0] = %T, want *gin.Context", args[0])
		}
		if lang, _ := args[1].(string); lang != "en" {
			t.Errorf("args[1] = %v, want the admin language", args[1])
		}
		return append(list, f.provide...)
	}, 10)
	f.unhook = func() { bus.RemoveFilter(handle) }
	f.h.SetHookBus(bus)
	return f
}

// router serves the notice routes as the given role and user.
func (f *noticeFixture) router(role string, userID uint) *gin.Engine {
	router := gin.New()
	group := router.Group("/admin", func(c *gin.Context) {
		c.Set("admin_role", role)
		c.Set("admin_user_id", userID)
		c.Set("admin_username", "tester")
	})
	group.GET("/page", func(c *gin.Context) { f.h.render(c, "themes", gin.H{"Title": "Any page"}) })
	group.GET("/extension", func(c *gin.Context) {
		fragment := TemplateRef{
			FS:   fstest.MapFS{"orders.tmpl": {Data: []byte(`{{define "content"}}orders{{end}}`)}},
			Name: "orders.tmpl",
		}
		if err := f.h.RenderExtensionPageFS(c, fragment, "Orders", "orders", nil); err != nil {
			c.String(http.StatusInternalServerError, "RenderExtensionPageFS: "+err.Error())
		}
	})
	group.POST("/notices/dismiss", f.h.NoticeDismiss)
	return router
}

func (f *noticeFixture) page(role string, userID uint, path string) string {
	rec := httptest.NewRecorder()
	f.router(role, userID).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Body.String()
}

func (f *noticeFixture) dismiss(role string, userID uint, n notice.Notice, mode string) *httptest.ResponseRecorder {
	form := url.Values{
		"source":   {n.Source},
		"id":       {n.ID},
		"revision": {itoa(n.Revision)},
		"mode":     {mode},
	}
	return postForm(f.router(role, userID), "/admin/notices/dismiss", form)
}

func itoa(n int) string { return strconv.Itoa(n) }

func officialNotice() notice.Notice {
	return notice.Notice{
		Source: "core.official", ID: "gp-2026-001", Revision: 1,
		Category: notice.CategorySecurity, Severity: notice.SeverityCritical,
		Title:     "Security fix in 0.7.48",
		Message:   "Upgrade soon.\n<script>alert(1)</script>",
		Link:      &notice.Link{URL: "https://gopress.xyz/security/gp-2026-001", Label: "Read the advisory"},
		Snoozable: true, Acknowledgeable: true,
		RemindAfter: 2 * time.Hour,
	}
}

func editorNotice() notice.Notice {
	return notice.Notice{
		Source: "commerce", ID: "gateway", Revision: 1, Severity: notice.SeverityWarning,
		Title: "Payment gateway needs a key", Resource: "content", Action: "update",
		Link:      &notice.Link{URL: "/admin/plugins/commerce/settings"},
		Snoozable: true,
	}
}

func shown(body string, n notice.Notice) bool {
	return strings.Contains(body, `data-notice-source="`+n.Source+`" data-notice-id="`+n.ID+`"`)
}

// Notices appear on every admin render path, escaped, with the controls and
// accessibility attributes the notice allows.
func TestNoticesRenderOnAdminPages(t *testing.T) {
	f := newNoticeFixture(t, officialNotice(), editorNotice())
	for _, path := range []string{"/admin/page", "/admin/extension"} {
		body := f.page(user.RoleSuperAdmin, 1, path)
		if !shown(body, officialNotice()) || !shown(body, editorNotice()) {
			t.Fatalf("%s: notices missing:\n%s", path, body)
		}
		for _, want := range []string{
			`class="alert admin-notice admin-notice-critical" role="alert"`,
			`class="alert admin-notice admin-notice-warning" role="status"`,
			`<span class="admin-notice-category">Security</span>`,
			`&lt;script&gt;alert(1)&lt;/script&gt;`,
			`href="https://gopress.xyz/security/gp-2026-001" target="_blank" rel="noopener noreferrer">Read the advisory</a>`,
			`href="/admin/plugins/commerce/settings">Learn more</a>`,
			`data-notice-mode="acknowledge"`,
			`aria-label="Hide for now. It shows again in 2 hours if it still applies."`,
			`aria-label="Hide for now. It shows again in 24 hours if it still applies."`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %s", path, want)
			}
		}
		if strings.Contains(body, "<script>alert(1)") {
			t.Fatalf("%s: notice HTML was not escaped", path)
		}
		// The warning allows only "×".
		start := strings.Index(body, `data-notice-id="gateway"`)
		end := strings.Index(body[start:], "</div>\n            </div>")
		if end < 0 || strings.Contains(body[start:start+end], `data-notice-mode="acknowledge"`) {
			t.Errorf("%s: the warning must not offer \"Got it\"", path)
		}
	}
}

// More than three notices fold under "N more notices".
func TestNoticesFoldAfterThree(t *testing.T) {
	var many []notice.Notice
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		many = append(many, notice.Notice{Source: "core.test", ID: id, Title: "Notice " + id, Snoozable: true})
	}
	body := newNoticeFixture(t, many...).page(user.RoleSuperAdmin, 1, "/admin/page")
	open, folded, ok := strings.Cut(body, `<details class="admin-notices-more">`)
	if !ok {
		t.Fatalf("no folded group:\n%s", body)
	}
	if strings.Count(open, "admin-notice-info") != 3 || strings.Count(folded, "admin-notice-info") != 2 {
		t.Fatalf("open %d, folded %d; want 3 and 2", strings.Count(open, "admin-notice-info"), strings.Count(folded, "admin-notice-info"))
	}
	if !strings.Contains(folded, `data-more-many="%d more notices">2 more notices</summary>`) {
		t.Fatalf("folded label missing:\n%s", folded)
	}
}

// A notice shows only to roles with its capability: notice.read by default,
// which only super admins hold; a source may name another capability.
func TestNoticesFollowTheirCapability(t *testing.T) {
	f := newNoticeFixture(t, officialNotice(), editorNotice())
	for role, want := range map[string][2]bool{
		user.RoleSuperAdmin: {true, true},
		user.RoleEditor:     {false, true},
		user.RoleAuthor:     {false, false},
		user.RoleSubscriber: {false, false},
	} {
		body := f.page(role, 7, "/admin/page")
		if got := [2]bool{shown(body, officialNotice()), shown(body, editorNotice())}; got != want {
			t.Errorf("%s: shown = %v, want %v", role, got, want)
		}
	}
	if body := f.page("", 0, "/admin/page"); strings.Contains(body, "admin-notice") {
		t.Error("no notices without a signed-in user")
	}
}

// "Got it" hides that revision for that user only, across requests; a new
// revision shows again. Both actions are audited.
func TestAcknowledgeIsPerUserAndPerRevision(t *testing.T) {
	f := newNoticeFixture(t, officialNotice())
	n := officialNotice()
	if rec := f.dismiss(user.RoleSuperAdmin, 1, n, notice.ModeAcknowledge); rec.Code != http.StatusOK {
		t.Fatalf("acknowledge: %d %s", rec.Code, rec.Body.String())
	}
	if shown(f.page(user.RoleSuperAdmin, 1, "/admin/page"), n) {
		t.Fatal("user 1 still sees the acknowledged notice")
	}
	if !shown(f.page(user.RoleSuperAdmin, 2, "/admin/page"), n) {
		t.Fatal("user 2 must still see it")
	}
	if raw := f.options.Get("admin.notice_dismissed.1"); !strings.Contains(raw, `"core.official/gp-2026-001@1":{"mode":"acknowledge"`) {
		t.Fatalf("stored dismissals = %s", raw)
	}
	if rec := f.dismiss(user.RoleSuperAdmin, 1, n, notice.ModeAcknowledge); rec.Code != http.StatusNotFound {
		t.Fatalf("second acknowledge = %d, want 404 (no longer visible)", rec.Code)
	}

	f.provide[0].Revision = 2
	if !shown(f.page(user.RoleSuperAdmin, 1, "/admin/page"), n) {
		t.Fatal("a new revision must show again")
	}
	actions := auditActions(t, f.log)
	if len(actions) != 1 || actions[0] != "acknowledge notice: core.official/gp-2026-001@1" {
		t.Fatalf("audit = %v", actions)
	}
}

// "×" hides the notice until RemindAfter has passed.
func TestSnoozeHidesUntilRemindAfter(t *testing.T) {
	f := newNoticeFixture(t, officialNotice())
	n := officialNotice()
	if rec := f.dismiss(user.RoleSuperAdmin, 1, n, notice.ModeSnooze); rec.Code != http.StatusOK {
		t.Fatalf("snooze: %d %s", rec.Code, rec.Body.String())
	}
	if shown(f.page(user.RoleSuperAdmin, 1, "/admin/page"), n) {
		t.Fatal("the snoozed notice still shows")
	}
	stored := notice.ParseDismissals(f.options.Get("admin.notice_dismissed.1"))
	rec := stored[notice.KeyOf(n)]
	if rec.Mode != notice.ModeSnooze || rec.Until == nil || rec.Until.Sub(rec.At) != 2*time.Hour {
		t.Fatalf("stored = %+v, want a two-hour snooze", rec)
	}
	ended := rec.At.Add(-3 * time.Hour)
	rec.At, rec.Until = ended, &ended
	stored[notice.KeyOf(n)] = rec
	raw, _ := stored.Encode()
	if err := f.options.Set("admin.notice_dismissed.1", raw); err != nil {
		t.Fatal(err)
	}
	if !shown(f.page(user.RoleSuperAdmin, 1, "/admin/page"), n) {
		t.Fatal("the notice must show again after the snooze")
	}
	if actions := auditActions(t, f.log); len(actions) != 1 || !strings.HasPrefix(actions[0], "snooze notice: ") {
		t.Fatalf("audit = %v", actions)
	}
}

// The endpoint rejects what the user cannot see or the notice does not
// allow, and stores nothing then.
func TestNoticeDismissRejectsInvalidRequests(t *testing.T) {
	f := newNoticeFixture(t, officialNotice(), editorNotice())
	unknown := officialNotice()
	unknown.ID = "gp-2026-999"
	staleRevision := officialNotice()
	staleRevision.Revision = 2
	forged := officialNotice()
	forged.Source = "core.other"
	for _, test := range []struct {
		name   string
		role   string
		n      notice.Notice
		mode   string
		status int
		code   string
	}{
		{"role without notice.read", user.RoleEditor, officialNotice(), notice.ModeAcknowledge, http.StatusForbidden, "forbidden"},
		{"subscriber", user.RoleSubscriber, editorNotice(), notice.ModeSnooze, http.StatusForbidden, "forbidden"},
		{"unknown notice", user.RoleSuperAdmin, unknown, notice.ModeAcknowledge, http.StatusNotFound, "not_found"},
		{"other revision", user.RoleSuperAdmin, staleRevision, notice.ModeAcknowledge, http.StatusNotFound, "not_found"},
		{"forged source", user.RoleSuperAdmin, forged, notice.ModeAcknowledge, http.StatusNotFound, "not_found"},
		{"mode the notice does not allow", user.RoleEditor, editorNotice(), notice.ModeAcknowledge, http.StatusBadRequest, "mode_not_allowed"},
		{"unknown mode", user.RoleSuperAdmin, officialNotice(), "forever", http.StatusBadRequest, "invalid_mode"},
	} {
		t.Run(test.name, func(t *testing.T) {
			rec := f.dismiss(test.role, 5, test.n, test.mode)
			if rec.Code != test.status || errorCode(t, rec) != test.code {
				t.Fatalf("got %d %s, want %d %s", rec.Code, rec.Body.String(), test.status, test.code)
			}
		})
	}
	if raw := f.options.Get("admin.notice_dismissed.5"); raw != "" {
		t.Fatalf("a rejected request stored %s", raw)
	}
	if actions := auditActions(t, f.log); len(actions) != 0 {
		t.Fatalf("a rejected request was audited: %v", actions)
	}
	// An editor may snooze the notice it can see.
	if rec := f.dismiss(user.RoleEditor, 5, editorNotice(), notice.ModeSnooze); rec.Code != http.StatusOK {
		t.Fatalf("editor snooze = %d %s", rec.Code, rec.Body.String())
	}
}

// A notice whose source went away cannot be dismissed, and the user's
// records of it are pruned on the next write.
func TestDismissalsArePrunedWhenNoticesGoAway(t *testing.T) {
	f := newNoticeFixture(t, officialNotice(), editorNotice())
	if rec := f.dismiss(user.RoleSuperAdmin, 1, officialNotice(), notice.ModeAcknowledge); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	f.unhook()
	if rec := f.dismiss(user.RoleSuperAdmin, 1, editorNotice(), notice.ModeSnooze); rec.Code != http.StatusNotFound {
		t.Fatalf("dismissing a notice no source provides = %d, want 404", rec.Code)
	}
	if body := f.page(user.RoleSuperAdmin, 1, "/admin/page"); strings.Contains(body, "admin-notices") {
		t.Fatal("deactivating the source must remove its notices")
	}

	f2 := newNoticeFixture(t, editorNotice())
	stale := notice.Dismissals{}
	stale.Record(officialNotice(), notice.ModeAcknowledge, time.Now())
	raw, _ := stale.Encode()
	if err := f2.options.Set("admin.notice_dismissed.1", raw); err != nil {
		t.Fatal(err)
	}
	if rec := f2.dismiss(user.RoleSuperAdmin, 1, editorNotice(), notice.ModeSnooze); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	got := notice.ParseDismissals(f2.options.Get("admin.notice_dismissed.1"))
	if _, ok := got["core.official/gp-2026-001@1"]; ok || len(got) != 1 {
		t.Fatalf("after the write = %v, want only the commerce snooze", got)
	}
}

// The dismissal route sits behind AuthMiddleware, which rejects
// cross-origin posts before the handler runs.
func TestNoticeDismissRejectsCrossOrigin(t *testing.T) {
	f := newNoticeFixture(t, officialNotice())
	auth := user.NewAuth("test-secret", 1, nil)
	token, err := auth.GenerateToken(&user.User{ID: 1, Username: "admin", Role: user.RoleSuperAdmin})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.POST("/admin/notices/dismiss", AuthMiddleware(auth), f.h.NoticeDismiss)
	form := url.Values{"source": {"core.official"}, "id": {"gp-2026-001"}, "revision": {"1"}, "mode": {"acknowledge"}}
	req := httptest.NewRequest(http.MethodPost, "https://example.com/admin/notices/dismiss", strings.NewReader(form.Encode()))
	req.Host = "example.com"
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: adminCookieName, Value: token})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin dismiss = %d, want 403", rec.Code)
	}
	if raw := f.options.Get("admin.notice_dismissed.1"); raw != "" {
		t.Fatalf("stored %s", raw)
	}
}

// The generic settings form cannot write another user's dismissals.
func TestNoticeDismissalsAreCoreOwned(t *testing.T) {
	if !coreOwnedOption("admin.notice_dismissed.1") {
		t.Fatal("notice dismissals must be reserved")
	}
}

type fakeUpdateStatus struct{ notices []notice.Notice }

func (f fakeUpdateStatus) Enabled() bool { return true }
func (f fakeUpdateStatus) Snapshot(updatecheck.Kind, string) updatecheck.Status {
	return updatecheck.Status{}
}
func (f fakeUpdateStatus) OfficialNotices(string) []notice.Notice { return f.notices }

// Official announcements reach the notice center as the core.official
// source, for notice.read holders, and can be dismissed like any notice.
func TestOfficialAnnouncementsInTheNoticeCenter(t *testing.T) {
	f := newNoticeFixture(t)
	official := notice.Notice{
		Source: updatecheck.OfficialNoticeSource, ID: "gp-2026-001", Revision: 1,
		Category: notice.CategorySecurity, Severity: notice.SeverityCritical,
		Title: "Security fix in 0.7.52", Message: "Upgrade soon.",
		Link:      &notice.Link{URL: "https://gopress.xyz/security/gp-2026-001"},
		Snoozable: true, Acknowledgeable: true,
	}
	f.h.SetUpdateStatusProvider(fakeUpdateStatus{notices: []notice.Notice{official}})
	if body := f.page(user.RoleSuperAdmin, 1, "/admin/page"); !shown(body, official) {
		t.Fatalf("the announcement is missing:\n%s", body)
	}
	if body := f.page(user.RoleEditor, 3, "/admin/page"); shown(body, official) {
		t.Fatal("editors do not hold notice.read")
	}
	if rec := f.dismiss(user.RoleSuperAdmin, 1, official, notice.ModeAcknowledge); rec.Code != http.StatusOK {
		t.Fatalf("acknowledge = %d %s", rec.Code, rec.Body.String())
	}
	if body := f.page(user.RoleSuperAdmin, 1, "/admin/page"); shown(body, official) {
		t.Fatal("still shown after Got it")
	}
}
