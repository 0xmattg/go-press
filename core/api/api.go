package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"go-press/core/content"
)

// response wraps standardized API responses.
type response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *apiError   `json:"error,omitempty"`
	Meta    *pagination `json:"meta,omitempty"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type pagination struct {
	Page       int   `json:"page"`
	PerPage    int   `json:"per_page"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

func respondOK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, response{Success: true, Data: data})
}

func respondPaginated(c *gin.Context, data interface{}, meta *pagination) {
	c.JSON(http.StatusOK, response{Success: true, Data: data, Meta: meta})
}

func respondError(c *gin.Context, status int, code, message string) {
	c.JSON(status, response{
		Success: false,
		Error:   &apiError{Code: code, Message: message},
	})
}

// contentDTO is the API representation of a Content item.
type contentDTO struct {
	ID          uint              `json:"id"`
	Type        string            `json:"type"`
	Status      string            `json:"status"`
	Title       string            `json:"title"`
	Slug        string            `json:"slug"`
	Content     string            `json:"content"`
	Excerpt     string            `json:"excerpt"`
	ImageURL    string            `json:"image_url,omitempty"`
	AuthorID    uint              `json:"author_id"`
	SortOrder   int               `json:"sort_order"`
	PublishedAt *string           `json:"published_at,omitempty"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
	Meta        map[string]string `json:"meta,omitempty"`
	URL         string            `json:"url,omitempty"`
}

func toDTO(c *content.Content) contentDTO {
	dto := contentDTO{
		ID:        c.ID,
		Type:      c.Type,
		Status:    c.Status,
		Title:     c.Title,
		Slug:      c.Slug,
		Content:   content.SanitizeHTML(c.Content),
		Excerpt:   c.Excerpt,
		ImageURL:  c.ImageURL,
		AuthorID:  c.AuthorID,
		SortOrder: c.SortOrder,
		CreatedAt: c.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt: c.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if c.PublishedAt != nil {
		s := c.PublishedAt.Format("2006-01-02T15:04:05Z")
		dto.PublishedAt = &s
	}
	if len(c.Meta) > 0 {
		dto.Meta = make(map[string]string, len(c.Meta))
		for _, m := range c.Meta {
			dto.Meta[m.MetaKey] = m.MetaValue
		}
	}
	return dto
}

// Handler provides REST API endpoint handlers.
type Handler struct {
	registry *content.Registry
	repo     *content.Repository
}

// NewHandler creates a new API handler.
func NewHandler(registry *content.Registry, repo *content.Repository) *Handler {
	return &Handler{registry: registry, repo: repo}
}

// RegisterRoutes dynamically registers REST API routes for all content types.
// Call this after themes have registered their content types.
func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	// Generic routes that work with any content type via query param
	r.GET("/content", h.List)
	r.GET("/content/:id", h.Get)

	// Type-specific convenience routes: /api/v1/products, /api/v1/posts, etc.
	for _, typeDef := range h.registry.AllTypes() {
		if !isPublicContentType(typeDef) {
			continue
		}
		slug := typeDef.Rewrite.Slug
		if slug == "" {
			slug = typeDef.Name
		}
		typeName := typeDef.Name // capture for closure
		r.GET("/"+slug, func(c *gin.Context) {
			c.Set("api_type", typeName)
			h.List(c)
		})
		r.GET("/"+slug+"/:id", func(c *gin.Context) {
			c.Set("api_type", typeName)
			h.Get(c)
		})
	}
}

// List handles GET /api/v1/content
//
//	@Summary		List content items
//	@Description	Query published public content with filters, pagination, and sorting
//	@Tags			Content
//	@Accept			json
//	@Produce		json
//	@Param			type		query	string	false	"Content type filter (e.g. product, post, service)"
//	@Param			status		query	string	false	"Only published is accepted"	default(published)
//	@Param			search		query	string	false	"Search keyword in title and content"
//	@Param			taxonomy	query	string	false	"Taxonomy name for filtering (requires term param)"
//	@Param			term		query	string	false	"Taxonomy term slug (requires taxonomy param)"
//	@Param			meta_key	query	string	false	"Meta key for filtering (requires meta_value)"
//	@Param			meta_value	query	string	false	"Meta value for filtering (requires meta_key)"
//	@Param			order_by	query	string	false	"Sort field: created_at, updated_at, published_at, title, sort_order"	default(created_at)
//	@Param			order		query	string	false	"Sort direction: ASC or DESC"	default(DESC)
//	@Param			page		query	int		false	"Page number"	default(1)
//	@Param			per_page	query	int		false	"Items per page (max 100)"	default(20)
//	@Success		200	{object}	response{data=[]contentDTO,meta=pagination}
//	@Failure		500	{object}	response{error=apiError}
//	@Router			/content [get]
func (h *Handler) List(c *gin.Context) {
	// Content type from route or query param
	typeName, _ := c.Get("api_type")
	if typeName == nil || typeName == "" {
		typeName = c.Query("type")
	}
	if t, ok := typeName.(string); ok && t != "" {
		if !isPublicContentType(h.registry.GetType(t)) {
			respondError(c, http.StatusNotFound, "not_found", "Content type not found")
			return
		}
	}

	// Public REST endpoints never expose drafts, archived rows, trash, or
	// scheduled content. Management access belongs to authenticated admin APIs.
	status := c.DefaultQuery("status", "published")
	if status != "" && status != content.StatusPublished {
		respondError(c, http.StatusBadRequest, "invalid_status", "Only published content is available")
		return
	}

	q := h.repo.Query().Published()
	if t, ok := typeName.(string); ok && t != "" {
		q = q.Type(t)
	} else {
		q = q.Types(h.publicContentTypeNames())
	}

	// Search
	if search := c.Query("search"); search != "" {
		q = q.Search(search)
	}

	// Taxonomy filter
	if tax := c.Query("taxonomy"); tax != "" {
		if term := c.Query("term"); term != "" {
			q = q.Taxonomy(tax, term)
		}
	}

	// Meta filter
	if metaKey := c.Query("meta_key"); metaKey != "" {
		if metaVal := c.Query("meta_value"); metaVal != "" {
			q = q.Meta(metaKey, metaVal)
		}
	}

	// Ordering
	orderBy := c.DefaultQuery("order_by", "created_at")
	orderDir := strings.ToUpper(c.DefaultQuery("order", "DESC"))
	if orderDir != "ASC" && orderDir != "DESC" {
		orderDir = "DESC"
	}
	// Whitelist allowed order fields to prevent SQL injection
	allowedOrder := map[string]bool{
		"created_at":   true,
		"updated_at":   true,
		"published_at": true,
		"title":        true,
		"sort_order":   true,
	}
	if !allowedOrder[orderBy] {
		orderBy = "created_at"
	}
	q = q.OrderBy(orderBy, orderDir)

	// Pagination
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}

	result, err := q.Paginate(page, perPage)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "query_error", "Failed to query content")
		return
	}

	dtos := make([]contentDTO, len(result.Items))
	for i, item := range result.Items {
		dtos[i] = toDTO(&item)
	}

	respondPaginated(c, dtos, &pagination{
		Page:       result.Page,
		PerPage:    result.PerPage,
		Total:      result.Total,
		TotalPages: result.TotalPages,
	})
}

// Get handles GET /api/v1/content/:id
//
//	@Summary		Get content by ID or slug
//	@Description	Retrieve a single content item by numeric ID or slug (slug requires type-specific endpoint)
//	@Tags			Content
//	@Accept			json
//	@Produce		json
//	@Param			id	path	string	true	"Content ID (numeric) or slug (string)"
//	@Success		200	{object}	response{data=contentDTO}
//	@Failure		400	{object}	response{error=apiError}
//	@Failure		404	{object}	response{error=apiError}
//	@Router			/content/{id} [get]
func (h *Handler) Get(c *gin.Context) {
	idStr := c.Param("id")
	requestedType, _ := c.Get("api_type")

	// Try as numeric ID first
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err == nil {
		item, err := h.repo.FindByID(uint(id))
		if err != nil || !h.canExpose(item, requestedType) {
			respondError(c, http.StatusNotFound, "not_found", "Content not found")
			return
		}
		respondOK(c, toDTO(item))
		return
	}

	// Try as slug — scoped so multilang's per-language WHERE applies when
	// the request carries a language hint (?lang=zh, /zh/api/... etc).
	typeName, _ := c.Get("api_type")
	if t, ok := typeName.(string); ok && t != "" {
		if !isPublicContentType(h.registry.GetType(t)) {
			respondError(c, http.StatusNotFound, "not_found", "Content not found")
			return
		}
		item, err := h.repo.FindBySlugScoped(c, t, idStr)
		if err != nil || !h.canExpose(item, t) {
			respondError(c, http.StatusNotFound, "not_found", "Content not found")
			return
		}
		respondOK(c, toDTO(item))
		return
	}

	respondError(c, http.StatusBadRequest, "invalid_id", "Provide a numeric ID or use a type-specific endpoint for slug lookup")
}

// Types handles GET /api/v1/types
//
//	@Summary		List registered content types
//	@Description	Return all registered content type definitions including fields, taxonomies, and rewrite rules
//	@Tags			Content Types
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	response
//	@Router			/types [get]
func (h *Handler) Types(c *gin.Context) {
	types := make([]*content.ContentTypeDef, 0)
	for _, typeDef := range h.registry.AllTypes() {
		if isPublicContentType(typeDef) {
			types = append(types, typeDef)
		}
	}
	respondOK(c, types)
}

func isPublicContentType(typeDef *content.ContentTypeDef) bool {
	return typeDef != nil && typeDef.HasArchive
}

func (h *Handler) publicContentTypeNames() []string {
	names := make([]string, 0)
	for _, typeDef := range h.registry.AllTypes() {
		if isPublicContentType(typeDef) {
			names = append(names, typeDef.Name)
		}
	}
	return names
}

func (h *Handler) canExpose(item *content.Content, requestedType interface{}) bool {
	if item == nil || !isPublicContentType(h.registry.GetType(item.Type)) {
		return false
	}
	if typeName, ok := requestedType.(string); ok && typeName != "" && item.Type != typeName {
		return false
	}
	if item.Status != content.StatusPublished {
		return false
	}
	return item.PublishedAt == nil || !item.PublishedAt.After(time.Now())
}
