package user

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/mail"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrExternalLoginDisabled  = errors.New("external identity login is disabled")
	ErrRegistrationDisabled   = errors.New("public user registration is disabled")
	ErrAccountLinkingDisabled = errors.New("external identity linking is disabled")
	ErrInvalidIdentity        = errors.New("invalid verified identity")
	ErrIdentityConflict       = errors.New("external identity is already linked to another user")
	ErrEmailAlreadyInUse      = errors.New("email belongs to an existing account and must be linked after sign-in")
	ErrLastLoginMethod        = errors.New("cannot remove the last login method")
	ErrAuthenticationRequired = errors.New("authentication required")
)

// VerifiedIdentity is the provider-neutral assertion accepted by core after a
// plugin has completed protocol-specific verification.
type VerifiedIdentity struct {
	Provider      string
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
	DisplayName   string
	AvatarURL     string
	ProfileJSON   string
	VerifiedAt    time.Time
}

type IdentityResult struct {
	User     *User
	Identity *UserIdentity
	Created  bool
}

// IdentityLoginOptions lets a provider narrow core registration policy for a
// single login attempt. It can never enable registration when the site-wide
// policy disables it.
type IdentityLoginOptions struct {
	AllowRegistration bool
}

// IdentityBroker owns external login, account provisioning and binding rules.
type IdentityBroker struct {
	db         *gorm.DB
	users      *Repository
	identities *IdentityRepository
	policy     *RegistrationPolicy
}

func NewIdentityBroker(db *gorm.DB, users *Repository, identities *IdentityRepository, policy *RegistrationPolicy) *IdentityBroker {
	return &IdentityBroker{db: db, users: users, identities: identities, policy: policy}
}

func (b *IdentityBroker) LoginOrRegister(ctx context.Context, verified VerifiedIdentity) (*IdentityResult, error) {
	return b.LoginOrRegisterWithOptions(ctx, verified, IdentityLoginOptions{AllowRegistration: true})
}

func (b *IdentityBroker) LoginOrRegisterWithOptions(ctx context.Context, verified VerifiedIdentity, options IdentityLoginOptions) (*IdentityResult, error) {
	if b == nil || b.db == nil || b.policy == nil || !b.policy.ExternalLoginEnabled() {
		return nil, ErrExternalLoginDisabled
	}
	identity, err := normalizeVerifiedIdentity(verified)
	if err != nil {
		return nil, err
	}

	existing, err := b.identities.FindByKey(ctx, identity.Provider, identity.Issuer, identity.Subject)
	if err == nil {
		return b.finishExistingLogin(ctx, existing, identity.ProfileJSON)
	}
	if !errors.Is(err, ErrIdentityNotFound) {
		return nil, err
	}
	if !options.AllowRegistration || !b.policy.AutoRegistrationEnabled() {
		return nil, ErrRegistrationDisabled
	}

	var result *IdentityResult
	err = b.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		users := NewRepository(tx)
		identities := NewIdentityRepository(tx)

		if identity.Email != nil {
			if _, findErr := users.FindByEmail(*identity.Email); findErr == nil {
				return ErrEmailAlreadyInUse
			} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
				return findErr
			}
		}

		account := &User{
			Username:     generatedUsername(),
			Email:        identity.Email,
			PasswordHash: "",
			DisplayName:  truncateString(strings.TrimSpace(verified.DisplayName), 100),
			AvatarURL:    truncateString(strings.TrimSpace(verified.AvatarURL), 500),
			Role:         b.policy.DefaultRole(),
			IsActive:     true,
		}
		if account.DisplayName == "" {
			account.DisplayName = account.Username
		}
		if err := users.Create(account); err != nil {
			return err
		}

		identity.UserID = account.ID
		if err := identities.Create(ctx, identity); err != nil {
			return err
		}
		result = &IdentityResult{User: account, Identity: identity, Created: true}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (b *IdentityBroker) LinkIdentity(ctx context.Context, userID uint, verified VerifiedIdentity) (*UserIdentity, error) {
	if b == nil || b.policy == nil || !b.policy.ExternalLoginEnabled() {
		return nil, ErrExternalLoginDisabled
	}
	if !b.policy.AccountLinkingEnabled() {
		return nil, ErrAccountLinkingDisabled
	}
	identity, err := normalizeVerifiedIdentity(verified)
	if err != nil {
		return nil, err
	}
	var linked *UserIdentity
	err = b.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var account User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&account, userID).Error; err != nil {
			return err
		}
		if !account.IsActive {
			return ErrAccountDisabled
		}
		identities := NewIdentityRepository(tx)
		if existing, findErr := identities.FindByKey(ctx, identity.Provider, identity.Issuer, identity.Subject); findErr == nil {
			if existing.UserID == userID {
				linked = existing
				return nil
			}
			return ErrIdentityConflict
		} else if !errors.Is(findErr, ErrIdentityNotFound) {
			return findErr
		}
		identity.UserID = userID
		if err := identities.Create(ctx, identity); err != nil {
			return err
		}
		linked = identity
		return nil
	})
	return linked, err
}

