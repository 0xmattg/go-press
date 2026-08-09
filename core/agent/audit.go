package agent

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/0xmattg/go-press/pkg/dbprefix"

	"gorm.io/gorm"
)

const (
	AuditStarted   = "started"
	AuditSucceeded = "succeeded"
	AuditDenied    = "denied"
	AuditFailed    = "failed"
	AuditReplayed  = "replayed"
)

type AuditEvent struct {
	ID               uint          `gorm:"primaryKey" json:"id"`
	RequestID        string        `gorm:"size:100;not null;index" json:"request_id"`
	TraceID          string        `gorm:"size:100;index" json:"trace_id,omitempty"`
	Adapter          string        `gorm:"size:50" json:"adapter,omitempty"`
	Protocol         string        `gorm:"size:50" json:"protocol,omitempty"`
	ClientVersion    string        `gorm:"size:50" json:"client_version,omitempty"`
	PrincipalKind    PrincipalKind `gorm:"size:30" json:"principal_kind,omitempty"`
	SubjectID        uint          `gorm:"index" json:"subject_id,omitempty"`
	Username         string        `gorm:"size:100" json:"username,omitempty"`
	CredentialID     uint          `gorm:"index" json:"credential_id,omitempty"`
	ToolName         string        `gorm:"size:150;index" json:"tool_name,omitempty"`
	ToolOwner        string        `gorm:"size:100" json:"tool_owner,omitempty"`
	Risk             RiskLevel     `gorm:"size:30" json:"risk,omitempty"`
	Status           string        `gorm:"size:30;not null;index" json:"status"`
	ErrorCode        ErrorCode     `gorm:"size:50" json:"error_code,omitempty"`
	DurationMS       int64         `json:"duration_ms"`
	ArgumentsSummary string        `gorm:"type:text" json:"arguments_summary,omitempty"`
	ResultHash       string        `gorm:"size:64" json:"result_hash,omitempty"`
	SourceDigest     string        `gorm:"size:128" json:"source_digest,omitempty"`
	UserAgent        string        `gorm:"size:255" json:"user_agent,omitempty"`
	CreatedAt        time.Time     `json:"created_at"`
}

func (AuditEvent) TableName() string { return dbprefix.Table("agent_audit_events") }

type AuditRecorder interface {
	Record(context.Context, *AuditEvent) error
}

type AuditStore struct {
	db *gorm.DB
}

type AuditQuery struct {
	Page         int
	PerPage      int
	ToolName     string
	Status       string
	CredentialID uint
}

type AuditPage struct {
	Events     []AuditEvent `json:"events"`
	Page       int          `json:"page"`
	PerPage    int          `json:"per_page"`
	Total      int64        `json:"total"`
	TotalPages int          `json:"total_pages"`
}

func NewAuditStore(db *gorm.DB) *AuditStore { return &AuditStore{db: db} }

func (s *AuditStore) Record(ctx context.Context, event *AuditEvent) error {
	if s == nil || s.db == nil || event == nil {
		return NewError(CodeAuditUnavailable, "agent audit store unavailable")
	}
	event.UserAgent = truncate(event.UserAgent, 255)
	event.SourceDigest = truncate(event.SourceDigest, 128)
	event.ArgumentsSummary = truncate(event.ArgumentsSummary, 4096)
	if err := s.db.WithContext(nonNilContext(ctx)).Create(event).Error; err != nil {
		return WrapError(CodeAuditUnavailable, "failed to persist agent audit event", err)
	}
	return nil
}

func (s *AuditStore) ListByRequest(ctx context.Context, requestID string) ([]AuditEvent, error) {
	if s == nil || s.db == nil {
		return nil, NewError(CodeAuditUnavailable, "agent audit store unavailable")
	}
	var events []AuditEvent
	err := s.db.WithContext(nonNilContext(ctx)).Where("request_id = ?", requestID).Order("id ASC").Find(&events).Error
	return events, err
}

// Query returns a bounded, newest-first page for the Agent audit admin view.
// It never exposes Tool argument values because AuditEvent stores only the
// size/hash/top-level-key summary produced by Executor.
func (s *AuditStore) Query(ctx context.Context, query AuditQuery) (*AuditPage, error) {
	if s == nil || s.db == nil {
		return nil, NewError(CodeAuditUnavailable, "agent audit store unavailable")
	}
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PerPage < 1 {
		query.PerPage = 50
	}
	if query.PerPage > 100 {
		query.PerPage = 100
	}
	query.ToolName = strings.TrimSpace(query.ToolName)
	query.Status = strings.TrimSpace(query.Status)
	if len(query.ToolName) > 150 || (query.Status != "" && !validAuditStatus(query.Status)) {
		return nil, NewError(CodeInvalidRequest, "valid audit tool and status filters are required")
	}
	db := s.db.WithContext(nonNilContext(ctx)).Model(&AuditEvent{})
	if query.ToolName != "" {
		db = db.Where("tool_name = ?", query.ToolName)
	}
	if query.Status != "" {
		db = db.Where("status = ?", query.Status)
	}
	if query.CredentialID > 0 {
		db = db.Where("credential_id = ?", query.CredentialID)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, WrapError(CodeAuditUnavailable, "failed to count agent audit events", err)
	}
	events := make([]AuditEvent, 0)
	if err := db.Order("id DESC").Offset((query.Page - 1) * query.PerPage).Limit(query.PerPage).Find(&events).Error; err != nil {
		return nil, WrapError(CodeAuditUnavailable, "failed to query agent audit events", err)
	}
	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(query.PerPage) - 1) / int64(query.PerPage))
	}
	return &AuditPage{Events: events, Page: query.Page, PerPage: query.PerPage, Total: total, TotalPages: totalPages}, nil
}

func validAuditStatus(status string) bool {
	switch status {
	case AuditStarted, AuditSucceeded, AuditDenied, AuditFailed, AuditReplayed:
		return true
	default:
		return false
	}
}

type argumentAuditSummary struct {
	Size int      `json:"size"`
	Hash string   `json:"hash"`
	Keys []string `json:"keys,omitempty"`
}

func summarizeArguments(raw json.RawMessage) string {
	summary := argumentAuditSummary{Size: len(raw), Hash: payloadHash(raw)}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) == nil {
		for key := range object {
			summary.Keys = append(summary.Keys, strings.TrimSpace(key))
		}
		sort.Strings(summary.Keys)
	}
	encoded, _ := json.Marshal(summary)
	return string(encoded)
}

func truncate(value string, length int) string {
	value = strings.TrimSpace(value)
	if len(value) <= length {
		return value
	}
	return value[:length]
}

var _ AuditRecorder = (*AuditStore)(nil)
