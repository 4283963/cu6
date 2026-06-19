package service

import (
	"context"
	"fmt"
	"time"

	"privacy-relay/internal/cache"
	"privacy-relay/internal/config"
	"privacy-relay/internal/model"
	"privacy-relay/internal/repository"
	appErr "privacy-relay/pkg/errors"
	"privacy-relay/pkg/utils"
)

type RelayService interface {
	RegisterRelay(ctx context.Context, req *model.RegisterRelayRequest) (*model.RegisterRelayResponse, error)
	GetRelay(ctx context.Context, relayID string) (*model.GetRelayResponse, error)
	DispatchDecrypt(ctx context.Context, req *model.DispatchDecryptRequest) (*DispatchResult, error)
	UpdateDecryptStatus(ctx context.Context, req *model.UpdateDecryptStatusRequest) (*model.UpdateDecryptStatusResponse, error)
	ListRelays(ctx context.Context, req *model.ListRelaysRequest) (*model.ListRelaysResponse, error)
}

type DispatchResult struct {
	Response     *model.DispatchDecryptResponse
	AlreadyDone  bool
	Throttled    bool
	RetryAfterMs int64
}

type relayService struct {
	cfg              *config.Config
	relayRepo        repository.RelayRepository
	stateRepo        repository.StateTransitionRepository
	replayRepo       repository.ReplayRecordRepository
	idempotentSvc    IdempotentService
	backoff          BackoffStrategy
	stateMachine     StateMachine
	defaultMaxRetry  int
	defaultExpireTTL time.Duration
}

func NewRelayService(
	cfg *config.Config,
	relayRepo repository.RelayRepository,
	stateRepo repository.StateTransitionRepository,
	replayRepo repository.ReplayRecordRepository,
	idempotentSvc IdempotentService,
) RelayService {
	return &relayService{
		cfg:              cfg,
		relayRepo:        relayRepo,
		stateRepo:        stateRepo,
		replayRepo:       replayRepo,
		idempotentSvc:    idempotentSvc,
		backoff:          NewExponentialBackoff(&cfg.Relay),
		stateMachine:     NewRelayStateMachine(),
		defaultMaxRetry:  cfg.Relay.MaxRetryCount,
		defaultExpireTTL: cfg.Relay.IdempotentTTL,
	}
}

