package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ilamparithi-in/matfix/internal/api"
	"github.com/ilamparithi-in/matfix/internal/bus"
	"github.com/ilamparithi-in/matfix/internal/correlation"
	"github.com/ilamparithi-in/matfix/internal/persistence"
)

type compiledRule struct {
	rule   WebhookRule
	rooms  []string
	filter *correlation.CompiledFilter
}

// Config holds configuration parameters for WebhookManager.
type Config struct {
	FilePath       string
	Store          persistence.WebhookStore
	Bus            bus.Bus
	DefaultTimeout time.Duration
	Concurrency    int
}

// WebhookManager manages the lifecycle, fast-path dispatch, and retries of outbound webhooks.
type WebhookManager struct {
	fileMgr        *FileManager
	store          persistence.WebhookStore
	bus            bus.Bus
	dispatcher     *Dispatcher
	retryWorker    *RetryWorker
	watcher        *Watcher
	defaultTimeout time.Duration
	concurrency    int

	rulesMu       sync.RWMutex
	rules         map[string]*compiledRule
	rulesByRoom   map[string]map[string]*compiledRule
	wildcardRules map[string]*compiledRule

	subIDs   []bus.SubscriptionID
	ctx      context.Context
	cancel   context.CancelFunc
	stateMu  sync.Mutex
	stopped  bool
	wg       sync.WaitGroup
	sem      chan struct{}
	stopOnce sync.Once
}

// NewManager constructs a ready-to-start WebhookManager.
func NewManager(cfg Config) (*WebhookManager, error) {
	fileMgr := NewFileManager(cfg.FilePath)
	dispatcher := NewDispatcher()

	timeout := cfg.DefaultTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = 4
	}

	m := &WebhookManager{
		fileMgr:        fileMgr,
		store:          cfg.Store,
		bus:            cfg.Bus,
		dispatcher:     dispatcher,
		defaultTimeout: timeout,
		concurrency:    concurrency,
		rules:          make(map[string]*compiledRule),
		rulesByRoom:    make(map[string]map[string]*compiledRule),
		wildcardRules:  make(map[string]*compiledRule),
	}

	m.retryWorker = NewRetryWorker(cfg.Store, dispatcher, concurrency, m.Get)

	// Load initial rules from disk
	if err := m.Reload(); err != nil {
		slog.Warn("webhook: initial load warning (will start with empty rules)", "error", err)
	}

	// Initialize file watcher
	watcher, err := NewWatcher(cfg.FilePath, func() {
		slog.Info("webhook: file change detected, reloading rules", "file", cfg.FilePath)
		if err := m.Reload(); err != nil {
			slog.Error("webhook: reload from disk failed", "error", err)
		}
	})
	if err != nil {
		slog.Warn("webhook: could not start file watcher", "error", err)
	} else {
		m.watcher = watcher
	}

	return m, nil
}

// Start begins event bus subscription, watcher, and retry worker.
func (m *WebhookManager) Start(ctx context.Context) {
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.sem = make(chan struct{}, m.concurrency*4)

	// Crash recovery: restore any entries that were in 'sending' state when daemon last stopped
	if m.store != nil {
		if n, err := m.store.RestoreStuck(m.ctx, time.Now().UnixMilli()); err == nil && n > 0 {
			slog.Warn("webhook: restored stuck retries to failed on startup", "count", n)
		}
	}

	if m.watcher != nil {
		m.watcher.Start(m.ctx)
	}

	if m.retryWorker != nil {
		m.retryWorker.Start(m.ctx)
	}

	if m.bus != nil {
		inboundTypes := []bus.EventType{
			bus.EventInboundMessage,
			bus.EventInboundReaction,
			bus.EventInboundEdit,
			bus.EventInboundRedaction,
			bus.EventInboundMembership,
			bus.EventInboundReceipt,
		}
		for _, et := range inboundTypes {
			id := m.bus.Subscribe(et, m.handleEvent)
			m.subIDs = append(m.subIDs, id)
		}
	}
}

// Stop cleanly terminates all manager components and drains in-flight requests.
func (m *WebhookManager) Stop() {
	m.stopOnce.Do(func() {
		m.stateMu.Lock()
		m.stopped = true
		m.stateMu.Unlock()

		if m.bus != nil {
			for _, id := range m.subIDs {
				m.bus.Unsubscribe(id)
			}
			m.subIDs = nil
		}
		if m.watcher != nil {
			m.watcher.Stop()
		}
		if m.retryWorker != nil {
			m.retryWorker.Stop()
		}
		if m.cancel != nil {
			m.cancel()
		}
		m.wg.Wait()
	})
}

