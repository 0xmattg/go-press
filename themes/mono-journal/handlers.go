package monojournal

import (
	"context"
	"errors"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go-press/core/comment"
	coreI18n "go-press/core/i18n"
	coreTheme "go-press/core/theme"
	"go-press/core/user"
	"go-press/pkg/middleware"
)

type Handler struct {
	pageService *PageService
	templates   map[string]*template.Template
	templateDir string
	i18n        *coreI18n.Manager
	comments    PublicCommentService
	authorizer  coreTheme.PublicAuthorizationApp
}

type PublicCommentService interface {
	Create(ctx context.Context, input comment.CreateInput) (*comment.Comment, error)
	CountByUser(userID uint) (int64, error)
	RecentByUser(userID uint, limit int) ([]comment.View, error)
}

func NewHandler(service *PageService, themeDir string, i18n *coreI18n.Manager) *Handler {
	return &Handler{pageService: service, templates: map[string]*template.Template{}, templateDir: filepath.Join(themeDir, "templates"), i18n: i18n}
}

func (h *Handler) SetPublicRuntime(comments PublicCommentService, authorizer coreTheme.PublicAuthorizationApp) {
	h.comments = comments
	h.authorizer = authorizer
}

func (h *Handler) LoadPageTemplates(t coreTheme.Theme) error {
	bundle, err := coreTheme.LoadPageBundle(t, []string{"home", "search", "profile"})
	if err != nil {
		return err
	}
	h.templates = bundle
	return nil
}

func (h *Handler) Profile(c *gin.Context) {
	account := user.CurrentUser(c)
	if account == nil {
		c.Redirect(http.StatusSeeOther, user.LoginURL(c))
		return
	}
	if h.authorizer == nil || !h.authorizer.CanPublicUser(c, "profile", "read_own") {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	if h.comments == nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	count, err := h.comments.CountByUser(account.ID)
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	recent, err := h.comments.RecentByUser(account.ID, 5)
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	title := "Profile"
	if h.i18n != nil {
		title = h.i18n.Translate(c, "profile.title")
	}
	data := h.pageService.ForRequest(c).GetProfileData(title, user.CurrentUserView(c), count, recent)
	c.Header("Cache-Control", "private, no-store")
	h.render(c, "profile", data)
}

func (h *Handler) CommentCreate(c *gin.Context) {
	returnTo := user.SafeReturnTo(c.PostForm("return_to"), "/")
	if !middleware.IsSameOrigin(c.Request) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	account := user.CurrentUser(c)
	if account == nil {
		c.Redirect(http.StatusSeeOther, "/login?return_to="+url.QueryEscape(returnTo))
		return
	}
	if h.authorizer == nil || !h.authorizer.CanPublicUser(c, "comment", "create") {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	if h.comments == nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	contentID, err := strconv.ParseUint(strings.TrimSpace(c.PostForm("content_id")), 10, 32)
	if err != nil || contentID == 0 {
		h.redirectCommentResult(c, returnTo, "", "invalid")
		return
	}
	var parentID *uint
	if raw := strings.TrimSpace(c.PostForm("parent_id")); raw != "" {
		parsed, err := strconv.ParseUint(raw, 10, 32)
		if err != nil || parsed == 0 {
			h.redirectCommentResult(c, returnTo, "", "invalid")
			return
		}
		value := uint(parsed)
		parentID = &value
	}
	_, err = h.comments.Create(c.Request.Context(), comment.CreateInput{
		ContentID: uint(contentID), UserID: account.ID, ParentID: parentID, Body: c.PostForm("body"),
	})
	if err != nil {
		h.redirectCommentResult(c, returnTo, "", commentErrorCode(err))
		return
	}
	h.redirectCommentResult(c, returnTo, "pending", "")
}

func (h *Handler) redirectCommentResult(c *gin.Context, returnTo, notice, errorCode string) {
	parsed, err := url.Parse(user.SafeReturnTo(returnTo, "/"))
	if err != nil {
		parsed = &url.URL{Path: "/"}
	}
	query := parsed.Query()
	query.Del("comment")
	query.Del("comment_error")
	if notice != "" {
		query.Set("comment", notice)
	}
	if errorCode != "" {
		query.Set("comment_error", errorCode)
	}
	parsed.RawQuery = query.Encode()
	parsed.Fragment = "comments"
	c.Redirect(http.StatusSeeOther, parsed.String())
}

func commentErrorCode(err error) string {
	switch {
	case errors.Is(err, comment.ErrInvalidBody):
		return "body"
	case errors.Is(err, comment.ErrCommentsClosed):
		return "closed"
	case errors.Is(err, comment.ErrInvalidParent), errors.Is(err, comment.ErrReplyDepth):
		return "reply"
	case errors.Is(err, comment.ErrRateLimited):
		return "rate"
	default:
		return "invalid"
	}
}

func (h *Handler) Home(c *gin.Context) {
	data, err := h.pageService.ForRequest(c).GetHomeData()
	if err != nil {
		log.Printf("[mono-journal] home data: %v", err)
		c.String(http.StatusInternalServerError, "Internal server error")
		return
	}
	h.render(c, "home", data)
}

func (h *Handler) Search(c *gin.Context) {
	data, err := h.pageService.ForRequest(c).GetSearchData(c.Query("q"))
	if err != nil {
		log.Printf("[mono-journal] search data: %v", err)
		c.String(http.StatusInternalServerError, "Internal server error")
		return
	}
	h.render(c, "search", data)
}

func (h *Handler) render(c *gin.Context, page string, data interface{}) {
	tmpl := h.templates[page]
	if tmpl == nil {
		c.String(http.StatusInternalServerError, "Template not found")
		return
	}
	if setter, ok := data.(interface{ SetCtx(*gin.Context) }); ok {
		setter.SetCtx(c)
	}
	if translator, ok := data.(interface {
		TranslateSettings(*gin.Context, *coreI18n.Manager)
	}); ok {
		translator.TranslateSettings(c, h.i18n)
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	if err := tmpl.ExecuteTemplate(c.Writer, "base", data); err != nil {
		log.Printf("[mono-journal] render %s: %v", page, err)
	}
}
