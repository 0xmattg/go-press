package metamaskidentity

import (
	"time"

	"go-press/pkg/dbprefix"
)

type walletChallenge struct {
	ID          uint       `gorm:"primaryKey"`
	TokenHash   string     `gorm:"size:64;not null;uniqueIndex:uidx_metamask_challenge_token"`
	NonceHash   string     `gorm:"size:64;not null"`
	MessageHash string     `gorm:"size:64;not null"`
	Address     string     `gorm:"size:42;not null;index:idx_metamask_challenge_address"`
	Scheme      string     `gorm:"size:16;not null"`
	Domain      string     `gorm:"size:255;not null"`
	URI         string     `gorm:"size:2048;not null"`
	ChainID     int        `gorm:"not null"`
	ReturnTo    string     `gorm:"size:1024;not null"`
	IssuedAt    time.Time  `gorm:"not null"`
	ExpiresAt   time.Time  `gorm:"not null;index:idx_metamask_challenge_expiry"`
	UsedAt      *time.Time `gorm:"index:idx_metamask_challenge_used"`
	CreatedAt   time.Time
}

func (walletChallenge) TableName() string {
	return dbprefix.PluginTable(storageSlug, "challenges")
}
