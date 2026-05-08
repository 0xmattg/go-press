package user

import (
	"time"

	"gorm.io/gorm"
)

// Repository provides CRUD operations for users.
type Repository struct {
	db *gorm.DB
}

// NewRepository creates a new user Repository.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// FindByID returns a user by ID.
func (r *Repository) FindByID(id uint) (*User, error) {
	var u User
	err := r.db.First(&u, id).Error
	return &u, err
}

// FindByUsername returns a user by username.
func (r *Repository) FindByUsername(username string) (*User, error) {
	var u User
	err := r.db.Where("username = ?", username).First(&u).Error
	return &u, err
}

// FindByEmail returns a user by email.
func (r *Repository) FindByEmail(email string) (*User, error) {
	var u User
	err := r.db.Where("email = ?", email).First(&u).Error
	return &u, err
}

// Create creates a new user. Password should already be hashed.
func (r *Repository) Create(u *User) error {
	return r.db.Create(u).Error
}

// Update updates an existing user.
func (r *Repository) Update(u *User) error {
	return r.db.Save(u).Error
}

// Delete soft-deletes a user.
func (r *Repository) Delete(id uint) error {
	return r.db.Delete(&User{}, id).Error
}

// List returns paginated users, optionally filtered by role.
func (r *Repository) List(role string, page, perPage int) ([]User, int64, error) {
	q := r.db.Model(&User{})
	if role != "" {
		q = q.Where("role = ?", role)
	}
	var total int64
	q.Count(&total)

	var users []User
	err := q.Order("created_at DESC").
		Offset((page - 1) * perPage).
		Limit(perPage).
		Find(&users).Error
	return users, total, err
}

// UpdateLastLogin sets the last login timestamp.
func (r *Repository) UpdateLastLogin(id uint) error {
	now := time.Now()
	return r.db.Model(&User{}).Where("id = ?", id).Update("last_login_at", now).Error
}

// --- UserMeta operations ---

// SaveMeta upserts a user meta key-value pair.
func (r *Repository) SaveMeta(userID uint, key, value string) error {
	var meta UserMeta
	result := r.db.Where("user_id = ? AND meta_key = ?", userID, key).First(&meta)
	if result.Error == nil {
		meta.MetaValue = value
		return r.db.Save(&meta).Error
	}
	meta = UserMeta{
		UserID:    userID,
		MetaKey:   key,
		MetaValue: value,
	}
	return r.db.Create(&meta).Error
}

// DeleteMeta deletes a user meta entry.
func (r *Repository) DeleteMeta(userID uint, key string) error {
	return r.db.Where("user_id = ? AND meta_key = ?", userID, key).Delete(&UserMeta{}).Error
}

// GetMeta returns all meta for a user.
func (r *Repository) GetMeta(userID uint) (map[string]string, error) {
	var metas []UserMeta
	err := r.db.Where("user_id = ?", userID).Find(&metas).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(metas))
	for _, m := range metas {
		out[m.MetaKey] = m.MetaValue
	}
	return out, nil
}

// CountByRole returns the number of users in each role.
func (r *Repository) CountByRole() (map[string]int64, error) {
	type Result struct {
		Role  string
		Count int64
	}
	var results []Result
	err := r.db.Model(&User{}).
		Select("role, count(*) as count").
		Group("role").
		Find(&results).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(results))
	for _, r := range results {
		out[r.Role] = r.Count
	}
	return out, nil
}
