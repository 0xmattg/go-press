package user

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrAccountDisabled    = errors.New("account is disabled")
	ErrInvalidToken       = errors.New("invalid or expired token")
)

// Claims represents JWT custom claims.
type Claims struct {
	UserID      uint   `json:"uid"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	DisplayName string `json:"display_name"`
	jwt.RegisteredClaims
}

// Auth handles authentication operations.
type Auth struct {
	secret     []byte
	expireTime time.Duration
	repo       *Repository
}

// NewAuth creates a new Auth handler.
func NewAuth(jwtSecret string, expireHours int, repo *Repository) *Auth {
	return &Auth{
		secret:     []byte(jwtSecret),
		expireTime: time.Duration(expireHours) * time.Hour,
		repo:       repo,
	}
}

// HashPassword hashes a plaintext password using bcrypt.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

// CheckPassword verifies a password against a hash.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// Login authenticates a user and returns a JWT token.
func (a *Auth) Login(username, password string) (string, *User, error) {
	u, err := a.repo.FindByUsername(username)
	if err != nil {
		return "", nil, ErrInvalidCredentials
	}
	if !CheckPassword(u.PasswordHash, password) {
		return "", nil, ErrInvalidCredentials
	}
	if !u.IsActive {
		return "", nil, ErrAccountDisabled
	}

	// Update last login
	now := time.Now()
	u.LastLoginAt = &now
	_ = a.repo.Update(u)

	token, err := a.GenerateToken(u)
	if err != nil {
		return "", nil, err
	}
	return token, u, nil
}

// GenerateToken creates a JWT token for a user.
func (a *Auth) GenerateToken(u *User) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:      u.ID,
		Username:    u.Username,
		Role:        u.Role,
		DisplayName: u.DisplayName,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(a.expireTime)),
			Issuer:    "gopress",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.secret)
}

// ParseToken validates a JWT token and returns claims.
func (a *Auth) ParseToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return a.secret, nil
	})
	if err != nil {
		return nil, ErrInvalidToken
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
