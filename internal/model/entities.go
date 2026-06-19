package model

import (
	"time"
)

type RelayStatus string

const (
	RelayStatusRegistered  RelayStatus = "REGISTERED"
	RelayStatusDistributed RelayStatus = "DISTRIBUTED"
	RelayStatusDecrypting  RelayStatus = "DECRYPTING"
	RelayStatusSuccess     RelayStatus = "SUCCESS"
	RelayStatusFailed      RelayStatus = "FAILED"
	RelayStatusRetrying    RelayStatus = "RETRYING"
	RelayStatusExpired     RelayStatus = "EXPIRED"
)

type RelayRecord struct {
	ID             uint64      `gorm:"primaryKey;autoIncrement" json:"id"`
	RelayID        string      `gorm:"type:varchar(64);uniqueIndex:uk_relay_id;not null" json:"relay_id"`
	IdempotentKey  string      `gorm:"type:varchar(128);uniqueIndex:uk_idempotent_key;not null" json:"idempotent_key"`
	ClientID       string      `gorm:"type:varchar(64);index:idx_client_id;not null" json:"client_id"`
	Ciphertext     string      `gorm:"type:text;not null" json:"ciphertext"`
	Plaintext      string      `gorm:"type:text" json:"plaintext,omitempty"`
	Status         RelayStatus `gorm:"type:varchar(32);index:idx_status;not null;default:'REGISTERED'" json:"status"`
	TargetEndpoint string      `gorm:"type:varchar(512);not null" json:"target_endpoint"`
	RetryCount     int         `gorm:"not null;default:0" json:"retry_count"`
	MaxRetryCount  int         `gorm:"not null;default:5" json:"max_retry_count"`
	LastError      string      `gorm:"type:text" json:"last_error,omitempty"`
	DistributedAt  *time.Time  `json:"distributed_at,omitempty"`
	LastRetryAt    *time.Time  `json:"last_retry_at,omitempty"`
	CompletedAt    *time.Time  `json:"completed_at,omitempty"`
	ExpireAt       time.Time   `gorm:"not null;index:idx_expire_at" json:"expire_at"`
	CreatedAt      time.Time   `gorm:"not null;index:idx_created_at" json:"created_at"`
	UpdatedAt      time.Time   `gorm:"not null" json:"updated_at"`
}

func (RelayRecord) TableName() string {
	return "relay_records"
}

type StateTransition struct {
	ID            uint64      `gorm:"primaryKey;autoIncrement" json:"id"`
	RelayID       string      `gorm:"type:varchar(64);index:idx_relay_id;not null" json:"relay_id"`
	FromStatus    RelayStatus `gorm:"type:varchar(32);not null" json:"from_status"`
	ToStatus      RelayStatus `gorm:"type:varchar(32);not null" json:"to_status"`
	TriggerReason string      `gorm:"type:varchar(256)" json:"trigger_reason,omitempty"`
	Operator      string      `gorm:"type:varchar(64)" json:"operator,omitempty"`
	Remark        string      `gorm:"type:varchar(512)" json:"remark,omitempty"`
	CreatedAt     time.Time   `gorm:"not null" json:"created_at"`
}

func (StateTransition) TableName() string {
	return "state_transitions"
}

type ReplayRecord struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Nonce     string    `gorm:"type:varchar(128);uniqueIndex:uk_nonce;not null" json:"nonce"`
	ClientID  string    `gorm:"type:varchar(64);index:idx_client_id;not null" json:"client_id"`
	Timestamp int64     `gorm:"not null" json:"timestamp"`
	RequestID string    `gorm:"type:varchar(64)" json:"request_id"`
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
	ExpireAt  time.Time `gorm:"not null;index:idx_expire_at" json:"expire_at"`
}

func (ReplayRecord) TableName() string {
	return "replay_records"
}
