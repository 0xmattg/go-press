package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

type RiskPolicy interface {
	Allow(Principal, Tool) bool
}

type revisionedRiskPolicy interface {
	Revision() uint64
}

type StaticRiskPolicy struct {
	MaxRisk RiskLevel
}

func (p StaticRiskPolicy) Allow(_ Principal, tool Tool) bool {
	maxRisk := p.MaxRisk.rank()
	return maxRisk <= RiskCritical.rank() && tool.Risk.rank() <= maxRisk
}

type Executor struct {
	registry    *Registry
	principals  PrincipalValidator
	authorizer  *Authorizer
	idempotency *IdempotencyStore
	audit       AuditRecorder
	risk        RiskPolicy

	MaxArgumentsBytes int
	MaxOutputBytes    int
	MaxJSONDepth      int
	DefaultTimeout    time.Duration

	gatesMu sync.Mutex
	gates   map[string]chan struct{}
}

func NewExecutor(registry *Registry, principals PrincipalValidator, authorizer *Authorizer, idempotency *IdempotencyStore, audit AuditRecorder) *Executor {
	return &Executor{
		registry: registry, principals: principals, authorizer: authorizer,
		idempotency: idempotency, audit: audit, risk: StaticRiskPolicy{MaxRisk: RiskRead},
		MaxArgumentsBytes: DefaultMaxArgumentsBytes, MaxOutputBytes: DefaultMaxOutputBytes,
		MaxJSONDepth: DefaultMaxJSONDepth, DefaultTimeout: 15 * time.Second,
		gates: make(map[string]chan struct{}),
	}
}

func (e *Executor) SetRiskPolicy(policy RiskPolicy) {
	if e != nil && policy != nil {
		e.risk = policy
	}
}

