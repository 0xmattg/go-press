package content

import (
	"context"
	"errors"
	"hash/crc32"
	"strings"
	"sync"
	"time"

	"go-press/pkg/logger"
)

const (
	ContactMessageType = "contact_message"

	ContactMessageMetaEmail    = "email"
	ContactMessageMetaPhone    = "phone"
	ContactMessageMetaRemoteIP = "remote_ip"

	ContactMessageLimit12Hours = 4
	ContactMessageLimit24Hours = 6
)

var ErrContactMessageRateLimited = errors.New("contact message rate limit exceeded")

var contactMessageRateLimitLocks [64]sync.Mutex

type ContactMessageInput struct {
	Name     string
	Email    string
	Phone    string
	Message  string
	RemoteIP string
}

type ContactMessageRateLimitDecision struct {
	Allowed      bool
	Count12Hours int64
	Count24Hours int64
	Limit12Hours int64
	Limit24Hours int64
}

func EvaluateContactMessageRateLimit(count12Hours, count24Hours int64) ContactMessageRateLimitDecision {
	decision := ContactMessageRateLimitDecision{
		Count12Hours: count12Hours,
		Count24Hours: count24Hours,
		Limit12Hours: ContactMessageLimit12Hours,
		Limit24Hours: ContactMessageLimit24Hours,
	}
	decision.Allowed = count12Hours < ContactMessageLimit12Hours && count24Hours < ContactMessageLimit24Hours
	return decision
}

func (r *Repository) CreateContactMessage(ctx context.Context, input ContactMessageInput) error {
	if ctx == nil {
		ctx = context.Background()
	}
	remoteIP := strings.TrimSpace(input.RemoteIP)
	if remoteIP != "" {
		lock := contactMessageRateLimitLock(remoteIP)
		lock.Lock()
		defer lock.Unlock()

		decision, err := r.CheckContactMessageRateLimit(remoteIP, time.Now())
		if err != nil {
			return err
		}
		if !decision.Allowed {
			logger.Warn("contact message rate limit exceeded",
				"remote_ip", remoteIP,
				"count_12h", decision.Count12Hours,
				"limit_12h", decision.Limit12Hours,
				"count_24h", decision.Count24Hours,
				"limit_24h", decision.Limit24Hours,
			)
			return ErrContactMessageRateLimited
		}
	}

	item := &Content{
		Type:    ContactMessageType,
		Status:  StatusDraft,
		Title:   input.Name,
		Content: input.Message,
	}
	meta := map[string]string{}
	if email := strings.TrimSpace(input.Email); email != "" {
		meta[ContactMessageMetaEmail] = email
	}
	if phone := strings.TrimSpace(input.Phone); phone != "" {
		meta[ContactMessageMetaPhone] = phone
	}
	if remoteIP != "" {
		meta[ContactMessageMetaRemoteIP] = remoteIP
	}
	return r.CreateWithMeta(ctx, item, meta)
}

func contactMessageRateLimitLock(remoteIP string) *sync.Mutex {
	idx := crc32.ChecksumIEEE([]byte(remoteIP)) % uint32(len(contactMessageRateLimitLocks))
	return &contactMessageRateLimitLocks[idx]
}

func (r *Repository) CheckContactMessageRateLimit(remoteIP string, now time.Time) (ContactMessageRateLimitDecision, error) {
	remoteIP = strings.TrimSpace(remoteIP)
	if remoteIP == "" {
		return EvaluateContactMessageRateLimit(0, 0), nil
	}
	count12Hours, err := r.CountContactMessagesByIP(remoteIP, now.Add(-12*time.Hour))
	if err != nil {
		return ContactMessageRateLimitDecision{}, err
	}
	count24Hours, err := r.CountContactMessagesByIP(remoteIP, now.Add(-24*time.Hour))
	if err != nil {
		return ContactMessageRateLimitDecision{}, err
	}
	return EvaluateContactMessageRateLimit(count12Hours, count24Hours), nil
}

func (r *Repository) CountContactMessagesByIP(remoteIP string, since time.Time) (int64, error) {
	if r == nil || r.db == nil || strings.TrimSpace(remoteIP) == "" {
		return 0, nil
	}
	contentsTable := Content{}.TableName()
	metaTable := ContentMeta{}.TableName()

	var count int64
	err := r.db.
		Table(contentsTable+" AS c").
		Joins("JOIN "+metaTable+" AS m ON m.content_id = c.id AND m.meta_key = ?", ContactMessageMetaRemoteIP).
		Where("c.type = ? AND m.meta_value = ? AND c.created_at >= ? AND c.deleted_at IS NULL", ContactMessageType, remoteIP, since).
		Count(&count).Error
	return count, err
}