// UnlinkIdentity scopes lookup and deletion to userID, preventing callers from
// unlinking another user's identity by guessing its ID.
func (b *IdentityBroker) UnlinkIdentity(ctx context.Context, userID, identityID uint) error {
	if b == nil || b.policy == nil || !b.policy.AccountLinkingEnabled() {
		return ErrAccountLinkingDisabled
	}
	return b.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var account User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&account, userID).Error; err != nil {
			return err
		}
		identities := NewIdentityRepository(tx)
		if _, err := identities.FindByIDForUser(ctx, identityID, userID); err != nil {
			return err
		}
		count, err := identities.CountByUser(ctx, userID)
		if err != nil {
			return err
		}
		if count <= 1 && strings.TrimSpace(account.PasswordHash) == "" {
			return ErrLastLoginMethod
		}
		return identities.DeleteForUser(ctx, identityID, userID)
	})
}

func (b *IdentityBroker) finishExistingLogin(ctx context.Context, identity *UserIdentity, profileJSON string) (*IdentityResult, error) {
	account, err := b.users.FindByID(identity.UserID)
	if err != nil {
		return nil, err
	}
	if !account.IsActive {
		return nil, ErrAccountDisabled
	}
	now := time.Now().UTC()
	_ = b.identities.UpdateUsage(ctx, identity.ID, profileJSON, now)
	_ = b.users.UpdateLastLogin(account.ID)
	identity.LastUsedAt = &now
	if profileJSON != "" {
		identity.ProfileJSON = profileJSON
	}
	return &IdentityResult{User: account, Identity: identity}, nil
}

func normalizeVerifiedIdentity(verified VerifiedIdentity) (*UserIdentity, error) {
	provider := strings.TrimSpace(verified.Provider)
	issuer := strings.TrimSpace(verified.Issuer)
	subject := strings.TrimSpace(verified.Subject)
	if provider == "" || issuer == "" || subject == "" || len(provider) > 64 || len(issuer) > 255 || len(subject) > 255 {
		return nil, ErrInvalidIdentity
	}
	if len(verified.ProfileJSON) > 64*1024 || (verified.ProfileJSON != "" && !json.Valid([]byte(verified.ProfileJSON))) {
		return nil, ErrInvalidIdentity
	}

	var email *string
	if verified.EmailVerified && strings.TrimSpace(verified.Email) != "" {
		normalized := strings.ToLower(strings.TrimSpace(verified.Email))
		parsed, err := mail.ParseAddress(normalized)
		if err != nil || !strings.EqualFold(parsed.Address, normalized) {
			return nil, ErrInvalidIdentity
		}
		email = &normalized
	}
	verifiedAt := verified.VerifiedAt.UTC()
	if verifiedAt.IsZero() {
		verifiedAt = time.Now().UTC()
	}
	return &UserIdentity{
		Provider:      provider,
		Issuer:        issuer,
		Subject:       subject,
		Email:         email,
		EmailVerified: verified.EmailVerified && email != nil,
		ProfileJSON:   verified.ProfileJSON,
		VerifiedAt:    verifiedAt,
	}, nil
}

func generatedUsername() string {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "user-" + hex.EncodeToString([]byte(time.Now().UTC().Format("150405.000000")))
	}
	return "user-" + hex.EncodeToString(random)
}
