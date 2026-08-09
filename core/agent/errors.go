package agent

import (
	"errors"
	"fmt"
)

// ErrorCode is stable across protocol adapters. MCP, REST, CLI, and future
// transports map these codes into their own wire-level error representation.
type ErrorCode string

const (
	CodeInvalidRequest       ErrorCode = "invalid_request"
	CodeInvalidArguments     ErrorCode = "invalid_arguments"
	CodeInvalidResult        ErrorCode = "invalid_result"
	CodeUnauthenticated      ErrorCode = "unauthenticated"
	CodeInsufficientScope    ErrorCode = "insufficient_scope"
	CodePermissionDenied     ErrorCode = "permission_denied"
	CodeNotFound             ErrorCode = "not_found"
	CodeConflict             ErrorCode = "conflict"
	CodeRiskDenied           ErrorCode = "risk_denied"
	CodeConfirmationRequired ErrorCode = "confirmation_required"
	CodeIdempotencyRequired  ErrorCode = "idempotency_required"
	CodeIdempotencyPending   ErrorCode = "idempotency_pending"
	CodeTimeout              ErrorCode = "timeout"
	CodeCanceled             ErrorCode = "canceled"
	CodeAuditUnavailable     ErrorCode = "audit_unavailable"
	CodeInternal             ErrorCode = "internal_error"
)

// Error is a protocol-neutral, client-safe execution error. Cause is retained
// for server diagnostics but never included by Error().
type Error struct {
	Code           ErrorCode `json:"code"`
	Message        string    `json:"message"`
	RequiredScopes []string  `json:"required_scopes,omitempty"`
	Cause          error     `json:"-"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error { return e.Cause }

func NewError(code ErrorCode, message string) *Error {
	return &Error{Code: code, Message: message}
}

func WrapError(code ErrorCode, message string, cause error) *Error {
	return &Error{Code: code, Message: message, Cause: cause}
}

func ErrorCodeOf(err error) ErrorCode {
	var agentErr *Error
	if errors.As(err, &agentErr) {
		return agentErr.Code
	}
	return CodeInternal
}

func IsErrorCode(err error, code ErrorCode) bool {
	return ErrorCodeOf(err) == code
}

func invalidField(path, message string) *Error {
	if path == "" {
		return NewError(CodeInvalidArguments, message)
	}
	return NewError(CodeInvalidArguments, fmt.Sprintf("%s: %s", path, message))
}
