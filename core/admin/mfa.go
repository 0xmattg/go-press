// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

// Two-step verification in the admin (core/mfa).
//
// Signing in: when the account has it on, a correct password does not start
// a session. LoginSubmit sets a five-minute ticket cookie and the code step
// (LoginVerifyPage, LoginVerifySubmit) starts the session once a code from
// the app or a recovery code is accepted. The session records the time
// two-step verification was turned on, and user.Auth ends sessions that do
// not match it.
//
// The account security page lets every admin user set it up for their own
// account, replace recovery codes and turn it off, and lets those who may
// change settings choose roles that must use it. Accounts with such a role
// can only open that page until they set it up (see blockUntilSecondFactor).
//
// Sensitive actions ask for a fresh code: core's own through stepUp, which
// asks only accounts that have two-step verification on, and plugin routes
// through RequireSecondFactor, which refuses accounts without it.

package admin

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/0xmattg/go-press/core/audit"
	coreMail "github.com/0xmattg/go-press/core/mail"
	"github.com/0xmattg/go-press/core/mfa"
	"github.com/0xmattg/go-press/core/notice"
	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/core/user"
	"github.com/0xmattg/go-press/pkg/middleware"

	"github.com/gin-gonic/gin"
)

const (
	// AccountSecurityPath is the account security page.
	AccountSecurityPath = "/admin/account/security"
	// OptionSecondFactorRoles lists, comma-separated, the roles that must
	// use two-step verification.
	OptionSecondFactorRoles = "admin_mfa_required_roles"
	// OTPField is the form field, and OTPHeader the request header, that
	// carry a second-factor code for a sensitive action.
	OTPField  = "otp_code"
	OTPHeader = "X-GoPress-OTP"

	loginVerifyPath    = "/admin/login/verify"
	adminMFACookie     = "admin_mfa"
	adminMFACookiePath = "/admin/login"
	// lowRecoveryCodes is when the notice center starts reminding a user
	// to replace their recovery codes.
	lowRecoveryCodes = 3
)

// Second-factor error codes, in JSON errors and as message keys
// ("mfa.error.<code>").
const (
	codeNotEnrolled = "second_factor_not_enrolled"
	codeInvalid     = "second_factor_invalid"
	codeLocked      = "second_factor_locked"
	codeUnavailable = "second_factor_unavailable"
	codeSetup       = "second_factor_setup_required"
)

// SetMFA wires two-step verification into the admin.
func (s *Service) SetMFA(m *mfa.Service) { s.secondFactor = m }

// Authenticate checks a username and password without starting a session.
func (s *Service) Authenticate(username, password string) (*user.User, error) {
	u, err := s.auth.Authenticate(username, password)
	if err != nil {
		return nil, fmt.Errorf("invalid username or password")
	}
	return u, nil
}

// SecondFactorRequiredRoles returns the roles that must use two-step
// verification.
func SecondFactorRequiredRoles(opts *option.Store) map[string]bool {
	out := map[string]bool{}
	if opts == nil {
		return out
	}
	for _, role := range strings.Split(opts.Get(OptionSecondFactorRoles), ",") {
		if role = strings.TrimSpace(role); role != "" {
			out[role] = true
		}
	}
	return out
}

// SecondFactorPolicy is the policy user.Auth applies: whether a role must
// use two-step verification.
func SecondFactorPolicy(opts *option.Store) func(role string) bool {
	return func(role string) bool { return SecondFactorRequiredRoles(opts)[role] }
}

// secondFactorEnabled reports whether the user has two-step verification on.
func (h *Handler) secondFactorEnabled(userID uint) (time.Time, bool, error) {
	if h.svc == nil || h.svc.secondFactor == nil || userID == 0 {
		return time.Time{}, false, nil
	}
	return h.svc.secondFactor.EnabledAt(userID)
}

// ==================== Signing in ====================