func (e *Executor) Execute(ctx context.Context, call Call) (*Result, error) {
	started := time.Now()
	ctx = nonNilContext(ctx)
	call.RequestID = strings.TrimSpace(call.RequestID)
	call.ToolName = strings.TrimSpace(call.ToolName)
	if call.RequestID == "" || call.ToolName == "" {
		return nil, NewError(CodeInvalidRequest, "request_id and tool_name are required")
	}
	if len(call.Arguments) == 0 {
		call.Arguments = json.RawMessage(`{}`)
	}
	if e == nil || e.registry == nil || e.principals == nil || e.authorizer == nil || e.audit == nil {
		return nil, NewError(CodeInternal, "agent executor is unavailable")
	}

	principal, err := e.principals.ValidatePrincipal(ctx, call.Principal)
	if err != nil {
		return nil, e.fail(ctx, started, call, RegisteredTool{}, call.Principal, normalizeExecutionError(err, CodeUnauthenticated))
	}
	call.Principal = principal
	registered, ok := e.registry.get(call.ToolName)
	if !ok {
		return nil, e.fail(ctx, started, call, RegisteredTool{}, principal, NewError(CodeNotFound, "agent tool not found"))
	}
	tool := registered.Tool
	if err := ValidateJSON(call.Arguments, tool.InputSchema, e.MaxArgumentsBytes, e.MaxJSONDepth, false); err != nil {
		return nil, e.fail(ctx, started, call, registered, principal, err)
	}
	requirement := tool.Permission
	if tool.ResolvePermission != nil {
		requirement, err = resolveToolPermission(ctx, tool.ResolvePermission, principal, call.Arguments)
		if err != nil {
			return nil, e.fail(ctx, started, call, registered, principal, normalizeExecutionError(err, CodePermissionDenied))
		}
	}
	if err := e.authorizer.Authorize(ctx, principal, requirement); err != nil {
		return nil, e.fail(ctx, started, call, registered, principal, err)
	}
	if e.risk == nil || !e.risk.Allow(principal, tool) {
		return nil, e.fail(ctx, started, call, registered, principal, NewError(CodeRiskDenied, "tool risk is disabled by current agent policy"))
	}
	if tool.RequiresConfirmation && !confirmedArguments(call.Arguments) {
		return nil, e.fail(ctx, started, call, registered, principal, NewError(CodeConfirmationRequired, "tool requires explicit confirmation"))
	}
	if err := e.record(ctx, started, call, registered, principal, AuditStarted, nil, nil); err != nil {
		return nil, err
	}

	var decision IdempotencyDecision
	if tool.Mutability == MutabilityWrite {
		argumentKey, keyErr := idempotencyKeyFromArguments(call.Arguments)
		if keyErr != nil {
			return nil, e.fail(ctx, started, call, registered, principal, keyErr)
		}
		if strings.TrimSpace(call.IdempotencyKey) == "" {
			call.IdempotencyKey = argumentKey
		} else if argumentKey != "" && call.IdempotencyKey != argumentKey {
			return nil, e.fail(ctx, started, call, registered, principal, NewError(CodeInvalidArguments, "idempotency keys do not match"))
		}
		if !tool.Idempotent || strings.TrimSpace(call.IdempotencyKey) == "" || e.idempotency == nil {
			err := NewError(CodeIdempotencyRequired, "write tool requires an idempotency key")
			return nil, e.fail(ctx, started, call, registered, principal, err)
		}
		decision, err = e.idempotency.Begin(ctx, principal.CredentialID, tool.Name, call.IdempotencyKey, call.Arguments)
		if err != nil {
			return nil, e.fail(ctx, started, call, registered, principal, err)
		}
		if decision.Replayed {
			if decision.Err != nil {
				return nil, e.fail(ctx, started, call, registered, principal, decision.Err)
			}
			result := &Result{RequestID: call.RequestID, ToolName: tool.Name, Output: decision.Output, Replayed: true}
			if err := e.record(ctx, started, call, registered, principal, AuditReplayed, decision.Output, nil); err != nil {
				return nil, err
			}
			return result, nil
		}
	}

	executionCtx, cancel := context.WithTimeout(ctx, e.toolTimeout(tool))
	defer cancel()
	release, err := e.acquire(executionCtx, tool)
	if err != nil {
		executionErr := normalizeExecutionError(err, CodeTimeout)
		e.failIdempotency(ctx, decision, executionErr)
		return nil, e.fail(ctx, started, call, registered, principal, executionErr)
	}
	defer release()
	executionCtx = WithPrincipal(executionCtx, principal)

	outputValue, handlerErr := invokeToolHandler(executionCtx, tool.Handler, Invocation{
		RequestID: call.RequestID, ToolName: tool.Name, Arguments: call.Arguments,
		IdempotencyKey: call.IdempotencyKey, Principal: principal, Client: call.Client,
	})
	if handlerErr != nil {
		executionErr := normalizeExecutionError(handlerErr, CodeInternal)
		e.failIdempotency(ctx, decision, executionErr)
		return nil, e.fail(ctx, started, call, registered, principal, executionErr)
	}
	resourceType := ""
	resourceID := uint(0)
	if resource, ok := outputValue.(ResourceResult); ok {
		outputValue = resource.Value
		resourceType = strings.TrimSpace(resource.ResourceType)
		resourceID = resource.ResourceID
	}
	output, err := json.Marshal(outputValue)
	if err != nil {
		executionErr := WrapError(CodeInvalidResult, "tool returned a non-JSON result", err)
		e.failIdempotency(ctx, decision, executionErr)
		return nil, e.fail(ctx, started, call, registered, principal, executionErr)
	}
	if err := ValidateJSON(output, tool.OutputSchema, e.MaxOutputBytes, e.MaxJSONDepth, true); err != nil {
		e.failIdempotency(ctx, decision, err)
		return nil, e.fail(ctx, started, call, registered, principal, err)
	}
	if decision.Acquired {
		if err := e.idempotency.Complete(ctx, decision.Record.ID, output, resourceType, resourceID); err != nil {
			return nil, e.fail(ctx, started, call, registered, principal, err)
		}
	}
	if err := e.record(ctx, started, call, registered, principal, AuditSucceeded, output, nil); err != nil {
		return nil, err
	}
	return &Result{RequestID: call.RequestID, ToolName: tool.Name, Output: output}, nil
}

func resolveToolPermission(ctx context.Context, resolver PermissionResolver, principal Principal, arguments json.RawMessage) (requirement PermissionRequirement, err error) {
	defer func() {
		if recover() != nil {
			err = NewError(CodeInternal, "tool permission resolver failed")
		}
	}()
	return resolver(ctx, principal, arguments)
}

func invokeToolHandler(ctx context.Context, handler Handler, invocation Invocation) (output any, err error) {
	defer func() {
		if recover() != nil {
			err = NewError(CodeInternal, "tool execution failed")
		}
	}()
	return handler(ctx, invocation)
}

func (e *Executor) VisibleTools(ctx context.Context, supplied Principal) (Snapshot, error) {
	if e == nil || e.registry == nil || e.principals == nil || e.authorizer == nil {
		return Snapshot{}, NewError(CodeInternal, "agent executor is unavailable")
	}
	principal, err := e.principals.ValidatePrincipal(nonNilContext(ctx), supplied)
	if err != nil {
		return Snapshot{}, normalizeExecutionError(err, CodeUnauthenticated)
	}
	snapshot := e.registry.Snapshot()
	visible := make([]RegisteredTool, 0, len(snapshot.Tools))
	for _, registered := range snapshot.Tools {
		if e.risk != nil && e.risk.Allow(principal, registered.Tool) &&
			e.authorizer.CanDiscover(principal, registered.Tool.Permission) {
			visible = append(visible, registered)
		}
	}
	snapshot.Tools = visible
	if policy, ok := e.risk.(revisionedRiskPolicy); ok {
		snapshot.Revision += policy.Revision()
	}
	return snapshot, nil
}