// Reload reads webhooks.yaml from disk and replaces the in-memory rules table atomically.
func (m *WebhookManager) Reload() error {
	rawRules, err := m.fileMgr.Load()
	if err != nil {
		return err
	}

	newRules := make(map[string]*compiledRule)
	newByRoom := make(map[string]map[string]*compiledRule)
	newWildcards := make(map[string]*compiledRule)

	for _, r := range rawRules {
		cf, err := correlation.NewCompiledFilter(&r.Filter)
		if err != nil {
			return fmt.Errorf("compile filter for webhook %q: %w", r.ID, err)
		}

		var rooms []string
		if r.Filter.RoomID != nil && len(r.Filter.RoomID.Include) > 0 {
			rooms = r.Filter.RoomID.Include
		}

		cr := &compiledRule{
			rule:   r,
			rooms:  rooms,
			filter: cf,
		}

		newRules[r.ID] = cr
		if len(rooms) == 0 {
			newWildcards[r.ID] = cr
		} else {
			for _, rid := range rooms {
				bucket, ok := newByRoom[rid]
				if !ok {
					bucket = make(map[string]*compiledRule)
					newByRoom[rid] = bucket
				}
				bucket[r.ID] = cr
			}
		}
	}

	m.rulesMu.Lock()
	m.rules = newRules
	m.rulesByRoom = newByRoom
	m.wildcardRules = newWildcards
	m.rulesMu.Unlock()

	return nil
}

// List returns all currently active webhook rules.
func (m *WebhookManager) List() []WebhookRule {
	m.rulesMu.RLock()
	defer m.rulesMu.RUnlock()

	out := make([]WebhookRule, 0, len(m.rules))
	for _, cr := range m.rules {
		out = append(out, cr.rule)
	}
	return out
}

// Get returns the rule for id, or nil if not found.
func (m *WebhookManager) Get(id string) *WebhookRule {
	m.rulesMu.RLock()
	defer m.rulesMu.RUnlock()

	cr, ok := m.rules[id]
	if !ok {
		return nil
	}
	r := cr.rule
	return &r
}

// AddOrUpdate validates rule, updates webhooks.yaml atomically, and reloads in-memory rules.
func (m *WebhookManager) AddOrUpdate(rule WebhookRule) error {
	if err := rule.Validate(); err != nil {
		return err
	}

	current := m.List()
	updated := make([]WebhookRule, 0, len(current)+1)
	replaced := false
	for _, r := range current {
		if r.ID == rule.ID {
			updated = append(updated, rule)
			replaced = true
		} else {
			updated = append(updated, r)
		}
	}
	if !replaced {
		updated = append(updated, rule)
	}

	if err := m.fileMgr.Save(updated); err != nil {
		return err
	}

	return m.Reload()
}

// Delete removes the rule with id, updates webhooks.yaml atomically, and reloads in-memory rules.
func (m *WebhookManager) Delete(id string) (bool, error) {
	current := m.List()
	updated := make([]WebhookRule, 0, len(current))
	found := false
	for _, r := range current {
		if r.ID == id {
			found = true
		} else {
			updated = append(updated, r)
		}
	}
	if !found {
		return false, nil
	}

	if err := m.fileMgr.Save(updated); err != nil {
		return false, err
	}

	return true, m.Reload()
}

// handleEvent matches incoming bus events and launches fast-path dispatch.
func (m *WebhookManager) handleEvent(env bus.EventEnvelope) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("webhook: panic in event handler", "panic", r)
		}
	}()

	m.rulesMu.RLock()
	var matched []*compiledRule

	// 1. Check room bucket if room ID is present
	if env.RoomID != "" {
		if bucket, ok := m.rulesByRoom[env.RoomID]; ok {
			for _, cr := range bucket {
				if cr.filter.MatchesEnvelope(env) {
					matched = append(matched, cr)
				}
			}
		}
	}

	// 2. Check wildcard bucket
	for _, cr := range m.wildcardRules {
		if cr.filter.MatchesEnvelope(env) {
			matched = append(matched, cr)
		}
	}
	m.rulesMu.RUnlock()

	for _, cr := range matched {
		m.stateMu.Lock()
		if m.stopped {
			m.stateMu.Unlock()
			return
		}
		m.wg.Add(1)
		m.stateMu.Unlock()

		rule := cr.rule
		go func() {
			defer m.wg.Done()
			if m.sem != nil {
				select {
				case m.sem <- struct{}{}:
					defer func() { <-m.sem }()
				case <-m.ctx.Done():
					return
				}
			}
			m.dispatchFastPath(env, rule)
		}()
	}
}