// beginSecondStep sends a user whose password was accepted to the code step.
func (h *Handler) beginSecondStep(c *gin.Context, u *user.User) {
	ticket, _, err := h.svc.secondFactor.SignPending(u.ID)
	if err != nil {
		log.Printf("admin login: second step for user %d: %v", u.ID, err)
		h.render(c, "login", gin.H{"Title": adminT(h.svc.AdminLanguage(), "page.login"), "Error": adminT(h.svc.AdminLanguage(), "mfa.error.second_factor_unavailable")})
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(adminMFACookie, ticket, int(mfa.PendingTTL/time.Second), adminMFACookiePath, "", h.svc.secureCookies(), true)
	c.Redirect(http.StatusFound, loginVerifyPath)
}

func (h *Handler) clearSecondStep(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(adminMFACookie, "", -1, adminMFACookiePath, "", h.svc.secureCookies(), true)
}

// pendingUser returns the user a valid second-step ticket names, or 0.
func (h *Handler) pendingUser(c *gin.Context) uint {
	if h.svc.secondFactor == nil {
		return 0
	}
	ticket, err := c.Cookie(adminMFACookie)
	if err != nil || ticket == "" {
		return 0
	}
	userID, err := h.svc.secondFactor.ParsePending(ticket)
	if err != nil {
		return 0
	}
	return userID
}

func (h *Handler) restartLogin(c *gin.Context) {
	h.clearSecondStep(c)
	c.Redirect(http.StatusFound, "/admin/login?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "mfa.error.sign_in_again")))
}

func (h *Handler) renderLoginVerify(c *gin.Context, status int, errText string) {
	lang := h.svc.AdminLanguage()
	if status != http.StatusOK {
		c.Status(status)
	}
	h.render(c, "login_mfa", gin.H{
		"Title":    adminT(lang, "mfa.login_title"),
		"Error":    errText,
		"Recovery": c.Query("recovery") == "1" || c.PostForm("recovery") == "1",
	})
}

// LoginVerifyPage shows the code step of signing in.
func (h *Handler) LoginVerifyPage(c *gin.Context) {
	if h.pendingUser(c) == 0 {
		h.restartLogin(c)
		return
	}
	h.renderLoginVerify(c, http.StatusOK, "")
}

// LoginVerifySubmit checks the code of the second step and starts the
// session. A recovery code is accepted here and then used up.
func (h *Handler) LoginVerifySubmit(c *gin.Context) {
	if middleware.IsStateChangingMethod(c.Request.Method) && !middleware.IsSameOrigin(c.Request) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	lang := h.svc.AdminLanguage()
	clientIP := c.ClientIP()
	if h.loginThrottle.blocked(clientIP) {
		h.renderLoginVerify(c, http.StatusTooManyRequests, adminT(lang, "error.login_throttled"))
		return
	}
	userID := h.pendingUser(c)
	if userID == 0 {
		h.restartLogin(c)
		return
	}
	method, err := h.svc.secondFactor.Verify(c.Request.Context(), userID, c.PostForm("code"), true)
	switch {
	case errors.Is(err, mfa.ErrNotEnrolled):
		// Turned off or reset meanwhile: start again with the password.
		h.restartLogin(c)
		return
	case errors.Is(err, mfa.ErrLocked):
		h.svc.LogActionContext(c.Request.Context(), userID, "", "login_failed", "auth", 0, "two-step verification locked after too many wrong codes", clientIP)
		h.renderLoginVerify(c, http.StatusTooManyRequests, adminT(lang, "mfa.error.second_factor_locked"))
		return
	case err != nil:
		h.loginThrottle.fail(clientIP)
		if !errors.Is(err, mfa.ErrInvalidCode) {
			log.Printf("admin login: verify second factor for user %d: %v", userID, err)
		}
		h.svc.LogActionContext(c.Request.Context(), userID, "", "login_failed", "auth", 0, "wrong two-step verification code", clientIP)
		h.renderLoginVerify(c, http.StatusUnauthorized, adminT(lang, "mfa.error.second_factor_invalid"))
		return
	}

	account, err := h.svc.userRepo.FindByID(userID)
	if err != nil || !account.IsActive {
		h.restartLogin(c)
		return
	}
	enabledAt, on, err := h.secondFactorEnabled(userID)
	if err != nil || !on {
		h.restartLogin(c)
		return
	}
	token, err := h.svc.auth.IssueSession(account, enabledAt)
	if err != nil {
		log.Printf("admin login: issue session for user %d: %v", userID, err)
		h.renderLoginVerify(c, http.StatusInternalServerError, adminT(lang, "mfa.error.second_factor_unavailable"))
		return
	}
	h.clearSecondStep(c)
	h.loginThrottle.reset(clientIP)
	writeAdminCookie(c, token, h.svc.secureCookies())
	detail := "user login with an authenticator code"
	if method == mfa.MethodRecovery {
		detail = "user login with a recovery code"
		h.securityMail(c, account, "recovery_used")
	}
	h.svc.LogActionContext(c.Request.Context(), account.ID, account.Username, "login", "auth", 0, detail, clientIP)
	c.Redirect(http.StatusFound, "/admin/")
}

// ==================== Setup and policy gate ====================

// secondFactorSetupAllowed reports the paths an account that must set up
// two-step verification may open before it does.
func secondFactorSetupAllowed(path string) bool {
	return path == AccountSecurityPath || strings.HasPrefix(path, AccountSecurityPath+"/") || path == "/admin/logout"
}

// blockUntilSecondFactor stops an account that must set up two-step
// verification from using anything but the setup page: page requests go
// there, others get 403. It reports whether it stopped the request.
func blockUntilSecondFactor(c *gin.Context, claims *user.Claims) bool {
	if claims == nil || !claims.SecondFactorSetupRequired || secondFactorSetupAllowed(c.Request.URL.Path) {
		return false
	}
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
		c.Redirect(http.StatusFound, AccountSecurityPath+"?required=1")
		c.Abort()
		return true
	}
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"ok": false, "error": gin.H{"code": codeSetup}})
	return true
}

