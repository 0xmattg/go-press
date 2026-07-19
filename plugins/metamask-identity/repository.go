package metamaskidentity

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

var errChallengeUnavailable = errors.New("wallet challenge is invalid, expired, or already used")

type challengeRepository struct{ db *gorm.DB }

type challengeStore interface {
	Create(context.Context, *walletChallenge) error
	FindActiveByToken(context.Context, string, time.Time) (*walletChallenge, error)
	Consume(context.Context, uint, time.Time) error
	DeleteStale(context.Context, time.Time) error
}

func newChallengeRepository(db *gorm.DB) *challengeRepository { return &challengeRepository{db: db} }

func (r *challengeRepository) AutoMigrate() error {
	return r.db.AutoMigrate(&walletChallenge{})
}

func (r *challengeRepository) Create(ctx context.Context, challenge *walletChallenge) error {
	return r.db.WithContext(ctx).Create(challenge).Error
}

func (r *challengeRepository) FindActiveByToken(ctx context.Context, tokenHash string, now time.Time) (*walletChallenge, error) {
	var challenge walletChallenge
	err := r.db.WithContext(ctx).
		Where("token_hash = ? AND used_at IS NULL AND expires_at > ?", tokenHash, now).
		First(&challenge).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errChallengeUnavailable
	}
	return &challenge, err
}

func (r *challengeRepository) Consume(ctx context.Context, id uint, now time.Time) error {
	result := r.db.WithContext(ctx).Model(&walletChallenge{}).
		Where("id = ? AND used_at IS NULL AND expires_at > ?", id, now).
		Update("used_at", now)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errChallengeUnavailable
	}
	return nil
}

func (r *challengeRepository) DeleteStale(ctx context.Context, now time.Time) error {
	usedCutoff := now.Add(-time.Hour)
	return r.db.WithContext(ctx).
		Where("expires_at <= ? OR (used_at IS NOT NULL AND used_at <= ?)", now, usedCutoff).
		Delete(&walletChallenge{}).Error
}
