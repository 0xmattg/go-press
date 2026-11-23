// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

// Admin notice center.
//
// Sources add notices through the hook.AdminNotices filter. Every admin page
// shows, at the top, the notices the current user's role may read and that
// the user has not dismissed: up to three open, the rest folded under
// "N more notices". A user dismisses a notice for themselves only, with "×"
// (snooze: it comes back after its RemindAfter if it still applies) or "Got
// it" (acknowledge: this revision stays hidden). Dismissals are stored per
// user in the option noticeDismissedOptionPrefix+<user ID>, so they follow
// the user across browsers, and both actions are written to the audit log.

package admin

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/0xmattg/go-press/core/hook"
	"github.com/0xmattg/go-press/core/notice"

	"github.com/gin-gonic/gin"
)

const (
	// noticeDismissedOptionPrefix plus a user ID names the option holding
	// that user's dismissals as JSON (notice.Dismissals).
	noticeDismissedOptionPrefix = "admin.notice_dismissed."
	// maxOpenNotices is how many notices a page shows before folding the
	// rest.
	maxOpenNotices = 3
)

var (
	// noticeDismissMu serializes the read-modify-write of a user's
	// dismissals in this process.
	noticeDismissMu sync.Mutex
	// noticeProblemsLogged keeps an invalid notice from being logged on
	// every page.
	noticeProblemsLogged sync.Map
)

// NoticeBanner is the layout data of the notice center: the notices shown
// open and those folded under "N more notices".
type NoticeBanner struct {
	Label     string
	Open      []NoticeView
	Folded    []NoticeView
	MoreLabel string
	// MoreOne and MoreMany are the folded label for one and for several
	// notices ("%d" is the count), so the page can update it after a
	// dismissal.
	MoreOne  string
	MoreMany string
}

// NoticeView is one notice with the text the layout shows around it.
type NoticeView struct {
	notice.Notice
	CategoryLabel string
	LinkLabel     string
	External      bool
	AckLabel      string
	AckHint       string
	SnoozeHint    string
	FailedText    string
}

// providedNotices returns the normalized notices the sources provide for
// this request, before permissions and dismissals.
func (h *Handler) providedNotices(c *gin.Context, lang string, now time.Time) []notice.Notice {
	if h == nil {
		return nil
	}
	list := h.securityNotices(c, lang)
	if h.updateStatus != nil {
		list = append(list, h.updateStatus.OfficialNotices(lang)...)
	}
	if h.hooks != nil {
		extra, ok := h.hooks.ApplyFilter(hook.AdminNotices, []notice.Notice{}, c, lang).([]notice.Notice)
		if ok {
			list = append(list, extra...)
		} else {
			log.Printf("admin notices: a filter on %s returned the wrong type; their notices are not shown", hook.AdminNotices)
		}
	}
	out, problems := notice.Normalize(list, now)
	for _, p := range problems {
		key := p.Source + "/" + p.ID + ": " + p.Reason
		if _, seen := noticeProblemsLogged.LoadOrStore(key, true); !seen {
			log.Printf("admin notices: %s/%s: %s", p.Source, p.ID, p.Reason)
		}
	}
	return out
}

// canReadNotice reports whether the current user's role may see n.
func (h *Handler) canReadNotice(c *gin.Context, n notice.Notice) bool {
	return h.can(c, n.Resource, n.Action)
}

func noticeDismissedOption(userID uint) string {
	return noticeDismissedOptionPrefix + strconv.FormatUint(uint64(userID), 10)
}

func (h *Handler) loadNoticeDismissals(userID uint) notice.Dismissals {
	if h.svc == nil || h.svc.options == nil {
		return notice.Dismissals{}
	}
	return notice.ParseDismissals(h.svc.options.Get(noticeDismissedOption(userID)))
}

// visibleNotices returns the notices the current user sees now.
func (h *Handler) visibleNotices(c *gin.Context, lang string, now time.Time) []notice.Notice {
	userID := adminUserID(c)
	if userID == 0 || c.GetString("admin_role") == "" {
		return nil
	}
	provided := h.providedNotices(c, lang, now)
	if len(provided) == 0 {
		return nil
	}
	dismissed := h.loadNoticeDismissals(userID)
	visible := make([]notice.Notice, 0, len(provided))
	for _, n := range provided {
		if h.canReadNotice(c, n) && !dismissed.Hides(n, now) {
			visible = append(visible, n)
		}
	}
	return visible
}

// injectNotices adds the notice center to the layout data.
func (h *Handler) injectNotices(c *gin.Context, data gin.H) {
	if h == nil || h.svc == nil {
		return
	}
	lang, _ := data["AdminLanguage"].(string)
	if lang == "" {
		lang = h.svc.AdminLanguage()
	}
	visible := h.visibleNotices(c, lang, time.Now())
	if len(visible) == 0 {
		return
	}
	views := make([]NoticeView, len(visible))
	for i, n := range visible {
		views[i] = noticeView(lang, n)
	}
	banner := &NoticeBanner{
		Label:    adminT(lang, "notices.region"),
		MoreOne:  adminT(lang, "notices.more_one"),
		MoreMany: adminT(lang, "notices.more"),
	}
	if len(views) > maxOpenNotices {
		banner.Open, banner.Folded = views[:maxOpenNotices], views[maxOpenNotices:]
	} else {
		banner.Open = views
	}
	if n := len(banner.Folded); n == 1 {
		banner.MoreLabel = banner.MoreOne
	} else if n > 1 {
		banner.MoreLabel = fmt.Sprintf(banner.MoreMany, n)
	}
	data["AdminNotices"] = banner
}