// ==================== Sensitive actions ====================

// secondFactorCode reads the code of a sensitive action from the header or
// the form.
func secondFactorCode(c *gin.Context) string {
	if code := strings.TrimSpace(c.GetHeader(OTPHeader)); code != "" {
		return code
	}
	return c.PostForm(OTPField)
}

// checkSecondFactor verifies the code of a sensitive action for the
// current user. required refuses users without two-step verification; when
// it is not set they pass. It returns "" or an error code, and audits a
// wrong code.
func checkSecondFactor(c *gin.Context, m *mfa.Service, log *audit.Service, required bool) string {
	userID := adminUserID(c)
	if m == nil || userID == 0 {
		if required {
			return codeUnavailable
		}
		return ""
	}
	_, on, err := m.EnabledAt(userID)
	if err != nil {
		return codeUnavailable
	}
	if !on {
		if required {
			return codeNotEnrolled
		}
		return ""
	}
	_, err = m.Verify(c.Request.Context(), userID, secondFactorCode(c), false)
	if err == nil {
		return ""
	}
	code := codeInvalid
	if errors.Is(err, mfa.ErrLocked) {
		code = codeLocked
	} else if !errors.Is(err, mfa.ErrInvalidCode) {
		code = codeUnavailable
	}
	if log != nil {
		_ = log.Record(c.Request.Context(), &audit.Event{
			UserID: userID, Username: c.GetString("admin_username"), Action: "second_factor_failed",
			Resource: "auth", Details: c.Request.Method + " " + c.Request.URL.Path, IPAddress: c.ClientIP(),
		})
	}
	return code
}

// stepUp asks for a fresh code when the current user has two-step
// verification on. It returns "" or an error code.
func (h *Handler) stepUp(c *gin.Context) string {
	return checkSecondFactor(c, h.svc.secondFactor, h.svc.audit, false)
}

func secondFactorStatus(code string) int {
	switch code {
	case codeLocked:
		return http.StatusTooManyRequests
	case codeUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusForbidden
	}
}

