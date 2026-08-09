package agent

import (
	"context"
	"sort"
	"strings"
)

type PrincipalKind string

const (
	PrincipalUser           PrincipalKind = "user"
	PrincipalServiceAccount PrincipalKind = "service_account"
)

// Principal is the current, credential-backed actor used for authorization.
// Executor refreshes it through PrincipalValidator before every Tool call.
type Principal struct {
	Kind         PrincipalKind `json:"kind"`
	SubjectID    uint          `json:"subject_id"`
	Username     string        `json:"username"`
	Role         string        `json:"role"`
	Scopes       []string      `json:"scopes"`
	Audience     string        `json:"audience"`
	CredentialID uint          `json:"credential_id"`
}

func (p Principal) Valid() bool {
	return (p.Kind == PrincipalUser || p.Kind == PrincipalServiceAccount) &&
		p.SubjectID > 0 && p.Role != "" && p.Audience != "" && p.CredentialID > 0
}

func (p Principal) HasScope(required string) bool {
	required = strings.TrimSpace(required)
	if required == "" {
		return true
	}
	for _, scope := range p.Scopes {
		scope = strings.TrimSpace(scope)
		if scope == required || scope == "*" || scope == "gopress:*" {
			return true
		}
		if strings.HasSuffix(scope, ":*") && strings.HasPrefix(required, strings.TrimSuffix(scope, "*")) {
			return true
		}
	}
	return false
}

func NormalizeScopes(scopes []string) []string {
	seen := make(map[string]struct{}, len(scopes))
	normalized := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if _, duplicate := seen[scope]; duplicate {
			continue
		}
		seen[scope] = struct{}{}
		normalized = append(normalized, scope)
	}
	sort.Strings(normalized)
	return normalized
}

// PrincipalValidator reloads current credential and account state. Executor
// requires one, preventing adapters from authorizing with stale embedded roles.
type PrincipalValidator interface {
	ValidatePrincipal(context.Context, Principal) (Principal, error)
}

type principalContextKey struct{}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	if ctx == nil {
		return Principal{}, false
	}
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}