func noticeView(lang string, n notice.Notice) NoticeView {
	v := NoticeView{
		Notice:        n,
		CategoryLabel: noticeCategoryLabel(lang, n.Category),
		AckLabel:      adminT(lang, "notices.acknowledge"),
		AckHint:       adminT(lang, "notices.acknowledge_hint"),
		SnoozeHint:    adminT(lang, "notices.snooze_hint", remindText(lang, n.RemindAfter)),
		FailedText:    adminT(lang, "notices.failed"),
	}
	if n.Link != nil {
		v.External = n.Link.IsExternal()
		v.LinkLabel = n.Link.Label
		if v.LinkLabel == "" {
			v.LinkLabel = adminT(lang, "notices.link_default")
		}
	}
	return v
}

func noticeCategoryLabel(lang, category string) string {
	switch category {
	case notice.CategorySecurity, notice.CategoryMaintenance, notice.CategoryDeprecation, notice.CategoryConfiguration:
		return adminT(lang, "notices.category."+category)
	default:
		return adminT(lang, "notices.category.announcement")
	}
}

// remindText says how long a snooze lasts, in whole days when it is a
// multiple of a day and in hours otherwise.
func remindText(lang string, d time.Duration) string {
	if d <= 0 {
		d = notice.DefaultRemindAfter
	}
	if d%(24*time.Hour) == 0 && d > 24*time.Hour {
		return adminT(lang, "notices.days", int(d/(24*time.Hour)))
	}
	hours := int((d + time.Hour - 1) / time.Hour)
	if hours == 1 {
		return adminT(lang, "notices.hour")
	}
	return adminT(lang, "notices.hours", hours)
}

// NoticeDismiss handles POST /admin/notices/dismiss with the form fields
// source, id, revision and mode ("snooze" or "acknowledge"). The admin
// route group already requires a session and a same-origin request. The
// notice must be one its sources provide now (404 otherwise), the user's
// role must be allowed to read it (403), the user must not have dismissed
// it already (404), and it must allow the mode (400). Only the current
// user's record changes.
func (h *Handler) NoticeDismiss(c *gin.Context) {
	mode := c.PostForm("mode")
	if mode != notice.ModeSnooze && mode != notice.ModeAcknowledge {
		noticeError(c, http.StatusBadRequest, "invalid_mode")
		return
	}
	revision, err := strconv.Atoi(strings.TrimSpace(c.PostForm("revision")))
	if err != nil || revision < 1 {
		noticeError(c, http.StatusBadRequest, "invalid_revision")
		return
	}
	source := strings.TrimSpace(c.PostForm("source"))
	id := strings.TrimSpace(c.PostForm("id"))
	userID := adminUserID(c)
	if userID == 0 || h.svc == nil || h.svc.options == nil {
		noticeError(c, http.StatusForbidden, "forbidden")
		return
	}

	now := time.Now()
	provided := h.providedNotices(c, h.svc.AdminLanguage(), now)
	var target *notice.Notice
	for i := range provided {
		if provided[i].Source == source && provided[i].ID == id {
			target = &provided[i]
			break
		}
	}
	if target == nil || target.Revision != revision {
		noticeError(c, http.StatusNotFound, "not_found")
		return
	}
	if !h.canReadNotice(c, *target) {
		noticeError(c, http.StatusForbidden, "forbidden")
		return
	}

	noticeDismissMu.Lock()
	defer noticeDismissMu.Unlock()
	dismissed := h.loadNoticeDismissals(userID)
	if dismissed.Hides(*target, now) {
		noticeError(c, http.StatusNotFound, "not_found")
		return
	}
	if (mode == notice.ModeSnooze && !target.Snoozable) || (mode == notice.ModeAcknowledge && !target.Acknowledgeable) {
		noticeError(c, http.StatusBadRequest, "mode_not_allowed")
		return
	}
	dismissed.Record(*target, mode, now)
	dismissed.Prune(provided, now)
	raw, err := dismissed.Encode()
	if err == nil {
		err = h.svc.options.Set(noticeDismissedOption(userID), raw)
	}
	if err != nil {
		log.Printf("admin notices: save dismissal for user %d: %v", userID, err)
		noticeError(c, http.StatusInternalServerError, "save_failed")
		return
	}
	h.logAction(c, mode, "notice", 0, notice.KeyOf(*target))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// noticeError answers a dismissal request with the JSON error shape the
// other admin endpoints use.
func noticeError(c *gin.Context, status int, code string) {
	c.JSON(status, gin.H{"ok": false, "error": gin.H{"code": code}})
}