// VerifySecondFactor is RequireSecondFactor for a handler that answers
// with a page rather than JSON: it checks the code of the current request
// the same way and returns "" or the error code, for the handler to show.
// The user must have two-step verification on. Call it after
// RequirePermission.
func VerifySecondFactor(c *gin.Context, m *mfa.Service, log *audit.Service) string {
	return checkSecondFactor(c, m, log, true)
}

// RequireSecondFactor protects a sensitive extension route: the signed-in
// user must have two-step verification on and send a fresh code in the
// X-GoPress-OTP header or the otp_code form field, every time. Place it
// after RequirePermission. A refusal is JSON {"ok": false, "error": {"code":
// ...}} with second_factor_not_enrolled (403), second_factor_invalid (403),
// second_factor_locked (429) or second_factor_unavailable (503). Each code
// works once, so two actions in the same 30 seconds need two codes. Plugins
// get it from Engine.RequireSecondFactor.
func RequireSecondFactor(m *mfa.Service, log *audit.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if code := checkSecondFactor(c, m, log, true); code != "" {
			c.AbortWithStatusJSON(secondFactorStatus(code), gin.H{"ok": false, "error": gin.H{"code": code}})
			return
		}
		c.Next()
	}
}

// ==================== Account security page ====================

// accountSecurityView is the account security page.
type accountSecurityView struct {
	Status        mfa.Status
	Required      bool
	SetupRequired bool
	Enrollment    *mfa.Enrollment
	// QRImage and AppLink are the enrollment's data: image and otpauth://
	// link. Both are built by core/mfa, so the page may use them as URLs.
	QRImage       template.URL
	AppLink       template.URL
	RecoveryCodes []string
	RecoveryFile  template.URL
	RecoveryTotal int
	Policy        *securityPolicyView
}

type securityPolicyView struct {
	Roles []securityPolicyRole
	// SecondFactor asks for a code to save the policy.
	SecondFactor bool
}

type securityPolicyRole struct {
	Name    string
	Checked bool
}

func (h *Handler) currentAccount(c *gin.Context) (*user.User, bool) {
	account, err := h.svc.userRepo.FindByID(adminUserID(c))
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/login")
		c.Abort()
		return nil, false
	}
	return account, true
}

// renderAccountSecurity shows the page with the current state plus what
// the action that led here adds (a setup in progress, new recovery codes).
func (h *Handler) renderAccountSecurity(c *gin.Context, view accountSecurityView, errText string) {
	lang := h.svc.AdminLanguage()
	userID := adminUserID(c)
	if h.svc.secondFactor != nil {
		status, err := h.svc.secondFactor.Status(c.Request.Context(), userID)
		if err != nil {
			log.Printf("account security: status for user %d: %v", userID, err)
			errText = adminT(lang, "mfa.error.second_factor_unavailable")
		}
		view.Status = status
	}
	role := c.GetString("admin_role")
	view.Required = SecondFactorRequiredRoles(h.svc.options)[role]
	view.SetupRequired = view.Required && !view.Status.Enabled
	view.RecoveryTotal = mfa.RecoveryCodeCount
	if view.Enrollment != nil {
		view.QRImage = template.URL(view.Enrollment.QRCode)
		view.AppLink = template.URL(view.Enrollment.URI)
	}
	if len(view.RecoveryCodes) > 0 {
		text := adminT(lang, "mfa.recovery_file_header", h.svc.SiteName(), c.GetString("admin_username")) + "\n\n" + strings.Join(view.RecoveryCodes, "\n") + "\n"
		view.RecoveryFile = template.URL("data:text/plain;charset=utf-8," + url.PathEscape(text))
	}
	if h.can(c, "setting", "update") {
		required := SecondFactorRequiredRoles(h.svc.options)
		policy := &securityPolicyView{SecondFactor: view.Status.Enabled}
		for _, r := range h.svc.GetRoles() {
			policy.Roles = append(policy.Roles, securityPolicyRole{Name: r, Checked: required[r]})
		}
		view.Policy = policy
	}
	data := gin.H{
		"Title":    adminT(lang, "mfa.page_title"),
		"Active":   "account",
		"Security": view,
	}
	if errText != "" {
		data["Error"] = errText
	}
	if c.Query("required") == "1" && view.SetupRequired {
		data["Error"] = adminT(lang, "mfa.required_notice")
	}
	h.render(c, "account_security", data)
}

