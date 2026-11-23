// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/0xmattg/go-press/core/audit"
	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/mfa"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/user"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const mfaTestSecret = "test-secret"

// mfaFixture is the admin with real routes, sessions and two-step
// verification over an SQLite database with two super admins: 1 "admin"
// (password testPassword) and 2 "sso" (no password).
type mfaFixture struct {
	t      *testing.T
	h      *Handler
	m      *mfa.Service
	auth   *user.Auth
	log    *audit.Service
	db     *gorm.DB
	opts   *option.Store
	router *gin.Engine
	clock  time.Time
}

func newMFAFixture(t *testing.T) *mfaFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	users, log, db := extensionTestDB(t)
	if err := db.Exec(`CREATE TABLE gp_user_mfa (user_id INTEGER PRIMARY KEY, totp_secret TEXT NOT NULL DEFAULT '',
		pending_secret TEXT NOT NULL DEFAULT '', pending_expires_at DATETIME, key_id TEXT NOT NULL DEFAULT '',
		enabled_at DATETIME, last_step INTEGER NOT NULL DEFAULT 0, recovery_hashes TEXT NOT NULL DEFAULT '',
		recovery_generated_at DATETIME, failed_count INTEGER NOT NULL DEFAULT 0, failed_since DATETIME,
		locked_until DATETIME, sessions_not_before DATETIME, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO gp_users (id, username, password_hash, role) SELECT 3, 'ed', password_hash, 'editor' FROM gp_users WHERE id = 1`).Error; err != nil {
		t.Fatal(err)
	}
	f := &mfaFixture{t: t, log: log, db: db, clock: time.Now().UTC().Truncate(30 * time.Second).Add(time.Second)}
	m, err := mfa.New(db, mfaTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	m.SetClock(func() time.Time { return f.clock })
	f.m = m
	f.opts = option.NewStore(db)
	f.auth = user.NewAuth(mfaTestSecret, 1, users)
	f.auth.SetSecondFactor(m)
	f.auth.SetSecondFactorPolicy(SecondFactorPolicy(f.opts))
	svc := &Service{rbac: user.NewRBAC(), userRepo: users, db: db, options: f.opts, auth: f.auth}
	svc.SetAuditService(log)
	svc.SetMFA(m)
	f.h = NewHandler(svc, content.NewRegistry(), filepath.Join("templates"))
	f.router = gin.New()
	SetupRoutes(f.router, f.h, f.auth, content.NewRegistry())
	return f
}

// tick moves to the next 30-second window, so a new code is accepted.
func (f *mfaFixture) tick() { f.clock = f.clock.Add(30 * time.Second) }

func (f *mfaFixture) code(secret string) string {
	f.t.Helper()
	code, err := mfa.CodeAt(secret, f.clock)
	if err != nil {
		f.t.Fatal(err)
	}
	return code
}

// enroll turns two-step verification on for a user and returns the key,
// the recovery codes and a session signed in with a code.
func (f *mfaFixture) enroll(userID uint) (string, []string, string) {
	f.t.Helper()
	e, err := f.m.Begin(context.Background(), userID, "GoPress", "x")
	if err != nil {
		f.t.Fatal(err)
	}
	codes, enabledAt, err := f.m.Confirm(context.Background(), userID, f.code(e.Secret))
	if err != nil {
		f.t.Fatal(err)
	}
	f.tick()
	return e.Secret, codes, f.session(userID, enabledAt)
}

func (f *mfaFixture) session(userID uint, secondFactorAt time.Time) string {
	f.t.Helper()
	account, err := f.h.svc.userRepo.FindByID(userID)
	if err != nil {
		f.t.Fatal(err)
	}
	token, err := f.auth.RenewSession(account, secondFactorAt)
	if err != nil {
		f.t.Fatal(err)
	}
	return token
}

type mfaRequest struct {
	method, path string
	form         url.Values
	token        string
	cookies      []*http.Cookie
	origin       string
	header       http.Header
}

