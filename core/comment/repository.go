package comment

import (
	"time"

	"go-press/core/content"
	"go-press/core/user"

	"gorm.io/gorm"
)

// Repository centralizes persistence and query scoping for comments.
type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Create(item *Comment) error {
	return r.db.Create(item).Error
}

func (r *Repository) FindByID(id uint) (*Comment, error) {
	var item Comment
	if err := r.db.Preload("Author").Preload("Target").First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) FindPublishedTarget(id uint, now time.Time) (*content.Content, error) {
	var item content.Content
	err := r.db.Where(
		"id = ? AND status = ? AND (published_at IS NULL OR published_at <= ?)",
		id, content.StatusPublished, now,
	).First(&item).Error
	return &item, err
}

func (r *Repository) FindActiveUser(id uint) (*user.User, error) {
	var account user.User
	err := r.db.Where("id = ? AND is_active = ?", id, true).First(&account).Error
	return &account, err
}

func (r *Repository) ListVisibleForContent(contentID, viewerID uint) ([]Comment, error) {
	var items []Comment
	q := r.db.Preload("Author").Where("content_id = ?", contentID)
	if viewerID > 0 {
		q = q.Where("status = ? OR (status = ? AND user_id = ?)", StatusApproved, StatusPending, viewerID)
	} else {
		q = q.Where("status = ?", StatusApproved)
	}
	err := q.Order("created_at ASC, id ASC").Find(&items).Error
	return items, err
}

// ListVisibleForContentReview returns the public thread plus its pending
// moderation queue. Authorization is intentionally handled by the service
// caller because ownership and moderator capabilities are request concerns.
func (r *Repository) ListVisibleForContentReview(contentID uint) ([]Comment, error) {
	var items []Comment
	err := r.db.Preload("Author").Where(
		"content_id = ? AND status IN ?", contentID, []string{StatusApproved, StatusPending},
	).Order("created_at ASC, id ASC").Find(&items).Error
	return items, err
}

func (r *Repository) CountApprovedForContent(contentID uint) (int64, error) {
	var count int64
	err := r.db.Model(&Comment{}).
		Where("content_id = ? AND status = ?", contentID, StatusApproved).
		Count(&count).Error
	return count, err
}

func (r *Repository) CountRecentByUser(userID uint, since time.Time) (int64, error) {
	var count int64
	err := r.db.Model(&Comment{}).
		Where("user_id = ? AND created_at >= ?", userID, since).
		Count(&count).Error
	return count, err
}

func (r *Repository) CountByUser(userID uint) (int64, error) {
	var count int64
	err := r.db.Model(&Comment{}).
		Where("user_id = ? AND status IN ?", userID, []string{StatusPending, StatusApproved}).
		Count(&count).Error
	return count, err
}

func (r *Repository) RecentByUser(userID uint, limit int) ([]Comment, error) {
	if limit <= 0 {
		limit = 5
	}
	var items []Comment
	err := r.db.Preload("Target").
		Where("user_id = ? AND status IN ?", userID, []string{StatusPending, StatusApproved}).
		Order("created_at DESC, id DESC").Limit(limit).Find(&items).Error
	return items, err
}

func (r *Repository) AdminList(status string, page, perPage int) (*Page, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	q := r.db.Model(&Comment{})
	if isStatus(status) {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	var items []Comment
	if err := q.Preload("Author").Preload("Target").Preload("Parent").
		Order("created_at DESC, id DESC").
		Offset((page - 1) * perPage).Limit(perPage).Find(&items).Error; err != nil {
		return nil, err
	}
	totalPages := int((total + int64(perPage) - 1) / int64(perPage))
	return &Page{Items: items, Total: total, Page: page, PerPage: perPage, TotalPages: totalPages}, nil
}

func (r *Repository) UpdateStatus(id uint, status string) error {
	return r.db.Model(&Comment{}).Where("id = ?", id).Update("status", status).Error
}

func (r *Repository) DeleteByContentIDs(tx *gorm.DB, contentIDs []uint) error {
	if len(contentIDs) == 0 {
		return nil
	}
	if tx == nil {
		tx = r.db
	}
	return tx.Unscoped().Where("content_id IN ?", contentIDs).Delete(&Comment{}).Error
}

func isStatus(status string) bool {
	switch status {
	case StatusPending, StatusApproved, StatusSpam, StatusTrash:
		return true
	default:
		return false
	}
}
