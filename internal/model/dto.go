package model

import "time"

type RegisterRelayRequest struct {
	IdempotentKey  string `json:"idempotent_key" binding:"required,min=1,max=128"`
	ClientID       string `json:"client_id" binding:"required,min=1,max=64"`
	Ciphertext     string `json:"ciphertext" binding:"required,min=1"`
	TargetEndpoint string `json:"target_endpoint" binding:"required,url,max=512"`
	MaxRetryCount  int    `json:"max_retry_count" binding:"omitempty,min=0,max=100"`
	TTLSeconds     int    `json:"ttl_seconds" binding:"omitempty,min=60,max=8640000"`
}

type RegisterRelayResponse struct {
	RelayID string      `json:"relay_id"`
	Status  RelayStatus `json:"status"`
}

type GetRelayResponse struct {
	RelayID       string      `json:"relay_id"`
	IdempotentKey string      `json:"idempotent_key"`
	ClientID      string      `json:"client_id"`
	Status        RelayStatus `json:"status"`
	RetryCount    int         `json:"retry_count"`
	MaxRetryCount int         `json:"max_retry_count"`
	LastError     string      `json:"last_error,omitempty"`
	Plaintext     string      `json:"plaintext,omitempty"`
	DistributedAt *time.Time  `json:"distributed_at,omitempty"`
	LastRetryAt   *time.Time  `json:"last_retry_at,omitempty"`
	CompletedAt   *time.Time  `json:"completed_at,omitempty"`
	ExpireAt      time.Time   `json:"expire_at"`
	CreatedAt     time.Time   `json:"created_at"`
}

type DispatchDecryptRequest struct {
	RelayID string `json:"relay_id" binding:"required,min=1,max=64"`
}

type DispatchDecryptResponse struct {
	RelayID    string      `json:"relay_id"`
	Status     RelayStatus `json:"status"`
	DispatchID string      `json:"dispatch_id"`
}

type UpdateDecryptStatusRequest struct {
	RelayID    string `json:"relay_id" binding:"required,min=1,max=64"`
	DispatchID string `json:"dispatch_id" binding:"required,min=1,max=128"`
	Success    bool   `json:"success"`
	Plaintext  string `json:"plaintext"`
	ErrorMsg   string `json:"error_msg"`
}

type UpdateDecryptStatusResponse struct {
	RelayID     string      `json:"relay_id"`
	Status      RelayStatus `json:"status"`
	RetryCount  int         `json:"retry_count"`
	NextRetryAt *time.Time  `json:"next_retry_at,omitempty"`
}

type ListRelaysRequest struct {
	ClientID string        `form:"client_id" binding:"omitempty,min=1,max=64"`
	Status   []RelayStatus `form:"status" binding:"omitempty"`
	PageNum  int           `form:"page_num" binding:"omitempty,min=1"`
	PageSize int           `form:"page_size" binding:"omitempty,min=1,max=100"`
}

type ListRelaysResponse struct {
	Total int64               `json:"total"`
	Items []*GetRelayResponse `json:"items"`
}

type CommonResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type MetricsRequest struct {
	StartDate string `form:"start_date" binding:"omitempty,datetime=2006-01-02"`
	EndDate   string `form:"end_date" binding:"omitempty,datetime=2006-01-02"`
	Days      int    `form:"days" binding:"omitempty,min=1,max=365"`
}

type RealtimeMetrics struct {
	TotalRequests      int64            `json:"total_requests"`
	SuccessRequests    int64            `json:"success_requests"`
	FailedRequests     int64            `json:"failed_requests"`
	ReplayIntercepted  int64            `json:"replay_intercepted"`
	IdempotentBlocked  int64            `json:"idempotent_blocked"`
	RequestsInFlight   int64            `json:"requests_in_flight"`
	SuccessRate        float64          `json:"success_rate"`
	ErrorDistribution  map[string]int64 `json:"error_distribution"`
	PathDistribution   map[string]int64 `json:"path_distribution"`
	UpdatedAt          int64            `json:"updated_at"`
}

type HistoricalMetrics struct {
	TotalRelays        int64            `json:"total_relays"`
	SuccessRelays      int64            `json:"success_relays"`
	FailedRelays       int64            `json:"failed_relays"`
	RetryingRelays     int64            `json:"retrying_relays"`
	ProcessingRelays   int64            `json:"processing_relays"`
	PendingRelays      int64            `json:"pending_relays"`
	ExpiredRelays      int64            `json:"expired_relays"`
	TotalDispatches    int64            `json:"total_dispatches"`
	SuccessRate        float64          `json:"success_rate"`
	StatusDistribution map[string]int64 `json:"status_distribution"`
}

type DailyMetrics struct {
	Date               string           `json:"date"`
	TotalRelays        int64            `json:"total_relays"`
	SuccessRelays      int64            `json:"success_relays"`
	FailedRelays       int64            `json:"failed_relays"`
	StatusDistribution map[string]int64 `json:"status_distribution"`
}

type MetricsResponse struct {
	Realtime   *RealtimeMetrics   `json:"realtime"`
	Historical *HistoricalMetrics `json:"historical"`
	Daily      []*DailyMetrics    `json:"daily,omitempty"`
	GeneratedAt int64             `json:"generated_at"`
}
