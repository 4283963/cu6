package cache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr(),
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
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
		return false, appErr.CacheError("failed to acquire lock", err)
	}
	if result == 1 {
		return true, nil
	}
	return false, nil
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
