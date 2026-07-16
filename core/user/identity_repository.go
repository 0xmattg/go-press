package user

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// IdentityRepository persists verified external identity bindings.
type IdentityRepository struct {
	db *gorm.DB
}

func NewIdentityRepository(db *gorm.DB) *IdentityRepository {
	return &IdentityRepository{db: db}
}

func (r *IdentityRepository) WithDB(db *gorm.DB) *IdentityRepository {
	return NewIdentityRepository(db)
}

func (r *IdentityRepository) Create(ctx context.Context, identity *UserIdentity) error {
	return r.db.WithContext(ctx).Create(identity).Error
}

func (r *IdentityRepository) FindByKey(ctx context.Context, provider, issuer, subject string) (*UserIdentity, error) {
	var identity UserIdentity
	err := r.db.WithContext(ctx).
		Where("provider = ? AND issuer = ? AND subject = ?", provider, issuer, subject).
		First(&identity).Error
	if err == gorm.ErrRecordNotFound {
		return nil, ErrIdentityNotFound
	}
	return &identity, err
}

func (r *IdentityRepository) FindByIDForUser(ctx context.Context, id, userID uint) (*UserIdentity, error) {
	var identity UserIdentity
	err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&identity).Error
	if err == gorm.ErrRecordNotFound {
		return nil, ErrIdentityNotFound
	}
	return &identity, err
}

func (r *IdentityRepository) ListByUser(ctx context.Context, userID uint) ([]UserIdentity, error) {
	var identities []UserIdentity
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at ASC").Find(&identities).Error
	return identities, err
}

func (r *IdentityRepository) CountByUser(ctx context.Context, userID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&UserIdentity{}).Where("user_id = ?", userID).Count(&count).Error
	return count, err
}

func (r *IdentityRepository) UpdateUsage(ctx context.Context, id uint, profileJSON string, usedAt time.Time) error {
	updates := map[string]interface{}{"last_used_at": usedAt}
	if profileJSON != "" {
		updates["profile_json"] = profileJSON
	}
	return r.db.WithContext(ctx).Model(&UserIdentity{}).Where("id = ?", id).Updates(updates).Error
}

// DeleteForUser scopes deletion by both identity and owner to prevent IDOR.
func (r *IdentityRepository) DeleteForUser(ctx context.Context, id, userID uint) error {
	result := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&UserIdentity{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrIdentityNotFound
	}
	return nil
}

// SessionRepository persists revocable public-site sessions.
type SessionRepository struct {
	db *gorm.DB
}

func NewSessionRepository(db *gorm.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

func (r *SessionRepository) Create(ctx context.Context, session *UserSession) error {
	return r.db.WithContext(ctx).Create(session).Error
}

func (r *SessionRepository) FindActiveByHash(ctx context.Context, tokenHash string, now time.Time) (*UserSession, error) {
	var session UserSession
	err := r.db.WithContext(ctx).
		Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ?", tokenHash, now).
		First(&session).Error
	if err == gorm.ErrRecordNotFound {
		return nil, ErrSessionNotFound
	}
	return &session, err
}

func (r *SessionRepository) Touch(ctx context.Context, id uint, seenAt time.Time) error {
	return r.db.WithContext(ctx).Model(&UserSession{}).Where("id = ?", id).Update("last_seen_at", seenAt).Error
}

func (r *SessionRepository) RevokeByHash(ctx context.Context, tokenHash string, now time.Time) error {
	result := r.db.WithContext(ctx).Model(&UserSession{}).
		Where("token_hash = ? AND revoked_at IS NULL", tokenHash).
		Update("revoked_at", now)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (r *SessionRepository) RevokeAllForUser(ctx context.Context, userID uint, now time.Time) error {
	return r.db.WithContext(ctx).Model(&UserSession{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", now).Error
}

func (r *SessionRepository) DeleteExpired(ctx context.Context, now time.Time) error {
	return r.db.WithContext(ctx).
		Where("expires_at <= ? OR revoked_at IS NOT NULL", now).
		Delete(&UserSession{}).Error
}