func (s *relayService) RegisterRelay(ctx context.Context, req *model.RegisterRelayRequest) (*model.RegisterRelayResponse, error) {
	cachedRelayID, exists, err := s.idempotentSvc.GetRegisterResult(ctx, req.IdempotentKey)
	if err != nil {
		return nil, err
	}
	if exists {
		existing, err := s.relayRepo.GetByRelayID(ctx, cachedRelayID)
		if err == nil && existing != nil {
			return &model.RegisterRelayResponse{
				RelayID: existing.RelayID,
				Status:  existing.Status,
			}, appErr.Conflict("relay already registered with this idempotent key")
		}
	}

	acquired, lockToken, err := s.idempotentSvc.AcquireRegisterLock(ctx, req.IdempotentKey)
	if err != nil {
		return nil, err
	}
	if !acquired {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
		cachedRelayID, exists, err = s.idempotentSvc.GetRegisterResult(ctx, req.IdempotentKey)
		if err == nil && exists {
			existing, err := s.relayRepo.GetByRelayID(ctx, cachedRelayID)
			if err == nil && existing != nil {
				return &model.RegisterRelayResponse{
					RelayID: existing.RelayID,
					Status:  existing.Status,
				}, appErr.Conflict("relay already registered with this idempotent key")
			}
		}
		return nil, appErr.IdempotentLock("another registration is in progress, please retry later")
	}
	defer func() {
		_ = s.idempotentSvc.ReleaseRegisterLock(ctx, req.IdempotentKey, lockToken)
	}()

	existing, err := s.relayRepo.GetByIdempotentKey(ctx, req.IdempotentKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		_ = s.idempotentSvc.MarkRegisterCompleted(ctx, req.IdempotentKey, existing.RelayID)
		return &model.RegisterRelayResponse{
			RelayID: existing.RelayID,
			Status:  existing.Status,
		}, appErr.Conflict("relay already registered with this idempotent key")
	}

	relayID := utils.GenerateRelayID()
	maxRetry := s.defaultMaxRetry
	if req.MaxRetryCount > 0 {
		maxRetry = req.MaxRetryCount
	}

	expireTTL := s.defaultExpireTTL
	if req.TTLSeconds > 0 {
		expireTTL = time.Duration(req.TTLSeconds) * time.Second
	}

	now := time.Now()
	record := &model.RelayRecord{
		RelayID:        relayID,
		IdempotentKey:  req.IdempotentKey,
		ClientID:       req.ClientID,
		Ciphertext:     req.Ciphertext,
		Status:         model.RelayStatusRegistered,
		TargetEndpoint: req.TargetEndpoint,
		RetryCount:     0,
		MaxRetryCount:  maxRetry,
		ExpireAt:       now.Add(expireTTL),
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.relayRepo.Create(ctx, record); err != nil {
		return nil, err
	}

	transition := &model.StateTransition{
		RelayID:       relayID,
		FromStatus:    "",
		ToStatus:      model.RelayStatusRegistered,
		TriggerReason: "register_relay",
		Operator:      req.ClientID,
		CreatedAt:     now,
	}
	if err := s.stateRepo.Create(ctx, transition); err != nil {
		return nil, err
	}

	if err := s.idempotentSvc.MarkRegisterCompleted(ctx, req.IdempotentKey, relayID); err != nil {
		return nil, err
	}

	return &model.RegisterRelayResponse{
		RelayID: relayID,
		Status:  model.RelayStatusRegistered,
	}, nil
}

func (s *relayService) GetRelay(ctx context.Context, relayID string) (*model.GetRelayResponse, error) {
	record, err := s.relayRepo.GetByRelayID(ctx, relayID)
	if err != nil {
		return nil, err
	}
	return s.toGetRelayResponse(record), nil
}

func (s *relayService) DispatchDecrypt(ctx context.Context, req *model.DispatchDecryptRequest) (*DispatchResult, error) {
	precheck, err := s.idempotentSvc.PrecheckDispatch(ctx, req.RelayID)
	if err != nil {
		return nil, err
	}

	if precheck.AlreadyDispatched {
		record, dbErr := s.relayRepo.GetByRelayID(ctx, req.RelayID)
		if dbErr == nil && record != nil &&
			(record.Status == model.RelayStatusDistributed || record.Status == model.RelayStatusDecrypting) {
			return &DispatchResult{
				AlreadyDone: true,
				Response: &model.DispatchDecryptResponse{
					RelayID:    req.RelayID,
					Status:     record.Status,
					DispatchID: precheck.DispatchID,
				},
			}, appErr.Conflict("decrypt dispatch already processed")
		}
	}

	if precheck.Throttled {
		retryAfterMs := precheck.NextAllowedAtMs - time.Now().UnixMilli()
		if retryAfterMs < 0 {
			retryAfterMs = 0
		}
		return &DispatchResult{
			Throttled:    true,
			RetryAfterMs: retryAfterMs,
		}, appErr.IdempotentLock(fmt.Sprintf("dispatch throttled, retry after %dms", retryAfterMs))
	}

	acquired, stateToken, err := s.idempotentSvc.AcquireStateLock(ctx, req.RelayID)
	if err != nil {
		return nil, err
	}
	if !acquired {
		return &DispatchResult{
			Throttled:    true,
			RetryAfterMs: 500,
		}, appErr.IdempotentLock("another dispatch is processing, please retry later")
	}
	defer func() {
		_ = s.idempotentSvc.ReleaseStateLock(ctx, req.RelayID, stateToken)
	}()

	record, err := s.relayRepo.GetByRelayID(ctx, req.RelayID)
	if err != nil {
		return nil, err
	}

	if record.ExpireAt.Before(time.Now()) && record.Status == model.RelayStatusRegistered {
		_ = s.transitionState(ctx, record, model.RelayStatusExpired, "dispatch_expired", "system", "")
		return nil, appErr.StateTransition("relay has expired")
	}

	if s.stateMachine.IsTerminal(record.Status) {
		return nil, appErr.Conflict("relay already in terminal state: " + string(record.Status))
	}

	if record.Status == model.RelayStatusRetrying && record.LastRetryAt != nil {
		delay := s.backoff.NextDelay(record.RetryCount)
		earliestAllowed := record.LastRetryAt.Add(delay)
		if time.Now().Before(earliestAllowed) {
			_ = s.idempotentSvc.SetNextDispatchAllowed(ctx, req.RelayID, earliestAllowed)
			retryMs := time.Until(earliestAllowed).Milliseconds()
			if retryMs < 0 {
				retryMs = 0
			}
			return &DispatchResult{
				Throttled:    true,
				RetryAfterMs: retryMs,
			}, appErr.IdempotentLock(fmt.Sprintf("retry backoff active, retry after %dms", retryMs))
		}
	}

	allowedFromStates := []model.RelayStatus{
		model.RelayStatusRegistered,
		model.RelayStatusRetrying,
	}
	fromAllowed := false
	for _, st := range allowedFromStates {
		if record.Status == st {
			fromAllowed = true
			break
		}
	}
	if !fromAllowed {
		if record.Status == model.RelayStatusDistributed || record.Status == model.RelayStatusDecrypting {
			return &DispatchResult{
				AlreadyDone: true,
				Response: &model.DispatchDecryptResponse{
					RelayID: req.RelayID,
					Status:  record.Status,
				},
			}, appErr.Conflict("relay already dispatched, status: " + string(record.Status))
		}
		return nil, appErr.StateTransition("invalid state for dispatch: " + string(record.Status))
	}

	dispatchID := utils.GenerateRequestID()
	fromStatus := record.Status
	now := time.Now()
	extraUpdates := map[string]interface{}{
		"distributed_at": now,
	}
	if fromStatus == model.RelayStatusRetrying {
		extraUpdates["last_retry_at"] = now
	}

	if err := s.relayRepo.UpdateStatus(ctx, req.RelayID, fromStatus, model.RelayStatusDistributed, extraUpdates); err != nil {
		return nil, err
	}

	if err := s.stateRepo.Create(ctx, &model.StateTransition{
		RelayID:       req.RelayID,
		FromStatus:    fromStatus,
		ToStatus:      model.RelayStatusDistributed,
		TriggerReason: "dispatch_decrypt",
		Operator:      "dispatch_system",
		Remark:        "dispatch_id=" + dispatchID,
		CreatedAt:     now,
	}); err != nil {
		return nil, err
	}

	if err := s.idempotentSvc.MarkDispatchCompleted(ctx, req.RelayID, dispatchID, s.cfg.Relay.StateLockTTL*3); err != nil {
		return nil, err
	}

	return &DispatchResult{
		Response: &model.DispatchDecryptResponse{
			RelayID:    req.RelayID,
			Status:     model.RelayStatusDistributed,
			DispatchID: dispatchID,
		},
	}, nil
}

func (s *relayService) UpdateDecryptStatus(ctx context.Context, req *model.UpdateDecryptStatusRequest) (*model.UpdateDecryptStatusResponse, error) {
	acquired, stateToken, err := s.idempotentSvc.AcquireStateLock(ctx, req.RelayID)
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, appErr.IdempotentLock("state update locked, please retry later")
	}
	defer func() {
		_ = s.idempotentSvc.ReleaseStateLock(ctx, req.RelayID, stateToken)
	}()

	record, err := s.relayRepo.GetByRelayID(ctx, req.RelayID)
	if err != nil {
		return nil, err
	}

	if s.stateMachine.IsTerminal(record.Status) {
		return &model.UpdateDecryptStatusResponse{
			RelayID:    req.RelayID,
			Status:     record.Status,
			RetryCount: record.RetryCount,
		}, appErr.Conflict("relay already in terminal state: " + string(record.Status))
	}

	if req.Success {
		now := time.Now()
		if err := s.relayRepo.MarkSuccess(ctx, req.RelayID, req.Plaintext, now); err != nil {
			return nil, err
		}
		_ = s.stateRepo.Create(ctx, &model.StateTransition{
			RelayID:       req.RelayID,
			FromStatus:    record.Status,
			ToStatus:      model.RelayStatusSuccess,
			TriggerReason: "decrypt_success",
			Operator:      "callback_system",
			Remark:        "dispatch_id=" + req.DispatchID,
			CreatedAt:     now,
		})
		return &model.UpdateDecryptStatusResponse{
			RelayID:    req.RelayID,
			Status:     model.RelayStatusSuccess,
			RetryCount: record.RetryCount,
		}, nil
	}

	nextRetryCount := record.RetryCount + 1
	now := time.Now()
	nextRetryAt := s.backoff.NextRetryAt(nextRetryCount)

	if !s.backoff.ShouldRetry(nextRetryCount) || nextRetryCount >= record.MaxRetryCount {
		fromStatus := record.Status
		extraUpdates := map[string]interface{}{
			"retry_count":   nextRetryCount,
			"last_error":    req.ErrorMsg,
			"last_retry_at": now,
			"completed_at":  now,
		}
		if err := s.relayRepo.UpdateStatus(ctx, req.RelayID, fromStatus, model.RelayStatusFailed, extraUpdates); err != nil {
			return nil, err
		}
		_ = s.stateRepo.Create(ctx, &model.StateTransition{
			RelayID:       req.RelayID,
			FromStatus:    fromStatus,
			ToStatus:      model.RelayStatusFailed,
			TriggerReason: "max_retry_exceeded",
			Operator:      "retry_system",
			Remark:        "error=" + truncateString(req.ErrorMsg, 400),
			CreatedAt:     now,
		})
		return &model.UpdateDecryptStatusResponse{
			RelayID:    req.RelayID,
			Status:     model.RelayStatusFailed,
			RetryCount: nextRetryCount,
		}, appErr.MaxRetryExceeded("max retry count exceeded: " + req.ErrorMsg)
	}

	fromStatus := record.Status
	extraUpdates := map[string]interface{}{
		"retry_count":   nextRetryCount,
		"last_error":    req.ErrorMsg,
		"last_retry_at": now,
	}
	if err := s.relayRepo.UpdateStatus(ctx, req.RelayID, fromStatus, model.RelayStatusRetrying, extraUpdates); err != nil {
		return nil, err
	}

	_ = s.stateRepo.Create(ctx, &model.StateTransition{
		RelayID:       req.RelayID,
		FromStatus:    fromStatus,
		ToStatus:      model.RelayStatusRetrying,
		TriggerReason: "decrypt_failed_scheduled_retry",
		Operator:      "retry_system",
		Remark:        fmt.Sprintf("retry=%d/%d error=%s", nextRetryCount, record.MaxRetryCount, truncateString(req.ErrorMsg, 350)),
		CreatedAt:     now,
	})

	_ = s.idempotentSvc.SetNextDispatchAllowed(ctx, req.RelayID, nextRetryAt)

	return &model.UpdateDecryptStatusResponse{
		RelayID:     req.RelayID,
		Status:      model.RelayStatusRetrying,
		RetryCount:  nextRetryCount,
		NextRetryAt: &nextRetryAt,
	}, nil
}