// AccountSecurity shows the account security page. Every admin user may
// open it for their own account.
func (h *Handler) AccountSecurity(c *gin.Context) {
	if !h.checkPermission(c, "profile", "update") {
		return
	}
	h.renderAccountSecurity(c, accountSecurityView{}, "")
}

// recheckPassword confirms the signed-in user's password, with a per-account
// limit on wrong attempts. It returns "" or a message.
func (h *Handler) recheckPassword(c *gin.Context, account *user.User, password string) string {
	lang := h.svc.AdminLanguage()
	key := "account:" + strconv.FormatUint(uint64(account.ID), 10)
	if h.reauthThrottle.blocked(key) {
		return adminT(lang, "mfa.error.too_many_passwords")
	}
	if account.PasswordHash == "" {
		return adminT(lang, "mfa.error.no_password")
	}
	if !user.CheckPassword(account.PasswordHash, password) {
		h.reauthThrottle.fail(key)
		h.logAction(c, "reauth_failed", "auth", account.ID, "wrong password on the account security page")
		return adminT(lang, "mfa.error.password")
	}
	h.reauthThrottle.reset(key)
	return ""
}

func (h *Handler) requireSecondFactorService(c *gin.Context) bool {
	if h.svc.secondFactor != nil {
		return true
	}
	h.renderAccountSecurity(c, accountSecurityView{}, adminT(h.svc.AdminLanguage(), "mfa.error.second_factor_unavailable"))
	return false
}

// AccountSecurityBegin starts a setup after the password is confirmed and
// shows the QR code. The secret is in this response only.
func (h *Handler) AccountSecurityBegin(c *gin.Context) {
	if !h.checkPermission(c, "profile", "update") || !h.requireSecondFactorService(c) {
		return
	}
	account, ok := h.currentAccount(c)
	if !ok {
		return
	}
	if msg := h.recheckPassword(c, account, c.PostForm("password")); msg != "" {
		h.renderAccountSecurity(c, accountSecurityView{}, msg)
		return
	}
	enrollment, err := h.svc.secondFactor.Begin(c.Request.Context(), account.ID, h.secondFactorIssuer(c), account.Username)
	if err != nil {
		msg := adminT(h.svc.AdminLanguage(), "mfa.error.second_factor_unavailable")
		if errors.Is(err, mfa.ErrAlreadyEnabled) {
			msg = adminT(h.svc.AdminLanguage(), "mfa.error.already_on")
		} else {
			log.Printf("account security: begin for user %d: %v", account.ID, err)
		}
		h.renderAccountSecurity(c, accountSecurityView{}, msg)
		return
	}
	h.renderAccountSecurity(c, accountSecurityView{Enrollment: enrollment}, "")
}

// secondFactorIssuer names the site in the authenticator app:
// "GoPress (example.com)".
func (h *Handler) secondFactorIssuer(c *gin.Context) string {
	host := ""
	if h.svc.config != nil {
		if u, err := url.Parse(strings.TrimSpace(h.svc.config.Site.URL)); err == nil {
			host = u.Hostname()
		}
	}
	if host == "" {
		host = c.Request.Host
		if i := strings.LastIndex(host, ":"); i > 0 && !strings.Contains(host[i:], "]") {
			host = host[:i]
		}
	}
	if host == "" {
		return "GoPress"
	}
	return "GoPress (" + host + ")"
}

