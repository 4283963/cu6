package service

import (
	"context"
	"strconv"
	"time"

	"privacy-relay/internal/cache"
	"privacy-relay/internal/config"
	"privacy-relay/internal/model"
	appErr "privacy-relay/pkg/errors"
)

type MetricsCollector interface {
	IncRequest(ctx context.Context, path string)
	IncSuccess(ctx context.Context, path string)
	IncError(ctx context.Context, path string, code appErr.Code)
	IncReplayIntercepted(ctx context.Context, clientID string)
	IncIdempotentBlocked(ctx context.Context, key string)
	IncInFlight(ctx context.Context)
	DecInFlight(ctx context.Context)
	GetRealtimeMetrics(ctx context.Context) (*model.RealtimeMetrics, error)
	ResetCounters(ctx context.Context) error
}

type metricsCollector struct {
	redis *cache.RedisClient
	cfg   *config.RelayConfig
}

func NewMetricsCollector(redis *cache.RedisClient, cfg *config.RelayConfig) MetricsCollector {
	return &metricsCollector{
		redis: redis,
		cfg:   cfg,
	}
}

const (
	metricsPrefix          = "relay:metrics:"
	metricsTotalKey        = metricsPrefix + "total"
	metricsSuccessKey      = metricsPrefix + "success"
	metricsFailedKey       = metricsPrefix + "failed"
	metricsReplayKey       = metricsPrefix + "replay"
	metricsIdempotentKey   = metricsPrefix + "idempotent_blocked"
	metricsInFlightKey     = metricsPrefix + "in_flight"
	metricsErrorPrefix     = metricsPrefix + "error:"
	metricsPathPrefix      = metricsPrefix + "path:"
	metricsWindowKey       = metricsPrefix + "window"
)

func metricsDateKey(now time.Time) string {
	return now.Format("20060102")
}

func (m *metricsCollector) incr(ctx context.Context, key string, ttl time.Duration) int64 {
	client := m.redis.GetClient()
	val, err := client.Incr(ctx, key).Result()
	if err != nil {
		return 0
	}
	if ttl > 0 {
		_, _ = client.Expire(ctx, key, ttl).Result()
	}
	return val
}

func (m *metricsCollector) decr(ctx context.Context, key string) int64 {
	client := m.redis.GetClient()
	val, err := client.Decr(ctx, key).Result()
	if err != nil {
		return 0
	}
	if val < 0 {
		client.Set(ctx, key, 0, 0)
		return 0
	}
	return val
}

func (m *metricsCollector) getInt(ctx context.Context, key string) int64 {
	client := m.redis.GetClient()
	val, err := client.Get(ctx, key).Int64()
	if err != nil {
		return 0
	}
	return val
}

func (m *metricsCollector) IncRequest(ctx context.Context, path string) {
	ttl := 48 * time.Hour
	m.incr(ctx, metricsTotalKey, ttl)
	m.incr(ctx, metricsPathPrefix+path, ttl)
}

func (m *metricsCollector) IncSuccess(ctx context.Context, path string) {
	ttl := 48 * time.Hour
	m.incr(ctx, metricsSuccessKey, ttl)
}

func (m *metricsCollector) IncError(ctx context.Context, path string, code appErr.Code) {
	ttl := 48 * time.Hour
	m.incr(ctx, metricsFailedKey, ttl)
	m.incr(ctx, metricsErrorPrefix+strconv.Itoa(int(code)), ttl)
}

func (m *metricsCollector) IncReplayIntercepted(ctx context.Context, clientID string) {
	ttl := 48 * time.Hour
	m.incr(ctx, metricsReplayKey, ttl)
	if clientID != "" {
		m.incr(ctx, metricsReplayKey+":"+clientID, ttl)
	}
}

func (m *metricsCollector) IncIdempotentBlocked(ctx context.Context, key string) {
	ttl := 48 * time.Hour
	m.incr(ctx, metricsIdempotentKey, ttl)
}

func (m *metricsCollector) IncInFlight(ctx context.Context) {
	m.incr(ctx, metricsInFlightKey, 0)
}

func (m *metricsCollector) DecInFlight(ctx context.Context) {
	m.decr(ctx, metricsInFlightKey)
}

func (m *metricsCollector) GetRealtimeMetrics(ctx context.Context) (*model.RealtimeMetrics, error) {
	client := m.redis.GetClient()

	errorCodes := []appErr.Code{
		appErr.CodeInvalidParams,
		appErr.CodeUnauthorized,
		appErr.CodeForbidden,
		appErr.CodeNotFound,
		appErr.CodeConflict,
		appErr.CodeReplayAttack,
		appErr.CodeIdempotentLock,
		appErr.CodeInternalError,
		appErr.CodeDatabaseError,
		appErr.CodeCacheError,
		appErr.CodeStateTransition,
		appErr.CodeMaxRetryExceeded,
	}

	keys := make([]string, 0, len(errorCodes)+8)
	keys = append(keys,
		metricsTotalKey,
		metricsSuccessKey,
		metricsFailedKey,
		metricsReplayKey,
		metricsIdempotentKey,
		metricsInFlightKey,
	)

	paths := []string{
		"/api/v1/relays",
		"/api/v1/relays/dispatch",
		"/api/v1/relays/status",
		"/api/v1/health",
	}
	for _, path := range paths {
		keys = append(keys, metricsPathPrefix+path)
	}

	for _, code := range errorCodes {
		keys = append(keys, metricsErrorPrefix+strconv.Itoa(int(code)))
	}

	vals, err := client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, appErr.CacheError("failed to get metrics", err)
	}

	parseInt := func(v interface{}) int64 {
		if v == nil {
			return 0
		}
		if s, ok := v.(string); ok {
			n, _ := strconv.ParseInt(s, 10, 64)
			return n
		}
		return 0
	}

	idx := 0
	total := parseInt(vals[idx]); idx++
	success := parseInt(vals[idx]); idx++
	failed := parseInt(vals[idx]); idx++
	replay := parseInt(vals[idx]); idx++
	idempotentBlocked := parseInt(vals[idx]); idx++
	inFlight := parseInt(vals[idx]); idx++

	pathDist := make(map[string]int64)
	for _, path := range paths {
		pathDist[path] = parseInt(vals[idx]); idx++
	}

	errorDist := make(map[string]int64)
	for _, code := range errorCodes {
		errorDist[strconv.Itoa(int(code))] = parseInt(vals[idx]); idx++
	}

	successRate := 0.0
	if total > 0 {
		successRate = float64(success) / float64(total) * 100
		successRate = float64(int(successRate*100)) / 100
	}

	return &model.RealtimeMetrics{
		TotalRequests:     total,
		SuccessRequests:   success,
		FailedRequests:    failed,
		ReplayIntercepted: replay,
		IdempotentBlocked: idempotentBlocked,
		RequestsInFlight:  inFlight,
		SuccessRate:       successRate,
		ErrorDistribution: errorDist,
		PathDistribution:  pathDist,
		UpdatedAt:         time.Now().Unix(),
	}, nil
}

func (m *metricsCollector) ResetCounters(ctx context.Context) error {
	client := m.redis.GetClient()

	pattern := metricsPrefix + "*"
	var cursor uint64
	var keys []string

	for {
		var batch []string
		var err error
		batch, cursor, err = client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return appErr.CacheError("failed to scan metrics keys", err)
		}
		keys = append(keys, batch...)
		if cursor == 0 {
			break
		}
	}

	if len(keys) > 0 {
		if err := client.Del(ctx, keys...).Err(); err != nil {
			return appErr.CacheError("failed to reset metrics", err)
		}
	}

	return nil
}
