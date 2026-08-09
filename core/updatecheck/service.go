package updatecheck

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/0xmattg/go-press/core/option"
	"github.com/0xmattg/go-press/pkg/logger"
	"github.com/0xmattg/go-press/pkg/semver"
)

const (
	minimumTTL       = 6 * time.Hour
	maximumTTL       = 7 * 24 * time.Hour
	defaultTTL       = 24 * time.Hour
	firstCheckMin    = 5 * time.Minute
	firstCheckWindow = 25 * time.Minute
)

type Store interface {
	Get(name string) string
	Set(name, value string) error
}

type persistedState struct {
	NextCheckAt  time.Time         `json:"next_check_at"`
	LastCheckAt  time.Time         `json:"last_check_at,omitempty"`
	FailureCount int               `json:"failure_count,omitempty"`
	Results      map[string]Status `json:"results,omitempty"`
}

type Service struct {
	store    Store
	registry *Registry
	client   *Client

	mu       sync.Mutex
	state    persistedState
	inFlight bool
	now      func() time.Time
	random   func(time.Duration) time.Duration
}

func NewService(store Store, registry *Registry, client *Client) *Service {
	s := &Service{
		store:    store,
		registry: registry,
		client:   client,
		now:      func() time.Time { return time.Now().UTC() },
		random:   secureRandomDuration,
	}
	s.load()
	return s
}

func (s *Service) Registry() *Registry { return s.registry }

func (s *Service) Enabled() bool {
	return s != nil && s.store != nil && s.store.Get(option.KeyUpdateCheckEnabled) == "1"
}

// Prepare establishes a randomized first check without making a startup
// network request. It is safe to call on every boot because the timestamp is
// persisted across restarts.
func (s *Service) Prepare() {
	if !s.Enabled() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.state.NextCheckAt.IsZero() {
		return
	}
	s.state.NextCheckAt = s.now().Add(firstCheckMin + s.random(firstCheckWindow))
	s.persistLocked()
}

// RunDue is scheduled frequently by core but only performs network I/O when
// the persisted deadline is due.
func (s *Service) RunDue(ctx context.Context) error {
	if !s.Enabled() {
		return nil
	}
	s.mu.Lock()
	if s.inFlight {
		s.mu.Unlock()
		return nil
	}
	if s.state.NextCheckAt.IsZero() {
		s.state.NextCheckAt = s.now().Add(firstCheckMin + s.random(firstCheckWindow))
		s.persistLocked()
		s.mu.Unlock()
		return nil
	}
	if s.now().Before(s.state.NextCheckAt) {
		s.mu.Unlock()
		return nil
	}
	s.inFlight = true
	s.mu.Unlock()

	err := s.check(ctx)

	s.mu.Lock()
	s.inFlight = false
	s.mu.Unlock()
	return err
}

func (s *Service) Snapshot(kind Kind, slug string) Status {
	if s == nil || !s.Enabled() {
		return Status{}
	}
	s.mu.Lock()
	status := s.state.Results[string(kind)+":"+strings.TrimSpace(slug)]
	s.mu.Unlock()
	// A process may have been upgraded since this result was persisted. Always
	// compare against the currently registered version so a stale badge cannot
	// announce the version that is already running.
	if target, ok := s.registry.Get(kind, slug); ok && status.LatestVersion != "" {
		status.CurrentVersion = target.CurrentVersion
		if comparison, err := semver.Compare(target.CurrentVersion, status.LatestVersion); err == nil {
			status.HasUpdate = comparison < 0
		}
	}
	return status
}

func (s *Service) check(ctx context.Context) error {
	targets := s.registry.All()
	if len(targets) == 0 {
		return nil
	}
	groups := make(map[string][]Target)
	for _, target := range targets {
		groups[target.Endpoint] = append(groups[target.Endpoint], target)
	}
	endpoints := make([]string, 0, len(groups))
	for endpoint := range groups {
		endpoints = append(endpoints, endpoint)
	}
	sort.Strings(endpoints)

	checkedAt := s.now()
	nextTTL := maximumTTL
	haveSuccess := false
	results := make(map[string]Status)
	var failures []string
	for _, endpoint := range endpoints {
		requestTargets := groups[endpoint]
		var instance *Instance
		for _, target := range requestTargets {
			if !target.ReportInstallation {
				continue
			}
			value, instanceErr := s.ensureInstance()
			if instanceErr != nil {
				s.recordFailure(instanceErr)
				return instanceErr
			}
			instance = &value
			break
		}
		response, checkErr := s.client.Check(ctx, endpoint, Request{
			ProtocolVersion: ProtocolVersion,
			Instance:        instance,
			Targets:         requestTargets,
		})
		if checkErr != nil {
			failures = append(failures, checkErr.Error())
			continue
		}
		haveSuccess = true
		ttl := clampTTL(time.Duration(response.TTLSeconds) * time.Second)
		if ttl < nextTTL {
			nextTTL = ttl
		}
		requested := make(map[string]Target, len(requestTargets))
		for _, target := range requestTargets {
			requested[target.Key()] = target
		}
		for _, available := range response.Updates {
			target, ok := requested[available.Key()]
			if !ok || !validUpdate(available) {
				continue
			}
			comparison, compareErr := semver.Compare(target.CurrentVersion, available.LatestVersion)
			if compareErr != nil {
				continue
			}
			results[target.Key()] = Status{
				Kind:                    target.Kind,
				Slug:                    target.Slug,
				CurrentVersion:          target.CurrentVersion,
				LatestVersion:           available.LatestVersion,
				MinimumSupportedVersion: available.MinimumSupportedVersion,
				Severity:                normalizeSeverity(available.Severity),
				ReleaseURL:              available.ReleaseURL,
				ReleasedAt:              available.ReleasedAt,
				CheckedAt:               checkedAt,
				HasUpdate:               comparison < 0,
			}
		}
	}

	if len(failures) > 0 {
		failureErr := fmt.Errorf("update check failed: %s", strings.Join(failures, "; "))
		s.recordFailure(failureErr)
		return failureErr
	}
	if !haveSuccess {
		nextTTL = defaultTTL
	}
	s.mu.Lock()
	if s.state.Results == nil {
		s.state.Results = make(map[string]Status)
	}
	for key, status := range results {
		s.state.Results[key] = status
	}
	s.state.LastCheckAt = checkedAt
	s.state.NextCheckAt = checkedAt.Add(nextTTL)
	s.state.FailureCount = 0
	s.persistLocked()
	s.mu.Unlock()
	logger.Info("GoPress update check completed", "targets", len(targets), "next_check_at", checkedAt.Add(nextTTL))
	return nil
}

