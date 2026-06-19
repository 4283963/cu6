package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"privacy-relay/internal/model"
	appErr "privacy-relay/pkg/errors"
)

type RelayRepository interface {
	Create(ctx context.Context, record *model.RelayRecord) error
	GetByRelayID(ctx context.Context, relayID string) (*model.RelayRecord, error)
	GetByIdempotentKey(ctx context.Context, idempotentKey string) (*model.RelayRecord, error)
	UpdateStatus(ctx context.Context, relayID string, fromStatus, toStatus model.RelayStatus, extraUpdates map[string]interface{}) error
	UpdateRetryInfo(ctx context.Context, relayID string, retryCount int, lastError string, lastRetryAt time.Time) error
	MarkSuccess(ctx context.Context, relayID string, plaintext string, completedAt time.Time) error
	List(ctx context.Context, clientID string, statuses []model.RelayStatus, pageNum, pageSize int) ([]*model.RelayRecord, int64, error)
	GetExpiredRegistered(ctx context.Context, limit int) ([]*model.RelayRecord, error)
	GetPendingRetry(ctx context.Context, limit int) ([]*model.RelayRecord, error)
	UpdateWithTx(tx *gorm.DB, ctx context.Context, relayID string, updates map[string]interface{}) error
}

type relayRepository struct {
	db *Database
}

func NewRelayRepository(db *Database) RelayRepository {
	return &relayRepository{db: db}
}

func (r *relayRepository) Create(ctx context.Context, record *model.RelayRecord) error {
	if err := r.db.GetDB().WithContext(ctx).Create(record).Error; err != nil {
		return appErr.DatabaseError("failed to create relay record", err)
	}
	return nil
}

func (r *relayRepository) GetByRelayID(ctx context.Context, relayID string) (*model.RelayRecord, error) {
	var record model.RelayRecord
	err := r.db.GetDB().WithContext(ctx).Where("relay_id = ?", relayID).First(&record).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, appErr.NotFound("relay record not found")
		}
		return nil, appErr.DatabaseError("failed to get relay by id", err)
	}
	return &record, nil
}

func (r *relayRepository) GetByIdempotentKey(ctx context.Context, idempotentKey string) (*model.RelayRecord, error) {
	var record model.RelayRecord
	err := r.db.GetDB().WithContext(ctx).Where("idempotent_key = ?", idempotentKey).First(&record).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, appErr.DatabaseError("failed to get relay by idempotent key", err)
	}
	return &record, nil
}

func (r *relayRepository) UpdateStatus(ctx context.Context, relayID string, fromStatus, toStatus model.RelayStatus, extraUpdates map[string]interface{}) error {
	updates := map[string]interface{}{
		"status":     toStatus,
		"updated_at": time.Now(),
	}
	for k, v := range extraUpdates {
		updates[k] = v
	}

	result := r.db.GetDB().WithContext(ctx).
		Model(&model.RelayRecord{}).
		Where("relay_id = ? AND status = ?", relayID, fromStatus).
		Updates(updates)

	if result.Error != nil {
		return appErr.DatabaseError("failed to update relay status", result.Error)
	}
	if result.RowsAffected == 0 {
		return appErr.StateTransition("status transition conflict: expected status " + string(fromStatus))
	}
	return nil
}

func (r *relayRepository) UpdateRetryInfo(ctx context.Context, relayID string, retryCount int, lastError string, lastRetryAt time.Time) error {
	updates := map[string]interface{}{
		"retry_count":   retryCount,
		"last_error":    lastError,
		"last_retry_at": lastRetryAt,
		"updated_at":    time.Now(),
	}

	result := r.db.GetDB().WithContext(ctx).
		Model(&model.RelayRecord{}).
		Where("relay_id = ?", relayID).
		Updates(updates)

	if result.Error != nil {
		return appErr.DatabaseError("failed to update retry info", result.Error)
	}
	return nil
}

func (r *relayRepository) MarkSuccess(ctx context.Context, relayID string, plaintext string, completedAt time.Time) error {
	updates := map[string]interface{}{
		"status":       model.RelayStatusSuccess,
		"plaintext":    plaintext,
		"completed_at": completedAt,
		"updated_at":   time.Now(),
	}

	result := r.db.GetDB().WithContext(ctx).
		Model(&model.RelayRecord{}).
		Where("relay_id = ? AND status IN (?)", relayID, []model.RelayStatus{model.RelayStatusDecrypting, model.RelayStatusRetrying}).
		Updates(updates)

	if result.Error != nil {
		return appErr.DatabaseError("failed to mark success", result.Error)
	}
	if result.RowsAffected == 0 {
		return appErr.StateTransition("invalid state for marking success")
	}
	return nil
}

func (r *relayRepository) List(ctx context.Context, clientID string, statuses []model.RelayStatus, pageNum, pageSize int) ([]*model.RelayRecord, int64, error) {
	query := r.db.GetDB().WithContext(ctx).Model(&model.RelayRecord{})

	if clientID != "" {
		query = query.Where("client_id = ?", clientID)
	}
	if len(statuses) > 0 {
		query = query.Where("status IN ?", statuses)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, appErr.DatabaseError("failed to count relay records", err)
	}

	if pageNum <= 0 {
		pageNum = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	offset := (pageNum - 1) * pageSize

	var records []*model.RelayRecord
	if err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&records).Error; err != nil {
		return nil, 0, appErr.DatabaseError("failed to list relay records", err)
	}

	return records, total, nil
}

func (r *relayRepository) GetExpiredRegistered(ctx context.Context, limit int) ([]*model.RelayRecord, error) {
	var records []*model.RelayRecord
	err := r.db.GetDB().WithContext(ctx).
		Where("status = ? AND expire_at < ?", model.RelayStatusRegistered, time.Now()).
		Limit(limit).
		Find(&records).Error
	if err != nil {
		return nil, appErr.DatabaseError("failed to get expired registered records", err)
	}
	return records, nil
}

func (r *relayRepository) GetPendingRetry(ctx context.Context, limit int) ([]*model.RelayRecord, error) {
	var records []*model.RelayRecord
	err := r.db.GetDB().WithContext(ctx).
		Where("status = ? AND retry_count < max_retry_count", model.RelayStatusRetrying).
		Order("last_retry_at ASC").
		Limit(limit).
		Find(&records).Error
	if err != nil {
		return nil, appErr.DatabaseError("failed to get pending retry records", err)
	}
	return records, nil
}

func (r *relayRepository) UpdateWithTx(tx *gorm.DB, ctx context.Context, relayID string, updates map[string]interface{}) error {
	if tx == nil {
		tx = r.db.GetDB()
	}
	updates["updated_at"] = time.Now()
	result := tx.WithContext(ctx).
		Model(&model.RelayRecord{}).
		Where("relay_id = ?", relayID).
		Updates(updates)
	if result.Error != nil {
		return appErr.DatabaseError("failed to update relay with tx", result.Error)
	}
	return nil
}
