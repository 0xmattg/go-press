package agent

import (
	"context"
	"encoding/json"
	"time"
)

type Mutability string

const (
	MutabilityRead  Mutability = "read"
	MutabilityWrite Mutability = "write"
)

type RiskLevel string

const (
	RiskRead        RiskLevel = "read"
	RiskWrite       RiskLevel = "write"
	RiskPublish     RiskLevel = "publish"
	RiskDestructive RiskLevel = "destructive"
	RiskCritical    RiskLevel = "critical"
)

func (r RiskLevel) rank() int {
	switch r {
	case RiskRead:
		return 1
	case RiskWrite:
		return 2
	case RiskPublish:
		return 3
	case RiskDestructive:
		return 4
	case RiskCritical:
		return 5
	default:
		return 100
	}
}

// PermissionRequirement is the combined token Scope and Core RBAC decision.
// OwnAction is considered only when ResourceOwnerID matches the Principal.
type PermissionRequirement struct {
	Scope           string `json:"scope"`
	Resource        string `json:"resource"`
	Action          string `json:"action"`
	OwnAction       string `json:"own_action,omitempty"`
	ResourceOwnerID uint   `json:"resource_owner_id,omitempty"`
}

type PermissionResolver func(context.Context, Principal, json.RawMessage) (PermissionRequirement, error)

// Invocation is supplied to a Tool Handler after the mandatory execution
// pipeline has refreshed and authorized the Principal.
type Invocation struct {
	RequestID      string
	ToolName       string
	Arguments      json.RawMessage
	IdempotencyKey string
	Principal      Principal
	Client         ClientInfo
}

type Handler func(context.Context, Invocation) (any, error)

// Tool is the protocol-neutral executable capability registered in Core.
type Tool struct {
	Name                 string                `json:"name"`
	Title                string                `json:"title"`
	Description          string                `json:"description"`
	InputSchema          json.RawMessage       `json:"input_schema"`
	OutputSchema         json.RawMessage       `json:"output_schema"`
	Mutability           Mutability            `json:"mutability"`
	Risk                 RiskLevel             `json:"risk"`
	Idempotent           bool                  `json:"idempotent"`
	RequiresConfirmation bool                  `json:"requires_confirmation,omitempty"`
	Permission           PermissionRequirement `json:"permission"`
	ResolvePermission    PermissionResolver    `json:"-"`
	Timeout              time.Duration         `json:"timeout,omitempty"`
	MaxConcurrency       int                   `json:"max_concurrency,omitempty"`
	Handler              Handler               `json:"-"`
}

// ResourceResult lets a write handler associate its persisted resource with
// the idempotency record without leaking transport details into the handler.
// Executor serializes Value as the public Tool output.
type ResourceResult struct {
	Value        any
	ResourceType string
	ResourceID   uint
}

func ResultForResource(value any, resourceType string, resourceID uint) ResourceResult {
	return ResourceResult{Value: value, ResourceType: resourceType, ResourceID: resourceID}
}

type ClientInfo struct {
	Adapter      string `json:"adapter,omitempty"`
	Protocol     string `json:"protocol,omitempty"`
	Version      string `json:"version,omitempty"`
	UserAgent    string `json:"user_agent,omitempty"`
	SourceDigest string `json:"source_digest,omitempty"`
	TraceID      string `json:"trace_id,omitempty"`
}

type Call struct {
	RequestID      string          `json:"request_id"`
	ToolName       string          `json:"tool_name"`
	Arguments      json.RawMessage `json:"arguments"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Principal      Principal       `json:"principal"`
	Client         ClientInfo      `json:"client"`
}

type Result struct {
	RequestID string          `json:"request_id"`
	ToolName  string          `json:"tool_name"`
	Output    json.RawMessage `json:"output"`
	Replayed  bool            `json:"replayed,omitempty"`
}

type RegisteredTool struct {
	Owner string `json:"owner"`
	Tool  Tool   `json:"tool"`
}

type Snapshot struct {
	Revision uint64           `json:"revision"`
	Tools    []RegisteredTool `json:"tools"`
}