func (s *Service) ensureInstance() (Instance, error) {
	id := strings.TrimSpace(s.store.Get(KeyInstallationID))
	if id == "" {
		var err error
		id, err = NewInstallationID()
		if err != nil {
			return Instance{}, err
		}
		if err := s.store.Set(KeyInstallationID, id); err != nil {
			return Instance{}, fmt.Errorf("persist update installation ID: %w", err)
		}
	}
	policy := strings.TrimSpace(s.store.Get(KeyPolicyVersion))
	if policy == "" {
		policy = CurrentPolicyVersion
		if err := s.store.Set(KeyPolicyVersion, policy); err != nil {
			return Instance{}, fmt.Errorf("persist update policy version: %w", err)
		}
		if err := s.store.Set(KeyPolicyAcceptedAt, s.now().Format(time.RFC3339)); err != nil {
			return Instance{}, fmt.Errorf("persist update policy acceptance: %w", err)
		}
	}
	return Instance{ID: id, PolicyVersion: policy}, nil
}

func NewInstallationID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate update installation ID: %w", err)
	}
	// Mark the random value as UUID v4 while keeping the implementation free of
	// another dependency.
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	hexID := hex.EncodeToString(raw)
	return hexID[0:8] + "-" + hexID[8:12] + "-" + hexID[12:16] + "-" + hexID[16:20] + "-" + hexID[20:32], nil
}

func (s *Service) recordFailure(err error) {
	s.mu.Lock()
	s.state.FailureCount++
	failures := s.state.FailureCount
	s.state.NextCheckAt = s.now().Add(failureBackoff(s.state.FailureCount))
	s.persistLocked()
	s.mu.Unlock()
	logger.Info("GoPress update check deferred", "error", err, "failures", failures)
}

func failureBackoff(failures int) time.Duration {
	switch {
	case failures <= 1:
		return time.Hour
	case failures == 2:
		return 6 * time.Hour
	default:
		return 24 * time.Hour
	}
}

func clampTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return defaultTTL
	}
	if ttl < minimumTTL {
		return minimumTTL
	}
	if ttl > maximumTTL {
		return maximumTTL
	}
	return ttl
}

func validUpdate(update Update) bool {
	if update.Kind != KindCore && update.Kind != KindTheme && update.Kind != KindPlugin {
		return false
	}
	if strings.TrimSpace(update.Slug) == "" || !semver.Valid(update.LatestVersion) {
		return false
	}
	if update.MinimumSupportedVersion != "" && !semver.Valid(update.MinimumSupportedVersion) {
		return false
	}
	if update.ReleaseURL != "" {
		parsed, err := url.Parse(update.ReleaseURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return false
		}
	}
	return true
}

func normalizeSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "security":
		return "security"
	case "unsupported":
		return "unsupported"
	default:
		return "normal"
	}
}

func (s *Service) load() {
	s.state.Results = make(map[string]Status)
	if s.store == nil {
		return
	}
	raw := strings.TrimSpace(s.store.Get(KeyPersistedCheckState))
	if raw == "" {
		return
	}
	if err := json.Unmarshal([]byte(raw), &s.state); err != nil {
		s.state = persistedState{Results: make(map[string]Status)}
	}
	if s.state.Results == nil {
		s.state.Results = make(map[string]Status)
	}
}

func (s *Service) persistLocked() {
	if s.store == nil {
		return
	}
	raw, err := json.Marshal(s.state)
	if err != nil {
		return
	}
	if err := s.store.Set(KeyPersistedCheckState, string(raw)); err != nil {
		logger.Info("Failed to persist GoPress update state", "error", err)
	}
}

func secureRandomDuration(window time.Duration) time.Duration {
	if window <= 0 {
		return 0
	}
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return window / 2
	}
	var value uint64
	for _, b := range raw {
		value = value<<8 | uint64(b)
	}
	return time.Duration(value % uint64(window))
}
