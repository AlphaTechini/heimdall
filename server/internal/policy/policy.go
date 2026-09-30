// Package policy holds the user's protection policy (docs/api.md "Policy") and the pure
// decision "what should happen at this severity" (details.md §8.4).
package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/AlphaTechini/heimdall/server/internal/signals"
	"github.com/AlphaTechini/heimdall/server/internal/store"
)

// Policy is the JSON shape stored and served by the API.
type Policy struct {
	OnCritical   string  `json:"onCritical"` // auto_exit | ask_first
	OnWarning    string  `json:"onWarning"`  // notify | exit_half | exit_full
	SendTo       string  `json:"sendTo"`     // wallet_usdc
	PriorityExit bool    `json:"priorityExit"`
	TipCapUSD    float64 `json:"tipCapUsd"`
}

// Default returns the documented defaults.
func Default(tipCapUSD float64) Policy {
	return Policy{OnCritical: "auto_exit", OnWarning: "notify", SendTo: "wallet_usdc", PriorityExit: true, TipCapUSD: tipCapUSD}
}

// Validate returns a plain-English error for an invalid policy.
func (p Policy) Validate() error {
	switch p.OnCritical {
	case "auto_exit", "ask_first":
	default:
		return errors.New("When risk is Critical, choose \"auto_exit\" or \"ask_first\".")
	}
	switch p.OnWarning {
	case "notify", "exit_half", "exit_full":
	default:
		return errors.New("When risk is Warning, choose \"notify\", \"exit_half\" or \"exit_full\".")
	}
	if p.SendTo != "wallet_usdc" {
		return errors.New("Money can only be sent to your own wallet as USDC for now.")
	}
	if !(p.TipCapUSD > 0 && p.TipCapUSD <= 50) {
		return errors.New("The priority tip cap must be above $0 and at most $50.")
	}
	return nil
}

// Action is what the executor should do for a guard position.
type Action int

const (
	None Action = iota
	Notify
	AskFirst
	ExitHalf
	ExitFull
)

// Decide maps a severity to an action under a policy.
func Decide(p Policy, severity string) Action {
	switch severity {
	case signals.SevCritical:
		if p.OnCritical == "ask_first" {
			return AskFirst
		}
		return ExitFull
	case signals.SevWarning:
		switch p.OnWarning {
		case "exit_half":
			return ExitHalf
		case "exit_full":
			return ExitFull
		}
		return Notify
	}
	return None
}

// Service reads and writes policies with an in-memory cache, so the decision path does not
// wait on the database.
type Service struct {
	st         *store.Store
	defaultTip float64

	mu   sync.RWMutex
	pols map[string]Policy // guard|target
	defs map[string]Policy // owner
}

// NewService creates the policy service.
func NewService(st *store.Store, defaultTipCapUSD float64) *Service {
	return &Service{st: st, defaultTip: defaultTipCapUSD, pols: map[string]Policy{}, defs: map[string]Policy{}}
}

func key(guard, target string) string { return strings.ToLower(guard) + "|" + target }

// Global returns the built-in default.
func (s *Service) Global() Policy { return Default(s.defaultTip) }

// OwnerDefault returns the owner's default policy (or the built-in default).
func (s *Service) OwnerDefault(ctx context.Context, owner string) (Policy, error) {
	owner = strings.ToLower(owner)
	s.mu.RLock()
	p, ok := s.defs[owner]
	s.mu.RUnlock()
	if ok {
		return p, nil
	}
	u, err := s.st.GetUser(ctx, owner)
	if err != nil {
		return Policy{}, err
	}
	p = s.Global()
	if len(u.DefaultPolicy) > 0 {
		if err := json.Unmarshal(u.DefaultPolicy, &p); err != nil {
			return Policy{}, fmt.Errorf("stored default policy is unreadable: %w", err)
		}
	}
	s.mu.Lock()
	s.defs[owner] = p
	s.mu.Unlock()
	return p, nil
}

// SetOwnerDefault saves the owner's default policy.
func (s *Service) SetOwnerDefault(ctx context.Context, owner string, p Policy) error {
	raw, _ := json.Marshal(p)
	if err := s.st.SetDefaultPolicy(ctx, owner, raw); err != nil {
		return err
	}
	s.mu.Lock()
	s.defs[strings.ToLower(owner)] = p
	s.mu.Unlock()
	return nil
}

// Stored returns the policy saved for a guard position, or nil.
func (s *Service) Stored(ctx context.Context, guard, target string) (*Policy, error) {
	k := key(guard, target)
	s.mu.RLock()
	p, ok := s.pols[k]
	s.mu.RUnlock()
	if ok {
		return &p, nil
	}
	raw, err := s.st.GetPolicy(ctx, guard, target)
	if err != nil || raw == nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("stored policy is unreadable: %w", err)
	}
	s.mu.Lock()
	s.pols[k] = p
	s.mu.Unlock()
	return &p, nil
}

// For returns the effective policy: the stored one, else the owner's default.
func (s *Service) For(ctx context.Context, guard, owner, target string) (Policy, error) {
	if p, err := s.Stored(ctx, guard, target); err != nil || p != nil {
		if err != nil {
			return Policy{}, err
		}
		return *p, nil
	}
	return s.OwnerDefault(ctx, owner)
}

// Put saves a policy for a guard position.
func (s *Service) Put(ctx context.Context, guard, target string, p Policy) error {
	raw, _ := json.Marshal(p)
	if err := s.st.PutPolicy(ctx, guard, target, raw); err != nil {
		return err
	}
	s.mu.Lock()
	s.pols[key(guard, target)] = p
	s.mu.Unlock()
	return nil
}

// Reset drops the cache (after a simulator reset removed rows).
func (s *Service) Reset() {
	s.mu.Lock()
	s.pols = map[string]Policy{}
	s.defs = map[string]Policy{}
	s.mu.Unlock()
}
