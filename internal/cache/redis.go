package cache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/redis/go-redis/v9"

	"privacy-relay/internal/config"
	appErr "privacy-relay/pkg/errors"
)

type RedisClient struct {
	client *redis.Client
}

func NewRedisClient(cfg *config.RedisConfig) (*RedisClient, error) {
	poolTimeout := cfg.ReadTimeout + cfg.WriteTimeout
	if poolTimeout < 500*time.Millisecond {
		poolTimeout = 500 * time.Millisecond
	}

	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr(),
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		PoolTimeout:  poolTimeout,
		MaxRetries:   2,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, appErr.CacheError("failed to connect redis", err)
	}

	return &RedisClient{client: client}, nil
}

func (r *RedisClient) GetClient() *redis.Client {
	return r.client
}

func (r *RedisClient) Close() error {
	return r.client.Close()
}

func (r *RedisClient) PoolStats() *redis.PoolStats {
	return r.client.PoolStats()
}

type DistributedLock struct {
	redis    *RedisClient
	key      string
	value    string
	ttl      time.Duration
	released bool
}

func generateLockValue() string {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func (r *RedisClient) NewDistributedLock(key string, ttl time.Duration) *DistributedLock {
	return &DistributedLock{
		redis: r,
		key:   key,
		value: generateLockValue(),
		ttl:   ttl,
	}
}

func (r *RedisClient) NewDistributedLockWithValue(key, value string, ttl time.Duration) *DistributedLock {
	return &DistributedLock{
		redis: r,
		key:   key,
		value: value,
		ttl:   ttl,
	}
}

func (l *DistributedLock) GetValue() string {
	return l.value
}

const lockScript = `
if redis.call('SET', KEYS[1], ARGV[1], 'NX', 'PX', ARGV[2]) then
    return 1
end
return 0
`

func (l *DistributedLock) TryLock(ctx context.Context) (bool, error) {
	ttlMs := l.ttl.Milliseconds()
	result, err := l.redis.client.Eval(ctx, lockScript, []string{l.key}, l.value, ttlMs).Int()
	if err != nil {
		if isRedisTimeout(err) {
			return false, appErr.CacheError("redis timeout while acquiring lock", err)
		}
		return false, appErr.CacheError("failed to acquire lock", err)
	}
	return result == 1, nil
}

func (l *DistributedLock) Lock(ctx context.Context, retryInterval time.Duration, maxRetries int) (bool, error) {
	for i := 0; i < maxRetries; i++ {
		acquired, err := l.TryLock(ctx)
		if err != nil {
			return false, err
		}
		if acquired {
			return true, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(retryInterval):
		}
	}
	return false, nil
}

const unlockScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
    return redis.call('DEL', KEYS[1])
end
return 0
`

func (l *DistributedLock) Unlock(ctx context.Context) error {
	if l.released {
		return nil
	}
	_, err := l.redis.client.Eval(ctx, unlockScript, []string{l.key}, l.value).Result()
	if err != nil {
		if isRedisTimeout(err) {
			l.released = true
			return nil
		}
		return appErr.CacheError("failed to release lock", err)
	}
	l.released = true
	return nil
}

func (l *DistributedLock) Renew(ctx context.Context, extraTTL time.Duration) (bool, error) {
	script := `
	if redis.call('GET', KEYS[1]) == ARGV[1] then
		return redis.call('PEXPIRE', KEYS[1], ARGV[2])
	end
	return 0
	`
	ttlMs := extraTTL.Milliseconds()
	result, err := l.redis.client.Eval(ctx, script, []string{l.key}, l.value, ttlMs).Int()
	if err != nil {
		return false, appErr.CacheError("failed to renew lock", err)
	}
	return result == 1, nil
}

const dispatchPrecheckScript = `
local nextAllowedKey = KEYS[1]
local dispatchResultKey = KEYS[2]
local nowMs = tonumber(ARGV[1])

local dispatchResult = redis.call('GET', dispatchResultKey)
if dispatchResult then
	return {1, dispatchResult}
end

local nextAllowed = redis.call('GET', nextAllowedKey)
if nextAllowed and tonumber(nextAllowed) > nowMs then
	return {2, nextAllowed}
end

return {0, '0'}
`

type DispatchPrecheckResult struct {
	AlreadyDispatched bool
	DispatchID        string
	Throttled         bool
	NextAllowedAtMs   int64
}

func (r *RedisClient) CheckDispatchAllowed(ctx context.Context, relayID string) (*DispatchPrecheckResult, error) {
	nextAllowedKey := "relay:dispatch:next_allowed_at:" + relayID
	dispatchResultKey := dispatchResultKeyPrefix + relayID
	nowMs := time.Now().UnixMilli()

	res, err := r.client.Eval(ctx, dispatchPrecheckScript,
		[]string{nextAllowedKey, dispatchResultKey},
		nowMs,
	).Slice()
	if err != nil {
		if isRedisTimeout(err) {
			return nil, appErr.CacheError("redis timeout on dispatch precheck", err)
		}
		return nil, appErr.CacheError("failed to precheck dispatch", err)
	}

	if len(res) < 2 {
		return &DispatchPrecheckResult{}, nil
	}

	code, _ := res[0].(int64)
	switch code {
	case 1:
		dispatchID, _ := res[1].(string)
		return &DispatchPrecheckResult{
			AlreadyDispatched: true,
			DispatchID:        dispatchID,
		}, nil
	case 2:
		nextMs, _ := res[1].(string)
		var ms int64
		fmt.Sscanf(nextMs, "%d", &ms)
		return &DispatchPrecheckResult{
			Throttled:       true,
			NextAllowedAtMs: ms,
		}, nil
	default:
		return &DispatchPrecheckResult{}, nil
	}
}

func (r *RedisClient) SetNextDispatchAllowedAt(ctx context.Context, relayID string, nextAt time.Time, ttl time.Duration) error {
	key := "relay:dispatch:next_allowed_at:" + relayID
	ttlSec := int64(ttl.Seconds())
	if ttlSec < 1 {
		ttlSec = 1
	}
	err := r.client.Set(ctx, key, nextAt.UnixMilli(), time.Duration(ttlSec)*time.Second).Err()
	if err != nil {
		if isRedisTimeout(err) {
			return nil
		}
		return appErr.CacheError("failed to set next dispatch allowed at", err)
	}
	return nil
}

func isRedisTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	return false
}

const dispatchResultKeyPrefix = "relay:dispatch:result:"
