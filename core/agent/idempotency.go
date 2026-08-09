package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/0xmattg/go-press/pkg/dbprefix"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	IdempotencyInProgress = "in_progress"
	IdempotencyCompleted  = "completed"
	IdempotencyFailed     = "failed"
)

type IdempotencyRecord struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	CredentialID   uint      `gorm:"not null;uniqueIndex:idx_agent_idempotency,priority:1" json:"credential_id"`
	ToolName       string    `gorm:"size:150;not null;uniqueIndex:idx_agent_idempotency,priority:2" json:"tool_name"`
	IdempotencyKey string    `gorm:"size:200;not null;uniqueIndex:idx_agent_idempotency,priority:3" json:"idempotency_key"`
	RequestHash    string    `gorm:"size:64;not null" json:"request_hash"`
	Status         string    `gorm:"size:20;not null;index" json:"status"`
	ResourceType   string    `gorm:"size:100" json:"resource_type,omitempty"`
	ResourceID     uint      `json:"resource_id,omitempty"`
	ResultJSON     string    `gorm:"type:text" json:"-"`
	ResultHash     string    `gorm:"size:64" json:"result_hash,omitempty"`
	ErrorCode      ErrorCode `gorm:"size:50" json:"error_code,omitempty"`
	ErrorMessage   string    `gorm:"size:255" json:"error_message,omitempty"`
	ExpiresAt      time.Time `gorm:"not null;index" json:"expires_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (IdempotencyRecord) TableName() string { return dbprefix.Table("agent_idempotency_records") }

type IdempotencyDecision struct {
	Record   IdempotencyRecord
	Acquired bool
	Replayed bool
	Output   json.RawMessage
	Err      error
}

type IdempotencyStore struct {
	db  *gorm.DB
	now func() time.Time
	ttl time.Duration
}

func NewIdempotencyStore(db *gorm.DB) *IdempotencyStore {
	return &IdempotencyStore{db: db, now: func() time.Time { return time.Now().UTC() }, ttl: 24 * time.Hour}
}

func (s *IdempotencyStore) Begin(ctx context.Context, credentialID uint, toolName, key string, arguments json.RawMessage) (IdempotencyDecision, error) {
	toolName = strings.TrimSpace(toolName)
	key = strings.TrimSpace(key)
	if s == nil || s.db == nil {
		return IdempotencyDecision{}, NewError(CodeInternal, "idempotency store unavailable")
	}
	if credentialID == 0 || toolName == "" || key == "" || len(key) > 200 {
		return IdempotencyDecision{}, NewError(CodeIdempotencyRequired, "valid idempotency key required")
	}
	requestHash := canonicalPayloadHash(arguments)
	ctx = nonNilContext(ctx)
	for attempt := 0; attempt < 2; attempt++ {
		record := IdempotencyRecord{
			CredentialID: credentialID, ToolName: toolName, IdempotencyKey: key,
			RequestHash: requestHash, Status: IdempotencyInProgress, ExpiresAt: s.now().Add(s.ttl),
		}
		created := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&record)
		if created.Error != nil {
			return IdempotencyDecision{}, WrapError(CodeInternal, "failed to reserve idempotency key", created.Error)
		}
		if created.RowsAffected == 1 {
			return IdempotencyDecision{Record: record, Acquired: true}, nil
		}
		var existing IdempotencyRecord
		if err := s.db.WithContext(ctx).
			Where("credential_id = ? AND tool_name = ? AND idempotency_key = ?", credentialID, toolName, key).
			First(&existing).Error; err != nil {
			return IdempotencyDecision{}, WrapError(CodeInternal, "failed to load idempotency record", err)
		}
		if !existing.ExpiresAt.After(s.now()) {
			deleted := s.db.WithContext(ctx).Where("id = ? AND expires_at <= ?", existing.ID, s.now()).Delete(&IdempotencyRecord{})
			if deleted.Error != nil {
				return IdempotencyDecision{}, WrapError(CodeInternal, "failed to expire idempotency record", deleted.Error)
			}
			continue
		}
		if existing.RequestHash != requestHash {
			return IdempotencyDecision{}, NewError(CodeConflict, "idempotency key was used with different arguments")
		}
		switch existing.Status {
		case IdempotencyCompleted:
			return IdempotencyDecision{Record: existing, Replayed: true, Output: json.RawMessage(existing.ResultJSON)}, nil
		case IdempotencyFailed:
			return IdempotencyDecision{Record: existing, Replayed: true, Err: NewError(existing.ErrorCode, existing.ErrorMessage)}, nil
		default:
			return IdempotencyDecision{}, NewError(CodeIdempotencyPending, "an equivalent operation is still in progress")
		}
	}
	return IdempotencyDecision{}, NewError(CodeConflict, "idempotency key could not be reserved")
}

func (s *IdempotencyStore) Complete(ctx context.Context, recordID uint, output json.RawMessage, resourceType string, resourceID uint) error {
	if s == nil || s.db == nil || recordID == 0 {
		return NewError(CodeInternal, "idempotency store unavailable")
	}
	result := s.db.WithContext(nonNilContext(ctx)).Model(&IdempotencyRecord{}).
		Where("id = ? AND status = ?", recordID, IdempotencyInProgress).
		Updates(map[string]any{
			"status": IdempotencyCompleted, "result_json": string(output), "result_hash": payloadHash(output),
			"resource_type": strings.TrimSpace(resourceType), "resource_id": resourceID,
		})
	if result.Error != nil {
		return WrapError(CodeInternal, "failed to complete idempotency record", result.Error)
	}
	if result.RowsAffected != 1 {
		return NewError(CodeConflict, "idempotency record is no longer pending")
	}
	return nil
}

func (s *IdempotencyStore) Fail(ctx context.Context, recordID uint, err error) error {
	if s == nil || s.db == nil || recordID == 0 {
		return NewError(CodeInternal, "idempotency store unavailable")
	}
	code := ErrorCodeOf(err)
	message := "operation failed"
	var agentErr *Error
	if errors.As(err, &agentErr) && agentErr.Message != "" {
		message = agentErr.Message
	}
	if len(message) > 255 {
		message = message[:255]
	}
	return s.db.WithContext(nonNilContext(ctx)).Model(&IdempotencyRecord{}).
		Where("id = ? AND status = ?", recordID, IdempotencyInProgress).
		Updates(map[string]any{"status": IdempotencyFailed, "error_code": code, "error_message": message}).Error
}

func payloadHash(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func canonicalPayloadHash(payload []byte) string {
	value, err := decodeJSON(payload)
	if err != nil {
		return payloadHash(payload)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return payloadHash(payload)
	}
	return payloadHash(canonical)
}