func idempotencyKeyFromArguments(arguments json.RawMessage) (string, error) {
	var envelope struct {
		IdempotencyKey string `json:"idempotency_key"`
	}
	if err := json.Unmarshal(arguments, &envelope); err != nil {
		return "", NewError(CodeInvalidArguments, "invalid write arguments")
	}
	key := strings.TrimSpace(envelope.IdempotencyKey)
	if key != "" && (len(key) < 8 || len(key) > 200) {
		return "", NewError(CodeInvalidArguments, "idempotency_key must contain 8 to 200 characters")
	}
	return key, nil
}

func confirmedArguments(arguments json.RawMessage) bool {
	var envelope struct {
		Confirm bool `json:"confirm"`
	}
	return json.Unmarshal(arguments, &envelope) == nil && envelope.Confirm
}

func (e *Executor) fail(ctx context.Context, started time.Time, call Call, tool RegisteredTool, principal Principal, err error) error {
	status := AuditFailed
	code := ErrorCodeOf(err)
	if code == CodeUnauthenticated || code == CodeInsufficientScope || code == CodePermissionDenied || code == CodeRiskDenied || code == CodeConfirmationRequired {
		status = AuditDenied
	}
	if auditErr := e.record(ctx, started, call, tool, principal, status, nil, err); auditErr != nil {
		return auditErr
	}
	return err
}

func (e *Executor) record(ctx context.Context, started time.Time, call Call, registered RegisteredTool, principal Principal, status string, output json.RawMessage, callErr error) error {
	if e == nil || e.audit == nil {
		return NewError(CodeAuditUnavailable, "agent audit is required")
	}
	event := &AuditEvent{
		RequestID: call.RequestID, TraceID: call.Client.TraceID,
		Adapter: call.Client.Adapter, Protocol: call.Client.Protocol, ClientVersion: call.Client.Version,
		PrincipalKind: principal.Kind, SubjectID: principal.SubjectID, Username: principal.Username,
		CredentialID: principal.CredentialID, ToolName: call.ToolName, ToolOwner: registered.Owner,
		Risk: registered.Tool.Risk, Status: status, DurationMS: time.Since(started).Milliseconds(),
		ArgumentsSummary: summarizeArguments(call.Arguments), SourceDigest: call.Client.SourceDigest,
		UserAgent: call.Client.UserAgent,
	}
	if callErr != nil {
		event.ErrorCode = ErrorCodeOf(callErr)
	}
	if len(output) > 0 {
		event.ResultHash = payloadHash(output)
	}
	if err := e.audit.Record(nonNilContext(ctx), event); err != nil {
		return WrapError(CodeAuditUnavailable, "agent audit is unavailable", err)
	}
	return nil
}

func (e *Executor) failIdempotency(ctx context.Context, decision IdempotencyDecision, err error) {
	if decision.Acquired && e.idempotency != nil {
		_ = e.idempotency.Fail(ctx, decision.Record.ID, err)
	}
}

func (e *Executor) toolTimeout(tool Tool) time.Duration {
	if tool.Timeout > 0 && tool.Timeout <= time.Minute {
		return tool.Timeout
	}
	if e.DefaultTimeout > 0 {
		return e.DefaultTimeout
	}
	return 15 * time.Second
}

func (e *Executor) acquire(ctx context.Context, tool Tool) (func(), error) {
	if tool.MaxConcurrency <= 0 {
		return func() {}, nil
	}
	e.gatesMu.Lock()
	gate := e.gates[tool.Name]
	if gate == nil || cap(gate) != tool.MaxConcurrency {
		gate = make(chan struct{}, tool.MaxConcurrency)
		e.gates[tool.Name] = gate
	}
	e.gatesMu.Unlock()
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func normalizeExecutionError(err error, fallback ErrorCode) error {
	if err == nil {
		return nil
	}
	var agentErr *Error
	if errors.As(err, &agentErr) {
		return agentErr
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return WrapError(CodeTimeout, "tool execution timed out", err)
	}
	if errors.Is(err, context.Canceled) {
		return WrapError(CodeCanceled, "tool execution was canceled", err)
	}
	message := "tool execution failed"
	if fallback == CodeUnauthenticated {
		message = "agent authentication failed"
	} else if fallback == CodePermissionDenied {
		message = "tool permission resolution failed"
	}
	return WrapError(fallback, message, err)
}