func (f *mfaFixture) do(r mfaRequest) *httptest.ResponseRecorder {
	var body *strings.Reader
	if r.form != nil {
		body = strings.NewReader(r.form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(r.method, "http://example.com"+r.path, body)
	req.Host = "example.com"
	if r.form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if r.origin != "" {
		req.Header.Set("Origin", r.origin)
	} else if r.method == http.MethodPost {
		req.Header.Set("Origin", "http://example.com")
	}
	for k, v := range r.header {
		req.Header[k] = v
	}
	if r.token != "" {
		req.AddCookie(&http.Cookie{Name: adminCookieName, Value: r.token})
	}
	for _, c := range r.cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func (f *mfaFixture) audited(action string) []string {
	var out []string
	for _, a := range auditActions(f.t, f.log) {
		if strings.HasPrefix(a, action+" ") {
			out = append(out, a)
		}
	}
	return out
}

func TestLoginWithoutSecondFactorIsUnchanged(t *testing.T) {
	f := newMFAFixture(t)
	rec := f.do(mfaRequest{method: http.MethodPost, path: "/admin/login", form: url.Values{"username": {"admin"}, "password": {testPassword}}})
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/admin/" || cookieNamed(rec, adminCookieName) == nil {
		t.Fatalf("login = %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

func TestLoginAsksForACodeAndStartsTheSessionAfterIt(t *testing.T) {
	f := newMFAFixture(t)
	secret, _, _ := f.enroll(1)
	oldSession := f.session(1, time.Time{})

	rec := f.do(mfaRequest{method: http.MethodPost, path: "/admin/login", form: url.Values{"username": {"admin"}, "password": {testPassword}}})
	ticket := cookieNamed(rec, adminMFACookie)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != loginVerifyPath || ticket == nil || cookieNamed(rec, adminCookieName) != nil {
		t.Fatalf("password step = %d %s; it must not start a session", rec.Code, rec.Header().Get("Location"))
	}
	if ticket.Path != adminMFACookiePath || !ticket.HttpOnly {
		t.Fatalf("ticket cookie = %+v", ticket)
	}
	page := f.do(mfaRequest{method: http.MethodGet, path: loginVerifyPath, cookies: []*http.Cookie{ticket}})
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `autocomplete="one-time-code"`) {
		t.Fatalf("code page = %d", page.Code)
	}

	wrong := f.do(mfaRequest{method: http.MethodPost, path: loginVerifyPath, form: url.Values{"code": {"000000"}}, cookies: []*http.Cookie{ticket}})
	if wrong.Code != http.StatusUnauthorized || cookieNamed(wrong, adminCookieName) != nil {
		t.Fatalf("wrong code = %d", wrong.Code)
	}
	if got := f.audited("login_failed"); len(got) != 1 {
		t.Fatalf("audit = %v", auditActions(t, f.log))
	}

	ok := f.do(mfaRequest{method: http.MethodPost, path: loginVerifyPath, form: url.Values{"code": {f.code(secret)}}, cookies: []*http.Cookie{ticket}})
	session := cookieNamed(ok, adminCookieName)
	if ok.Code != http.StatusFound || session == nil {
		t.Fatalf("right code = %d %s", ok.Code, ok.Body.String())
	}
	if cleared := cookieNamed(ok, adminMFACookie); cleared == nil || cleared.MaxAge >= 0 {
		t.Fatal("the ticket must be cleared")
	}
	claims, err := f.auth.ActiveClaims(session.Value)
	if err != nil || !claims.SecondFactor {
		t.Fatalf("new session: %+v %v", claims, err)
	}
	if _, err := f.auth.ActiveClaims(oldSession); err == nil {
		t.Fatal("a session from before two-step verification was turned on must end")
	}
	if got := f.do(mfaRequest{method: http.MethodGet, path: AccountSecurityPath, token: session.Value}); got.Code != http.StatusOK {
		t.Fatalf("admin page with the new session = %d", got.Code)
	}
}

func TestLoginWithARecoveryCode(t *testing.T) {
	f := newMFAFixture(t)
	_, codes, _ := f.enroll(1)
	rec := f.do(mfaRequest{method: http.MethodPost, path: "/admin/login", form: url.Values{"username": {"admin"}, "password": {testPassword}}})
	ticket := cookieNamed(rec, adminMFACookie)
	ok := f.do(mfaRequest{method: http.MethodPost, path: loginVerifyPath, form: url.Values{"code": {codes[0]}, "recovery": {"1"}}, cookies: []*http.Cookie{ticket}})
	if ok.Code != http.StatusFound || cookieNamed(ok, adminCookieName) == nil {
		t.Fatalf("recovery code = %d", ok.Code)
	}
	if got := f.audited("login"); len(got) != 1 || !strings.Contains(got[0], "recovery code") {
		t.Fatalf("audit = %v", got)
	}
	again := f.do(mfaRequest{method: http.MethodPost, path: loginVerifyPath, form: url.Values{"code": {codes[0]}}, cookies: []*http.Cookie{ticket}})
	if again.Code != http.StatusUnauthorized {
		t.Fatalf("a used recovery code = %d", again.Code)
	}
}

func TestLoginCodeStepNeedsTicketAndSameOrigin(t *testing.T) {
	f := newMFAFixture(t)
	secret, _, _ := f.enroll(1)
	rec := f.do(mfaRequest{method: http.MethodPost, path: loginVerifyPath, form: url.Values{"code": {f.code(secret)}}})
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/admin/login?error=") {
		t.Fatalf("without a ticket = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	forged := &http.Cookie{Name: adminMFACookie, Value: "1.99999999999.00.forged"}
	if rec := f.do(mfaRequest{method: http.MethodPost, path: loginVerifyPath, form: url.Values{"code": {f.code(secret)}}, cookies: []*http.Cookie{forged}}); cookieNamed(rec, adminCookieName) != nil {
		t.Fatal("a forged ticket started a session")
	}
	login := f.do(mfaRequest{method: http.MethodPost, path: "/admin/login", form: url.Values{"username": {"admin"}, "password": {testPassword}}})
	ticket := cookieNamed(login, adminMFACookie)
	cross := f.do(mfaRequest{method: http.MethodPost, path: loginVerifyPath, form: url.Values{"code": {f.code(secret)}}, cookies: []*http.Cookie{ticket}, origin: "https://evil.example"})
	if cross.Code != http.StatusForbidden {
		t.Fatalf("cross-origin code step = %d", cross.Code)
	}
	// The ticket is not a session.
	if rec := f.do(mfaRequest{method: http.MethodGet, path: AccountSecurityPath, token: ticket.Value}); rec.Code != http.StatusFound {
		t.Fatalf("ticket used as a session = %d", rec.Code)
	}
}

var secretPattern = regexp.MustCompile(`id="mfaSecret">([A-Z2-7 ]+)<`)

func TestAccountSecuritySetupKeepsThisSessionAndEndsOthers(t *testing.T) {
	f := newMFAFixture(t)
	session := f.session(1, time.Time{})
	other := f.session(1, time.Time{})

	wrong := f.do(mfaRequest{method: http.MethodPost, path: AccountSecurityPath + "/begin", token: session, form: url.Values{"password": {"wrong"}}})
	if wrong.Code != http.StatusOK || strings.Contains(wrong.Body.String(), "mfaSecret") {
		t.Fatalf("wrong password = %d", wrong.Code)
	}
	// user_id in the form is ignored: the account comes from the session.
	begin := f.do(mfaRequest{method: http.MethodPost, path: AccountSecurityPath + "/begin", token: session, form: url.Values{"password": {testPassword}, "user_id": {"2"}}})
	match := secretPattern.FindStringSubmatch(begin.Body.String())
	if begin.Code != http.StatusOK || match == nil || !strings.Contains(begin.Body.String(), `src="data:image/png;base64,`) {
		t.Fatalf("begin = %d: no QR code and key on the page", begin.Code)
	}
	if st, _ := f.m.Status(context.Background(), 2); st.Pending || st.Enabled {
		t.Fatal("a user_id in the form must not touch another account")
	}
	confirm := f.do(mfaRequest{method: http.MethodPost, path: AccountSecurityPath + "/confirm", token: session, form: url.Values{"code": {f.code(match[1])}}})
	renewed := cookieNamed(confirm, adminCookieName)
	if confirm.Code != http.StatusOK || renewed == nil || strings.Count(confirm.Body.String(), "<li><code>") != mfa.RecoveryCodeCount {
		t.Fatalf("confirm = %d", confirm.Code)
	}
	if !strings.Contains(confirm.Body.String(), `download="gopress-recovery-codes.txt"`) {
		t.Fatal("the recovery codes can be downloaded")
	}
	if _, err := f.auth.ActiveClaims(renewed.Value); err != nil {
		t.Fatalf("this session must stay signed in: %v", err)
	}
	if _, err := f.auth.ActiveClaims(other); err == nil {
		t.Fatal("other sessions must end")
	}
	if got := f.audited("mfa_enable"); len(got) != 1 {
		t.Fatalf("audit = %v", auditActions(t, f.log))
	}
}

func TestAccountSecurityTurnOff(t *testing.T) {
	f := newMFAFixture(t)
	secret, codes, session := f.enroll(1)
	turnOff := func(password, code string) *httptest.ResponseRecorder {
		return f.do(mfaRequest{method: http.MethodPost, path: AccountSecurityPath + "/disable", token: session, form: url.Values{"password": {password}, "code": {code}}})
	}
	if rec := turnOff("wrong", f.code(secret)); rec.Code != http.StatusOK {
		t.Fatalf("wrong password = %d", rec.Code)
	}
	if rec := turnOff(testPassword, "000000"); rec.Code != http.StatusOK {
		t.Fatalf("wrong code = %d", rec.Code)
	}
	if _, on, _ := f.m.EnabledAt(1); !on {
		t.Fatal("a refused request turned it off")
	}

	// A role that must use it cannot turn it off.
	if err := f.opts.Set(OptionSecondFactorRoles, user.RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}
	if rec := turnOff(testPassword, codes[0]); rec.Code != http.StatusOK {
		t.Fatalf("required role = %d", rec.Code)
	}
	if _, on, _ := f.m.EnabledAt(1); !on {
		t.Fatal("a required role turned it off")
	}
	if err := f.opts.Set(OptionSecondFactorRoles, ""); err != nil {
		t.Fatal(err)
	}
	rec := turnOff(testPassword, codes[0])
	if rec.Code != http.StatusFound {
		t.Fatalf("turn off = %d %s", rec.Code, rec.Body.String())
	}
	if _, on, _ := f.m.EnabledAt(1); on {
		t.Fatal("still on")
	}
	if _, err := f.auth.ActiveClaims(session); err != nil {
		t.Fatalf("turning it off yourself keeps the session: %v", err)
	}
}

func TestRequiredRolesCanOnlyOpenTheSetupPage(t *testing.T) {
	f := newMFAFixture(t)
	secret, _, adminSession := f.enroll(1)
	editor := f.session(3, time.Time{})

	// Only those who may change settings set the policy.
	policy := url.Values{"required_roles": {user.RoleSuperAdmin, "not-a-role"}}
	if rec := f.do(mfaRequest{method: http.MethodPost, path: AccountSecurityPath + "/policy", token: editor, form: policy}); rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "error=") {
		t.Fatalf("editor = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	// From an account with two-step verification on, it takes a code.
	if rec := f.do(mfaRequest{method: http.MethodPost, path: AccountSecurityPath + "/policy", token: adminSession, form: policy}); rec.Code != http.StatusOK {
		t.Fatalf("without a code = %d", rec.Code)
	}
	if f.opts.Get(OptionSecondFactorRoles) != "" {
		t.Fatal("saved without a code")
	}
	policy.Set(OTPField, f.code(secret))
	if rec := f.do(mfaRequest{method: http.MethodPost, path: AccountSecurityPath + "/policy", token: adminSession, form: policy}); rec.Code != http.StatusFound {
		t.Fatalf("with a code = %d", rec.Code)
	}
	if got := f.opts.Get(OptionSecondFactorRoles); got != user.RoleSuperAdmin {
		t.Fatalf("saved %q; unknown roles are dropped", got)
	}

	// Super admin 2 has no second factor: only the setup page opens.
	sso := f.session(2, time.Time{})
	if rec := f.do(mfaRequest{method: http.MethodGet, path: "/admin/users", token: sso}); rec.Code != http.StatusFound || rec.Header().Get("Location") != AccountSecurityPath+"?required=1" {
		t.Fatalf("dashboard = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if rec := f.do(mfaRequest{method: http.MethodPost, path: "/admin/notices/dismiss", token: sso, form: url.Values{}}); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), codeSetup) {
		t.Fatalf("other actions = %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(mfaRequest{method: http.MethodGet, path: AccountSecurityPath, token: sso}); rec.Code != http.StatusOK {
		t.Fatalf("setup page = %d", rec.Code)
	}
	if rec := f.do(mfaRequest{method: http.MethodGet, path: "/admin/users", token: editor}); rec.Code != http.StatusOK {
		t.Fatalf("roles not on the list are unaffected: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

func TestSensitiveUserChangesNeedACode(t *testing.T) {
	f := newMFAFixture(t)
	secret, _, session := f.enroll(1)
	before, _ := f.h.svc.userRepo.FindByID(3)
	update := url.Values{"email": {"ed@example.com"}, "role": {user.RoleEditor}, "password": {"new password 123"}, "is_active": {"1"}}
	if rec := f.do(mfaRequest{method: http.MethodPost, path: "/admin/users/3/edit", token: session, form: update}); rec.Code != http.StatusOK {
		t.Fatalf("without a code = %d", rec.Code)
	}
	if after, _ := f.h.svc.userRepo.FindByID(3); after.PasswordHash != before.PasswordHash {
		t.Fatal("the password changed without a code")
	}
	update.Set(OTPField, f.code(secret))
	if rec := f.do(mfaRequest{method: http.MethodPost, path: "/admin/users/3/edit", token: session, form: update}); rec.Code != http.StatusFound {
		t.Fatalf("with a code = %d %s", rec.Code, rec.Body.String())
	}
	f.tick()
	// Making someone a super admin needs one too.
	promote := url.Values{"email": {"ed@example.com"}, "role": {user.RoleSuperAdmin}, "is_active": {"1"}}
	if rec := f.do(mfaRequest{method: http.MethodPost, path: "/admin/users/3/edit", token: session, form: promote}); rec.Code != http.StatusOK {
		t.Fatalf("promote without a code = %d", rec.Code)
	}
	if after, _ := f.h.svc.userRepo.FindByID(3); after.Role != user.RoleEditor {
		t.Fatal("promoted without a code")
	}
	// An admin without two-step verification is asked for nothing.
	plain := f.session(2, time.Time{})
	if rec := f.do(mfaRequest{method: http.MethodPost, path: "/admin/users/3/edit", token: plain, form: promote}); rec.Code != http.StatusFound {
		t.Fatalf("admin without two-step verification = %d", rec.Code)
	}
}

func TestResetSomeoneElsesSecondFactor(t *testing.T) {
	f := newMFAFixture(t)
	_, _, targetSession := f.enroll(3)
	secret, _, adminSession := f.enroll(1)
	// User 3 is an editor: no user.update.
	editor := targetSession
	if rec := f.do(mfaRequest{method: http.MethodPost, path: "/admin/users/1/mfa-reset", token: editor}); rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "error=") {
		t.Fatalf("editor = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if _, on, _ := f.m.EnabledAt(1); !on {
		t.Fatal("an editor reset a super admin")
	}
	if rec := f.do(mfaRequest{method: http.MethodPost, path: "/admin/users/3/mfa-reset", token: adminSession}); !strings.Contains(rec.Header().Get("Location"), "error=") {
		t.Fatalf("without a code = %s", rec.Header().Get("Location"))
	}
	rec := f.do(mfaRequest{method: http.MethodPost, path: "/admin/users/3/mfa-reset", token: adminSession, form: url.Values{OTPField: {f.code(secret)}}})
	if !strings.Contains(rec.Header().Get("Location"), "success=") {
		t.Fatalf("with a code = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if _, on, _ := f.m.EnabledAt(3); on {
		t.Fatal("still on")
	}
	if _, err := f.auth.ActiveClaims(targetSession); err == nil {
		t.Fatal("the target's sessions must end")
	}
	if got := f.audited("mfa_reset"); len(got) != 1 {
		t.Fatalf("audit = %v", auditActions(t, f.log))
	}
}

func TestRequireSecondFactorForPluginRoutes(t *testing.T) {
	f := newMFAFixture(t)
	router := gin.New()
	// The first handler stands in for RequirePermission.
	router.POST("/admin/publish/:uid", func(c *gin.Context) {
		if c.Param("uid") == "1" {
			c.Set("admin_user_id", uint(1))
		} else {
			c.Set("admin_user_id", uint(2))
		}
	}, RequireSecondFactor(f.m, f.log), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	post := func(uid, header, field string) *httptest.ResponseRecorder {
		form := url.Values{}
		if field != "" {
			form.Set(OTPField, field)
		}
		req := httptest.NewRequest(http.MethodPost, "/admin/publish/"+uid, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if header != "" {
			req.Header.Set(OTPHeader, header)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	if rec := post("2", "123456", ""); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), codeNotEnrolled) {
		t.Fatalf("not enrolled = %d %s", rec.Code, rec.Body.String())
	}
	secret, _, _ := f.enroll(1)
	if rec := post("1", "000000", ""); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), codeInvalid) {
		t.Fatalf("wrong code = %d %s", rec.Code, rec.Body.String())
	}
	code := f.code(secret)
	if rec := post("1", code, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("header code = %d %s", rec.Code, rec.Body.String())
	}
	if rec := post("1", "", code); rec.Code != http.StatusForbidden {
		t.Fatalf("the same code again = %d", rec.Code)
	}
	f.tick()
	if rec := post("1", "", f.code(secret)); rec.Code != http.StatusNoContent {
		t.Fatalf("form code = %d %s", rec.Code, rec.Body.String())
	}
	for i := 0; i < mfa.MaxFailures; i++ {
		post("1", "000000", "")
	}
	if rec := post("1", "000000", ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("after %d wrong codes = %d", mfa.MaxFailures, rec.Code)
	}
	if got := f.audited("second_factor_failed"); len(got) < 2 {
		t.Fatalf("wrong codes must be audited: %v", got)
	}
}

func TestDeveloperModeAsksForACodeWhenOn(t *testing.T) {
	fp := &fakePackages{}
	h, _ := extensionTestHandler(t, fp)
	if err := h.svc.db.Exec(`CREATE TABLE gp_user_mfa (user_id INTEGER PRIMARY KEY, totp_secret TEXT NOT NULL DEFAULT '',
		pending_secret TEXT NOT NULL DEFAULT '', pending_expires_at DATETIME, key_id TEXT NOT NULL DEFAULT '',
		enabled_at DATETIME, last_step INTEGER NOT NULL DEFAULT 0, recovery_hashes TEXT NOT NULL DEFAULT '',
		recovery_generated_at DATETIME, failed_count INTEGER NOT NULL DEFAULT 0, failed_since DATETIME,
		locked_until DATETIME, sessions_not_before DATETIME, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`).Error; err != nil {
		t.Fatal(err)
	}
	m, err := mfa.New(h.svc.db, mfaTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Now().UTC().Truncate(30 * time.Second).Add(time.Second)
	m.SetClock(func() time.Time { return clock })
	h.svc.SetMFA(m)
	e, _ := m.Begin(context.Background(), 1, "GoPress", "admin")
	first, _ := mfa.CodeAt(e.Secret, clock)
	if _, _, err := m.Confirm(context.Background(), 1, first); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(30 * time.Second)
	router := extensionRouter(h, user.RoleSuperAdmin, 1)
	form := url.Values{"enabled": {"1"}, "confirm": {"1"}, "password": {testPassword}}
	if rec := postForm(router, "/admin/settings/developer-mode", form); rec.Code != http.StatusForbidden || errorCode(t, rec) != codeInvalid {
		t.Fatalf("without a code = %d %s", rec.Code, rec.Body.String())
	}
	if fp.mode.Enabled {
		t.Fatal("turned on without a code")
	}
	code, _ := mfa.CodeAt(e.Secret, clock)
	form.Set(OTPField, code)
	if rec := postForm(router, "/admin/settings/developer-mode", form); rec.Code != http.StatusOK || !fp.mode.Enabled {
		t.Fatalf("with a code = %d %s", rec.Code, rec.Body.String())
	}
}

func TestSecurityNoticesForTheCurrentUser(t *testing.T) {
	f := newMFAFixture(t)
	path := "/admin/"
	ids := func(role string, userID uint) []string {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, path, nil)
		c.Set("admin_role", role)
		c.Set("admin_user_id", userID)
		var out []string
		for _, n := range f.h.securityNotices(c, "en") {
			out = append(out, n.ID)
		}
		return out
	}
	if got := ids(user.RoleSuperAdmin, 1); len(got) != 1 || got[0] != "turn-on-two-step" {
		t.Fatalf("super admin without it = %v", got)
	}
	path = AccountSecurityPath
	if got := ids(user.RoleSuperAdmin, 1); len(got) != 0 {
		t.Fatalf("on the setup page itself = %v", got)
	}
	path = "/admin/"
	if got := ids(user.RoleEditor, 3); len(got) != 0 {
		t.Fatalf("editor = %v; the suggestion is for those who manage the site", got)
	}
	_, codes, _ := f.enroll(1)
	if got := ids(user.RoleSuperAdmin, 1); len(got) != 0 {
		t.Fatalf("with ten codes = %v", got)
	}
	for _, code := range codes[:mfa.RecoveryCodeCount-2] {
		if _, err := f.m.Verify(context.Background(), 1, code, true); err != nil {
			t.Fatal(err)
		}
	}
	if got := ids(user.RoleSuperAdmin, 1); len(got) != 1 || got[0] != "recovery-codes-low" {
		t.Fatalf("with two codes left = %v", got)
	}
}

// VerifySecondFactor checks like RequireSecondFactor and leaves the answer
// to the handler.
func TestVerifySecondFactorForPageHandlers(t *testing.T) {
	f := newMFAFixture(t)
	check := func(userID uint, code string) string {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/x", strings.NewReader(url.Values{OTPField: {code}}.Encode()))
		c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		c.Set("admin_user_id", userID)
		return VerifySecondFactor(c, f.m, f.log)
	}
	if got := check(2, "123456"); got != codeNotEnrolled {
		t.Fatalf("not enrolled = %q", got)
	}
	secret, _, _ := f.enroll(1)
	if got := check(1, "000000"); got != codeInvalid {
		t.Fatalf("wrong code = %q", got)
	}
	if got := check(1, f.code(secret)); got != "" {
		t.Fatalf("right code = %q", got)
	}
	if got := VerifySecondFactor(&gin.Context{}, nil, nil); got != codeUnavailable {
		t.Fatalf("without the service = %q", got)
	}
}