// AccountSecurityConfirm turns two-step verification on with the first
// code, keeps this session signed in, ends the others, and shows the
// recovery codes once.
func (h *Handler) AccountSecurityConfirm(c *gin.Context) {
	if !h.checkPermission(c, "profile", "update") || !h.requireSecondFactorService(c) {
		return
	}
	account, ok := h.currentAccount(c)
	if !ok {
		return
	}
	lang := h.svc.AdminLanguage()
	codes, enabledAt, err := h.svc.secondFactor.Confirm(c.Request.Context(), account.ID, c.PostForm("code"))
	if err != nil {
		msg := adminT(lang, "mfa.error.second_factor_invalid")
		switch {
		case errors.Is(err, mfa.ErrNoPending):
			msg = adminT(lang, "mfa.error.setup_expired")
		case errors.Is(err, mfa.ErrAlreadyEnabled):
			msg = adminT(lang, "mfa.error.already_on")
		case errors.Is(err, mfa.ErrLocked):
			msg = adminT(lang, "mfa.error.second_factor_locked")
		case !errors.Is(err, mfa.ErrInvalidCode):
			log.Printf("account security: confirm for user %d: %v", account.ID, err)
			msg = adminT(lang, "mfa.error.second_factor_unavailable")
		}
		h.renderAccountSecurity(c, accountSecurityView{}, msg)
		return
	}
	token, err := h.svc.auth.RenewSession(account, enabledAt)
	if err == nil {
		writeAdminCookie(c, token, h.svc.secureCookies())
	} else {
		log.Printf("account security: renew session for user %d: %v", account.ID, err)
	}
	// This request carried the old session; the next ones carry the new.
	c.Set("admin_second_factor", true)
	h.logAction(c, "mfa_enable", "auth", account.ID, "two-step verification turned on")
	h.renderAccountSecurity(c, accountSecurityView{RecoveryCodes: codes}, "")
}

// AccountSecurityRecovery replaces the recovery codes after a code from the
// app.
func (h *Handler) AccountSecurityRecovery(c *gin.Context) {
	if !h.checkPermission(c, "profile", "update") || !h.requireSecondFactorService(c) {
		return
	}
	account, ok := h.currentAccount(c)
	if !ok {
		return
	}
	lang := h.svc.AdminLanguage()
	if code := checkSecondFactor(c, h.svc.secondFactor, h.svc.audit, true); code != "" {
		h.renderAccountSecurity(c, accountSecurityView{}, adminT(lang, "mfa.error."+code))
		return
	}
	codes, err := h.svc.secondFactor.RegenerateRecovery(c.Request.Context(), account.ID)
	if err != nil {
		log.Printf("account security: recovery codes for user %d: %v", account.ID, err)
		h.renderAccountSecurity(c, accountSecurityView{}, adminT(lang, "mfa.error.second_factor_unavailable"))
		return
	}
	h.logAction(c, "mfa_recovery_regenerate", "auth", account.ID, "recovery codes replaced")
	h.securityMail(c, account, "recovery_regenerated")
	h.renderAccountSecurity(c, accountSecurityView{RecoveryCodes: codes}, "")
}

