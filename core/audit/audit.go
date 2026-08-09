// Package audit provides protocol- and UI-neutral persistence for framework
// audit events. Admin, Agent, REST, and future transports should depend on this
// package instead of defining their own audit tables.
package audit

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/0xmattg/go-press/pkg/dbprefix"

	"gorm.io/gorm"
)

var ErrUnavailable = errors.New("audit service unavailable")

// Event is one auditable action performed against a framework resource.
type Event struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	UserID     uint      `json:"user_id"`
	Username   string    `gorm:"size:50" json:"username"`
	Action     string    `gorm:"size:50;not null" json:"action"`
	Resource   string    `gorm:"size:50" json:"resource"`
	ResourceID uint      `json:"resource_id"`
	Details    string    `gorm:"type:text" json:"details"`
	IPAddress  string    `gorm:"size:45" json:"ip_address"`
	CreatedAt  time.Time `json:"created_at"`
}

func (Event) TableName() string { return dbprefix.Table("audit_logs") }

// Recorder is the narrow audit capability consumed by higher-level services.
type Recorder interface {
	Record(context.Context, *Event) error
}

// Service persists and queries audit events.
type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

// Record stores an event. Context cancellation and deadlines are propagated to
// GORM so audit writes do not outlive the originating operation.
func (s *Service) Record(ctx context.Context, event *Event) error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	if event == nil {
		return errors.New("audit event is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return s.db.WithContext(ctx).Create(event).Error
}

// ListRecent returns the newest audit events first.
func (s *Service) ListRecent(ctx context.Context, limit int) ([]Event, error) {
	if s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		return []Event{}, nil
	}
	var events []Event
	err := s.db.WithContext(ctx).Order("created_at DESC").Limit(limit).Find(&events).Error
	return events, err
}

// LatestUsernamesByResource returns the newest non-empty actor username for
// each requested resource ID. It supports legacy rows whose author account no
// longer exists without leaking audit query details into another subsystem.
func (s *Service) LatestUsernamesByResource(ctx context.Context, action, resource string, resourceIDs []uint) (map[uint]string, error) {
	result := make(map[uint]string)
	if s == nil || s.db == nil {
		return result, ErrUnavailable
	}
	if len(resourceIDs) == 0 {
		return result, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var events []Event
	err := s.db.WithContext(ctx).
		Select("resource_id", "username", "created_at").
		Where("action = ? AND resource = ? AND resource_id IN ?", action, resource, resourceIDs).
		Order("created_at DESC").
		Find(&events).Error
	if err != nil {
		return result, err
	}
	for _, event := range events {
		if _, exists := result[event.ResourceID]; exists {
			continue
		}
		if username := strings.TrimSpace(event.Username); username != "" {
			result[event.ResourceID] = username
		}
	}
	return result, nil
}