// dispatchFastPath executes the zero-DB happy path HTTP call.
func (m *WebhookManager) dispatchFastPath(env bus.EventEnvelope, rule WebhookRule) {
	payload := api.EnvelopeToPayload(env)
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		slog.Error("webhook: marshal payload failed", "webhook_id", rule.ID, "error", err)
		return
	}

	reqCtx := m.ctx
	if reqCtx == nil {
		reqCtx = context.Background()
	}

	deliveryID := uuid.New().String()
	res := m.dispatcher.Dispatch(reqCtx, DispatchParams{
		DeliveryID: deliveryID,
		WebhookID:  rule.ID,
		URL:        rule.URL,
		Secret:     rule.Secret,
		Headers:    rule.Headers,
		EventType:  string(env.Type),
		AccountID:  env.AccountID,
		Payload:    payloadBytes,
		Timeout:    rule.TimeoutDuration(m.defaultTimeout),
	})

	if res.Success {
		// Zero-DB Happy Path: 0 database writes occurred!
		return
	}

	// Delivery failed: commit to SQLite for durable retries
	slog.Warn("webhook fast-path dispatch failed, committing to durable retry queue",
		"webhook_id", rule.ID,
		"delivery_id", deliveryID,
		"status_code", res.StatusCode,
		"error", res.Err,
	)

	if m.store == nil {
		return
	}

	headersJSON, _ := json.Marshal(rule.Headers)
	now := time.Now().UnixMilli()
	initialBackoff := rule.InitialIntervalDuration()

	errMsg := ""
	if res.Err != nil {
		errMsg = res.Err.Error()
	}

	entry := persistence.WebhookQueueEntry{
		ID:             deliveryID,
		WebhookID:      rule.ID,
		URL:            rule.URL,
		Payload:        string(payloadBytes),
		HeadersJSON:    string(headersJSON),
		Secret:         rule.Secret,
		State:          "failed",
		RetryCount:     1,
		MaxRetries:     rule.EffectiveMaxRetries(),
		ScheduledAt:    now + initialBackoff.Milliseconds(),
		LastError:      errMsg,
		LastStatusCode: res.StatusCode,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if (res.StatusCode >= 400 && res.StatusCode < 500) || entry.RetryCount >= entry.MaxRetries {
		entry.State = "dead_letter"
	}

	storeCtx, storeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer storeCancel()

	if err := m.store.RecordFailure(storeCtx, entry); err != nil {
		slog.Error("webhook: failed to record durable failure", "webhook_id", rule.ID, "delivery_id", deliveryID, "error", err)
		return
	}

	if m.retryWorker != nil {
		m.retryWorker.SignalWake()
	}
}

// QueueStats returns current queue counts.
func (m *WebhookManager) QueueStats(ctx context.Context) (failed int, sending int, deadLetter int, err error) {
	if m.store == nil {
		return 0, 0, 0, nil
	}
	return m.store.CountByState(ctx)
}

// Redrive moves a dead_letter job back to pending retry.
func (m *WebhookManager) Redrive(ctx context.Context, id string) error {
	if m.store == nil {
		return fmt.Errorf("no persistent store configured")
	}
	if err := m.store.Redrive(ctx, id, time.Now().UnixMilli()); err != nil {
		return err
	}
	if m.retryWorker != nil {
		m.retryWorker.SignalWake()
	}
	return nil
}

// RedriveAll moves all dead_letter jobs back to pending retry.
func (m *WebhookManager) RedriveAll(ctx context.Context) (int64, error) {
	if m.store == nil {
		return 0, fmt.Errorf("no persistent store configured")
	}
	n, err := m.store.RedriveAll(ctx, time.Now().UnixMilli())
	if err != nil {
		return 0, err
	}
	if m.retryWorker != nil {
		m.retryWorker.SignalWake()
	}
	return n, nil
}
