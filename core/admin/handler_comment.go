package admin

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/0xmattg/go-press/core/comment"
	"github.com/0xmattg/go-press/core/content"
	"github.com/0xmattg/go-press/core/rewrite"

	"github.com/gin-gonic/gin"
)

func (h *Handler) CommentList(c *gin.Context) {
	if !h.checkPermission(c, "comment", "moderate") {
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	status := strings.TrimSpace(c.Query("status"))
	result, err := h.svc.ListComments(status, page, 20)
	if err != nil {
		c.String(http.StatusServiceUnavailable, err.Error())
		return
	}
	if result.TotalPages > 0 && result.Page > result.TotalPages {
		c.Redirect(http.StatusFound, commentListURL(status, result.TotalPages))
		return
	}
	lang := h.svc.AdminLanguage()
	rows := h.adminCommentRows(c, result.Items)
	h.render(c, "comments", gin.H{
		"Title": adminT(lang, "nav.comments"), "Active": "comments",
		"Result": result, "Items": rows, "Status": status,
		"Pagination": buildCommentPagination(result, status),
	})
}

func buildCommentPagination(result *comment.Page, status string) AdminPaginationView {
	totalPages := result.TotalPages
	if totalPages < 1 {
		totalPages = 1
	}
	page := result.Page
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	buildURL := func(targetPage int) string {
		if targetPage < 1 {
			targetPage = 1
		}
		if targetPage > totalPages {
			targetPage = totalPages
		}
		return commentListURL(status, targetPage)
	}
	var from, to int64
	if result.Total > 0 && len(result.Items) > 0 {
		from = int64((page-1)*result.PerPage) + 1
		to = from + int64(len(result.Items)) - 1
	}
	return AdminPaginationView{
		Total: result.Total, Page: page, PerPage: result.PerPage,
		TotalPages: totalPages, From: from, To: to, Offset: (page - 1) * result.PerPage,
		FirstURL: buildURL(1), PrevURL: buildURL(page - 1),
		NextURL: buildURL(page + 1), LastURL: buildURL(totalPages),
	}
}

type adminCommentRow struct {
	comment.Comment
	IsReply     bool
	ContextText string
	ContextURL  string
	TargetURL   string
}

func (h *Handler) adminCommentRows(c *gin.Context, items []comment.Comment) []adminCommentRow {
	rows := make([]adminCommentRow, 0, len(items))
	for _, item := range items {
		targetURL := h.adminPublicContentURL(c, &item.Target)
		row := adminCommentRow{
			Comment:     item,
			ContextText: item.Target.Title,
			ContextURL:  targetURL,
			TargetURL:   targetURL,
		}
		if item.ParentID != nil {
			row.IsReply = true
			row.ContextText = "#" + strconv.FormatUint(uint64(*item.ParentID), 10)
			if item.Parent != nil {
				row.ContextText = adminCommentExcerpt(item.Parent.Body, 72)
			}
			row.ContextURL = commentAnchorURL(targetURL, *item.ParentID)
		}
		rows = append(rows, row)
	}
	return rows
}

func (h *Handler) adminPublicContentURL(c *gin.Context, item *content.Content) string {
	if item == nil {
		return ""
	}
	base := "/" + strings.Trim(item.Type, "/") + "/" + strings.Trim(item.Slug, "/")
	if h.registry != nil {
		base = rewrite.NewEngine(h.registry).BuildURL(item.Type, item.Slug)
	}
	prefix := ""
	if h.hooks != nil {
		if value := h.hooks.ApplyFilter(HookContentPermalinkPrefix, "", c, item); value != nil {
			prefix, _ = value.(string)
		}
	}
	return strings.TrimRight(strings.TrimSpace(prefix), "/") + base
}

func adminCommentExcerpt(body string, maxRunes int) string {
	normalized := strings.Join(strings.Fields(body), " ")
	if maxRunes < 1 {
		return normalized
	}
	runes := []rune(normalized)
	if len(runes) <= maxRunes {
		return normalized
	}
	return string(runes[:maxRunes]) + "..."
}

func commentAnchorURL(raw string, id uint) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	parsed.Fragment = "comment-" + strconv.FormatUint(uint64(id), 10)
	return parsed.String()
}

func commentListURL(status string, page int) string {
	if page < 1 {
		page = 1
	}
	query := url.Values{}
	if strings.TrimSpace(status) != "" {
		query.Set("status", strings.TrimSpace(status))
	}
	query.Set("page", strconv.Itoa(page))
	return "/admin/comments?" + query.Encode()
}

func (h *Handler) CommentStatusUpdate(c *gin.Context) {
	if !h.checkPermission(c, "comment", "moderate") {
		return
	}
	id := getIDParam(c)
	status := strings.TrimSpace(c.PostForm("status"))
	item, err := h.svc.ModerateComment(c.Request.Context(), id, status)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/comments?error="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "comment.update_failed")))
		return
	}
	h.invalidatePageCache()
	h.logAction(c, "moderate", "comment", item.ID, status)
	c.Redirect(http.StatusFound, "/admin/comments?success="+url.QueryEscape(adminT(h.svc.AdminLanguage(), "comment.updated")))
}

func commentStatusLabelKey(status string) string {
	switch status {
	case comment.StatusApproved:
		return "comment.status.approved"
	case comment.StatusSpam:
		return "comment.status.spam"
	case comment.StatusTrash:
		return "comment.status.trash"
	default:
		return "comment.status.pending"
	}
}
