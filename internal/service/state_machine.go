package service

import (
	"math"
	"time"

	"privacy-relay/internal/config"
	"privacy-relay/internal/model"
	"privacy-relay/pkg/utils"
)

type BackoffStrategy interface {
	NextDelay(retryCount int) time.Duration
	NextRetryAt(retryCount int) time.Time
	MaxRetryCount() int
	ShouldRetry(retryCount int) bool
}

type exponentialBackoff struct {
	baseDelay    time.Duration
	maxDelay     time.Duration
	maxRetry     int
	multiplier   float64
	jitterFactor float64
}

func NewExponentialBackoff(cfg *config.RelayConfig) BackoffStrategy {
	return &exponentialBackoff{
		baseDelay:    cfg.BaseRetryBackoff,
		maxDelay:     cfg.MaxRetryBackoff,
		maxRetry:     cfg.MaxRetryCount,
		multiplier:   2.0,
		jitterFactor: 0.1,
	}
}

func NewCustomExponentialBackoff(baseDelay, maxDelay time.Duration, maxRetry int) BackoffStrategy {
	return &exponentialBackoff{
		baseDelay:    baseDelay,
		maxDelay:     maxDelay,
		maxRetry:     maxRetry,
		multiplier:   2.0,
		jitterFactor: 0.1,
	}
}

func (b *exponentialBackoff) NextDelay(retryCount int) time.Duration {
	if retryCount < 0 {
		retryCount = 0
	}

	exponent := math.Min(float64(retryCount), 16)
	delay := float64(b.baseDelay) * math.Pow(b.multiplier, exponent)

	if delay > float64(b.maxDelay) {
		delay = float64(b.maxDelay)
	}

	if b.jitterFactor > 0 {
		jitterRange := delay * b.jitterFactor
		delay = delay - jitterRange + (float64(time.Now().UnixNano()%1000)/1000.0)*jitterRange*2
	}

	return time.Duration(delay)
}

func (b *exponentialBackoff) NextRetryAt(retryCount int) time.Time {
	return time.Now().Add(b.NextDelay(retryCount))
}

func (b *exponentialBackoff) MaxRetryCount() int {
	return b.maxRetry
}

func (b *exponentialBackoff) ShouldRetry(retryCount int) bool {
	return retryCount < b.maxRetry
}

type StateMachine interface {
	CanTransition(from, to interface{}) bool
	ValidTransitions(from interface{}) []interface{}
	IsTerminal(state interface{}) bool
}

type RelayStateMachine struct {
	transitions    map[model.RelayStatus][]model.RelayStatus
	terminalStates map[model.RelayStatus]bool
}

func NewRelayStateMachine() StateMachine {
	transitions := map[model.RelayStatus][]model.RelayStatus{
		model.RelayStatusRegistered: {
			model.RelayStatusDistributed,
			model.RelayStatusExpired,
		},
		model.RelayStatusDistributed: {
			model.RelayStatusDecrypting,
			model.RelayStatusRetrying,
			model.RelayStatusFailed,
		},
		model.RelayStatusDecrypting: {
			model.RelayStatusSuccess,
			model.RelayStatusRetrying,
			model.RelayStatusFailed,
		},
		model.RelayStatusRetrying: {
			model.RelayStatusDistributed,
			model.RelayStatusFailed,
		},
	}

	terminalStates := map[model.RelayStatus]bool{
		model.RelayStatusSuccess: true,
		model.RelayStatusFailed:  true,
		model.RelayStatusExpired: true,
	}

	return &RelayStateMachine{
		transitions:    transitions,
		terminalStates: terminalStates,
	}
}

func (sm *RelayStateMachine) CanTransition(from, to interface{}) bool {
	fromStatus, ok1 := from.(model.RelayStatus)
	toStatus, ok2 := to.(model.RelayStatus)
	if !ok1 || !ok2 {
		return false
	}

	if fromStatus == toStatus {
		return true
	}

	allowed, exists := sm.transitions[fromStatus]
	if !exists {
		return false
	}

	return utils.ContainsString(
		func() []string {
			result := make([]string, len(allowed))
			for i, s := range allowed {
				result[i] = string(s)
			}
			return result
		}(),
		string(toStatus),
	)
}

func (sm *RelayStateMachine) ValidTransitions(from interface{}) []interface{} {
	fromStatus, ok := from.(model.RelayStatus)
	if !ok {
		return nil
	}

	allowed, exists := sm.transitions[fromStatus]
	if !exists {
		return nil
	}

	result := make([]interface{}, len(allowed))
	for i, s := range allowed {
		result[i] = s
	}
	return result
}

func (sm *RelayStateMachine) IsTerminal(state interface{}) bool {
	status, ok := state.(model.RelayStatus)
	if !ok {
		return false
	}
	return sm.terminalStates[status]
}