// AccountSecurityDisable turns two-step verification off after the password
// and a code from the app or a recovery code. Accounts whose role must use
// it cannot turn it off.
func (h *Handler) AccountSecurityDisable(c *gin.Context) {
	if !h.checkPermission(c, "profile", "update") || !h.requireSecondFactorService(c) {
		return
	}
	account, ok := h.currentAccount(c)
	if !ok {
		return
	}
	lang := h.svc.AdminLanguage()
	if SecondFactorRequiredRoles(h.svc.options)[c.GetString("admin_role")] {
		h.renderAccountSecurity(c, accountSecurityView{}, adminT(lang, "mfa.error.required_role"))
		return
	}
	if msg := h.recheckPassword(c, account, c.PostForm("password")); msg != "" {
		h.renderAccountSecurity(c, accountSecurityView{}, msg)
		return
	}
	if _, err := h.svc.secondFactor.Verify(c.Request.Context(), account.ID, c.PostForm("code"), true); err != nil {
		code := codeInvalid
		switch {
		case errors.Is(err, mfa.ErrLocked):
			code = codeLocked
		case errors.Is(err, mfa.ErrNotEnrolled):
			c.Redirect(http.StatusFound, AccountSecurityPath)
			return
		case !errors.Is(err, mfa.ErrInvalidCode):
			code = codeUnavailable
		}
		h.logAction(c, "second_factor_failed", "auth", account.ID, "turning two-step verification off")
		h.renderAccountSecurity(c, accountSecurityView{}, adminT(lang, "mfa.error."+code))
		return
	}
	if err := h.svc.secondFactor.Remove(c.Request.Context(), account.ID); err != nil {
		log.Printf("account security: turn off for user %d: %v", account.ID, err)
		h.renderAccountSecurity(c, accountSecurityView{}, adminT(lang, "mfa.error.second_factor_unavailable"))
		return
	}
	h.logAction(c, "mfa_disable", "auth", account.ID, "two-step verification turned off")
	h.securityMail(c, account, "disabled")
	c.Redirect(http.StatusFound, AccountSecurityPath+"?success="+url.QueryEscape(adminT(lang, "mfa.disabled")))
}

// AccountSecurityPolicy saves the roles that must use two-step
// verification. It needs setting.update and, from an account with two-step
// verification on, a code.
func (h *Handler) AccountSecurityPolicy(c *gin.Context) {
	if !h.checkPermission(c, "setting", "update") {
		return
	}
	lang := h.svc.AdminLanguage()
	if code := h.stepUp(c); code != "" {
		h.renderAccountSecurity(c, accountSecurityView{}, adminT(lang, "mfa.error."+code))
		return
	}
	known := map[string]bool{}
	for _, r := range h.svc.GetRoles() {
		known[r] = true
	}
	var roles []string
	for _, r := range c.PostFormArray("required_roles") {
		if known[r] && !containsString(roles, r) {
			roles = append(roles, r)
		}
	}
	sort.Strings(roles)
	value := strings.Join(roles, ",")
	if h.svc.options == nil || h.svc.options.Set(OptionSecondFactorRoles, value) != nil {
		h.renderAccountSecurity(c, accountSecurityView{}, adminT(lang, "mfa.error.second_factor_unavailable"))
		return
	}
	h.logAction(c, "update", "mfa_policy", 0, "roles that must use two-step verification: "+value)
	c.Redirect(http.StatusFound, AccountSecurityPath+"?success="+url.QueryEscape(adminT(lang, "notice.settings_updated")))
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// UserMFAReset turns another user's two-step verification off, for someone
// who lost their phone and recovery codes. It needs user.update, a role at
// least as high as the target's and, from an account with two-step
// verification on, a code. The target sets it up again; their sessions end.
func (h *Handler) UserMFAReset(c *gin.Context) {
	if !h.checkPermission(c, "user", "update") {
		return
	}
	lang := h.svc.AdminLanguage()
	id := getIDParam(c)
	editURL := "/admin/users/" + strconv.FormatUint(uint64(id), 10) + "/edit"
	target, err := h.svc.userRepo.FindByID(id)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/users?error="+url.QueryEscape(adminT(lang, "error.not_found")))
		return
	}
	if target.ID == adminUserID(c) {
		c.Redirect(http.StatusFound, AccountSecurityPath)
		return
	}
	if h.svc.rbac.RoleLevel(c.GetString("admin_role")) < h.svc.rbac.RoleLevel(target.Role) {
		c.Redirect(http.StatusFound, editURL+"?error="+url.QueryEscape(adminT(lang, "error.permission_denied")))
		return
	}
	if code := h.stepUp(c); code != "" {
		c.Redirect(http.StatusFound, editURL+"?error="+url.QueryEscape(adminT(lang, "mfa.error."+code)))
		return
	}
	if h.svc.secondFactor == nil || h.svc.secondFactor.Reset(c.Request.Context(), target.ID) != nil {
		c.Redirect(http.StatusFound, editURL+"?error="+url.QueryEscape(adminT(lang, "mfa.error.second_factor_unavailable")))
		return
	}
	h.logAction(c, "mfa_reset", "user", target.ID, target.Username)
	h.securityMail(c, target, "reset")
	c.Redirect(http.StatusFound, editURL+"?success="+url.QueryEscape(adminT(lang, "mfa.reset_done")))
}