func (s *relayService) ListRelays(ctx context.Context, req *model.ListRelaysRequest) (*model.ListRelaysResponse, error) {
	pageNum := req.PageNum
	if pageNum <= 0 {
		pageNum = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	records, total, err := s.relayRepo.List(ctx, req.ClientID, req.Status, pageNum, pageSize)
	if err != nil {
		return nil, err
	}

	items := make([]*model.GetRelayResponse, len(records))
	for i, r := range records {
		items[i] = s.toGetRelayResponse(r)
	}

	return &model.ListRelaysResponse{
		Total: total,
		Items: items,
	}, nil
}

func (s *relayService) transitionState(ctx context.Context, record *model.RelayRecord, toStatus model.RelayStatus, reason, operator, remark string) error {
	if !s.stateMachine.CanTransition(record.Status, toStatus) {
		return appErr.StateTransition("cannot transition from " + string(record.Status) + " to " + string(toStatus))
	}

	fromStatus := record.Status
	if err := s.relayRepo.UpdateStatus(ctx, record.RelayID, fromStatus, toStatus, nil); err != nil {
		return err
	}

	_ = s.stateRepo.Create(ctx, &model.StateTransition{
		RelayID:       record.RelayID,
		FromStatus:    fromStatus,
		ToStatus:      toStatus,
		TriggerReason: reason,
		Operator:      operator,
		Remark:        remark,
	})

	record.Status = toStatus
	return nil
}

func (s *relayService) toGetRelayResponse(r *model.RelayRecord) *model.GetRelayResponse {
	return &model.GetRelayResponse{
		RelayID:       r.RelayID,
		IdempotentKey: r.IdempotentKey,
		ClientID:      r.ClientID,
		Status:        r.Status,
		RetryCount:    r.RetryCount,
		MaxRetryCount: r.MaxRetryCount,
		LastError:     r.LastError,
		Plaintext:     r.Plaintext,
		DistributedAt: r.DistributedAt,
		LastRetryAt:   r.LastRetryAt,
		CompletedAt:   r.CompletedAt,
		ExpireAt:      r.ExpireAt,
		CreatedAt:     r.CreatedAt,
	}
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

var _ = cache.DispatchPrecheckResult{}
