package service

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"privacy-relay/internal/cache"
	"privacy-relay/internal/config"
	appErr "privacy-relay/pkg/errors"
)

type IdempotentService interface {
	AcquireRegisterLock(ctx context.Context, idempotentKey string) (bool, string, error)
	ReleaseRegisterLock(ctx context.Context, idempotentKey, token string) error
	MarkRegisterCompleted(ctx context.Context, idempotentKey string, relayID string) error
	GetRegisterResult(ctx context.Context, idempotentKey string) (string, bool, error)
	AcquireDispatchLock(ctx context.Context, relayID string) (bool, string, error)
	ReleaseDispatchLock(ctx context.Context, relayID, token string) error
	MarkDispatchCompleted(ctx context.Context, relayID, dispatchID string, ttl time.Duration) error
	GetDispatchResult(ctx context.Context, relayID string) (string, bool, error)
	AcquireStateLock(ctx context.Context, relayID string) (bool, string, error)
	ReleaseStateLock(ctx context.Context, relayID, token string) error
}

type idempotentService struct {
	redis *cache.RedisClient
	cfg   *config.RelayConfig
}

func NewIdempotentService(redis *cache.RedisClient, cfg *config.RelayConfig) IdempotentService {
	return &idempotentService{
		redis: redis,
		cfg:   cfg,
	}
}

const (
	registerLockKeyPrefix   = "relay:register:lock:"
	registerResultKeyPrefix = "relay:register:result:"
	dispatchLockKeyPrefix   = "relay:dispatch:lock:"
	dispatchResultKeyPrefix = "relay:dispatch:result:"
	stateLockKeyPrefix      = "relay:state:lock:"
)

func (s *idempotentService) AcquireRegisterLock(ctx context.Context, idempotentKey string) (bool, string, error) {
	key := registerLockKeyPrefix + idempotentKey
	lock := s.redis.NewDistributedLock(key, s.cfg.IdempotentTTL)
	acquired, err := lock.TryLock(ctx)
	if err != nil {
		return false, "", err
	}
	if !acquired {
		return false, "", nil
	}
	return true, lock.GetValue(), nil
}

func (s *idempotentService) ReleaseRegisterLock(ctx context.Context, idempotentKey, token string) error {
	key := registerLockKeyPrefix + idempotentKey
	lock := s.redis.NewDistributedLockWithValue(key, token, s.cfg.IdempotentTTL)
	return lock.Unlock(ctx)
}

func (s *idempotentService) MarkRegisterCompleted(ctx context.Context, idempotentKey string, relayID string) error {
	resultKey := registerResultKeyPrefix + idempotentKey
	client := s.redis.GetClient()
	err := client.Set(ctx, resultKey, relayID, s.cfg.IdempotentTTL).Err()
	if err != nil {
		return appErr.CacheError("failed to mark register completed", err)
	}
	return nil
}

func (s *idempotentService) GetRegisterResult(ctx context.Context, idempotentKey string) (string, bool, error) {
	resultKey := registerResultKeyPrefix + idempotentKey
	client := s.redis.GetClient()
	relayID, err := client.Get(ctx, resultKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", false, nil
		}
		return "", false, appErr.CacheError("failed to get register result", err)
	}
	return relayID, true, nil
}

func (s *idempotentService) AcquireDispatchLock(ctx context.Context, relayID string) (bool, string, error) {
	key := dispatchLockKeyPrefix + relayID
	lock := s.redis.NewDistributedLock(key, s.cfg.StateLockTTL)
	acquired, err := lock.TryLock(ctx)
	if err != nil {
		return false, "", err
	}
	if !acquired {
		return false, "", nil
	}
	return true, lock.GetValue(), nil
}

func (s *idempotentService) ReleaseDispatchLock(ctx context.Context, relayID, token string) error {
	key := dispatchLockKeyPrefix + relayID
	lock := s.redis.NewDistributedLockWithValue(key, token, s.cfg.StateLockTTL)
	return lock.Unlock(ctx)
}

func (s *idempotentService) MarkDispatchCompleted(ctx context.Context, relayID, dispatchID string, ttl time.Duration) error {
	resultKey := dispatchResultKeyPrefix + relayID
	if ttl <= 0 {
		ttl = s.cfg.IdempotentTTL
	}
	client := s.redis.GetClient()
	err := client.Set(ctx, resultKey, dispatchID, ttl).Err()
	if err != nil {
		return appErr.CacheError("failed to mark dispatch completed", err)
	}
	return nil
}

func (s *idempotentService) GetDispatchResult(ctx context.Context, relayID string) (string, bool, error) {
	resultKey := dispatchResultKeyPrefix + relayID
	client := s.redis.GetClient()
	dispatchID, err := client.Get(ctx, resultKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", false, nil
		}
		return "", false, appErr.CacheError("failed to get dispatch result", err)
	}
	return dispatchID, true, nil
}

func (s *idempotentService) AcquireStateLock(ctx context.Context, relayID string) (bool, string, error) {
	key := stateLockKeyPrefix + relayID
	lock := s.redis.NewDistributedLock(key, s.cfg.StateLockTTL)
	acquired, err := lock.TryLock(ctx)
	if err != nil {
		return false, "", err
	}
	if !acquired {
		return false, "", nil
	}
	return true, lock.GetValue(), nil
}

func (s *idempotentService) ReleaseStateLock(ctx context.Context, relayID, token string) error {
	key := stateLockKeyPrefix + relayID
	lock := s.redis.NewDistributedLockWithValue(key, token, s.cfg.StateLockTTL)
	return lock.Unlock(ctx)
}

type ReplayProtectionService interface {
	CheckAndRecord(ctx context.Context, clientID, nonce string, timestamp int64) (bool, error)
}

type replayProtectionService struct {
	redis *cache.RedisClient
	cfg   *config.RelayConfig
}

func NewReplayProtectionService(redis *cache.RedisClient, cfg *config.RelayConfig) ReplayProtectionService {
	return &replayProtectionService{
		redis: redis,
		cfg:   cfg,
	}
}

const replayNonceKeyPrefix = "relay:replay:nonce:"

func (s *replayProtectionService) CheckAndRecord(ctx context.Context, clientID, nonce string, timestamp int64) (bool, error) {
	key := replayNonceKeyPrefix + clientID + ":" + nonce
	client := s.redis.GetClient()
	ttl := int64(s.cfg.ReplayTTL.Seconds())

	script := `
	local result = redis.call('SET', KEYS[1], ARGV[1], 'NX', 'EX', ARGV[2])
	if result then
		return 1
	end
	return 0
	`

	res, err := client.Eval(ctx, script, []string{key}, timestamp, ttl).Int()
	if err != nil {
		return false, appErr.CacheError("failed to check replay nonce", err)
	}
	return res == 1, nil
}