// ==================== Notices and mail ====================

// securityNotices are the notice center's core.security notices for the
// current user: a second factor that no longer opens, few recovery codes
// left, and, for those who may change settings, a suggestion to turn
// two-step verification on (except on the page that does it).
func (h *Handler) securityNotices(c *gin.Context, lang string) []notice.Notice {
	userID := adminUserID(c)
	if h.svc == nil || h.svc.secondFactor == nil || userID == 0 {
		return nil
	}
	status, err := h.svc.secondFactor.Status(c.Request.Context(), userID)
	if err != nil {
		return nil
	}
	link := &notice.Link{URL: AccountSecurityPath, Label: adminT(lang, "mfa.notice.open")}
	base := notice.Notice{Source: "core.security", Category: notice.CategorySecurity, Link: link, Resource: "profile", Action: "update"}
	var out []notice.Notice
	switch {
	case status.Enabled && status.Unreadable:
		n := base
		n.ID, n.Severity, n.Snoozable = "second-factor-unreadable", notice.SeverityCritical, true
		n.Title, n.Message = adminT(lang, "mfa.notice.unreadable_title"), adminT(lang, "mfa.notice.unreadable_message")
		out = append(out, n)
	case status.Enabled && status.RecoveryLeft < lowRecoveryCodes:
		n := base
		n.ID, n.Severity, n.Snoozable, n.Acknowledgeable = "recovery-codes-low", notice.SeverityWarning, true, true
		// A new revision for each code used, so the reminder comes back.
		n.Revision = mfa.RecoveryCodeCount - status.RecoveryLeft
		n.Title = adminT(lang, "mfa.notice.recovery_low_title", status.RecoveryLeft)
		n.Message = adminT(lang, "mfa.notice.recovery_low_message")
		out = append(out, n)
	case !status.Enabled && h.can(c, "setting", "update") && !secondFactorSetupAllowed(c.Request.URL.Path):
		// Not on the account security page itself, where the setup is.
		n := base
		n.ID, n.Severity, n.Snoozable, n.Acknowledgeable = "turn-on-two-step", notice.SeverityInfo, true, true
		n.RemindAfter = 7 * 24 * time.Hour
		n.Title, n.Message = adminT(lang, "mfa.notice.suggest_title"), adminT(lang, "mfa.notice.suggest_message")
		out = append(out, n)
	}
	return out
}

// securityMail tells the account owner about a change to their two-step
// verification by email, when mail is set up and the account has an
// address. It does not wait for delivery.
func (h *Handler) securityMail(c *gin.Context, account *user.User, event string) {
	if h.svc == nil || h.svc.mailer == nil || account == nil || account.Email == nil || strings.TrimSpace(*account.Email) == "" {
		return
	}
	lang := h.svc.AdminLanguage()
	site := h.svc.SiteName()
	when := time.Now().UTC().Format("2006-01-02 15:04 UTC")
	msg := coreMail.Message{
		To:      []string{strings.TrimSpace(*account.Email)},
		Subject: adminT(lang, "mfa.mail."+event+"_subject", site),
		Text: adminT(lang, "mfa.mail."+event+"_body", account.Username, site, when, c.ClientIP()) +
			"\n\n" + adminT(lang, "mfa.mail.footer"),
	}
	mailer := h.svc.mailer
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := mailer.Send(ctx, msg); err != nil && !errors.Is(err, coreMail.ErrDisabled) && !errors.Is(err, coreMail.ErrNotConfigured) {
			log.Printf("account security: mail about %s for user %d: %v", event, account.ID, err)
		}
	}()
}
