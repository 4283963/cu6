package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"privacy-relay/internal/model"
	appErr "privacy-relay/pkg/errors"
)

type StateTransitionRepository interface {
	Create(ctx context.Context, transition *model.StateTransition) error
	CreateWithTx(tx *gorm.DB, ctx context.Context, transition *model.StateTransition) error
	ListByRelayID(ctx context.Context, relayID string) ([]*model.StateTransition, error)
}

type stateTransitionRepository struct {
	db *Database
}

func NewStateTransitionRepository(db *Database) StateTransitionRepository {
	return &stateTransitionRepository{db: db}
}

func (r *stateTransitionRepository) Create(ctx context.Context, transition *model.StateTransition) error {
	if transition.CreatedAt.IsZero() {
		transition.CreatedAt = time.Now()
	}
	if err := r.db.GetDB().WithContext(ctx).Create(transition).Error; err != nil {
		return appErr.DatabaseError("failed to create state transition", err)
	}
	return nil
}

func (r *stateTransitionRepository) CreateWithTx(tx *gorm.DB, ctx context.Context, transition *model.StateTransition) error {
	if tx == nil {
		tx = r.db.GetDB()
	}
	if transition.CreatedAt.IsZero() {
		transition.CreatedAt = time.Now()
	}
	if err := tx.WithContext(ctx).Create(transition).Error; err != nil {
		return appErr.DatabaseError("failed to create state transition with tx", err)
	}
	return nil
}

func (r *stateTransitionRepository) ListByRelayID(ctx context.Context, relayID string) ([]*model.StateTransition, error) {
	var transitions []*model.StateTransition
	err := r.db.GetDB().WithContext(ctx).
		Where("relay_id = ?", relayID).
		Order("created_at ASC").
		Find(&transitions).Error
	if err != nil {
		return nil, appErr.DatabaseError("failed to list state transitions", err)
	}
	return transitions, nil
}

type ReplayRecordRepository interface {
	Create(ctx context.Context, record *model.ReplayRecord) error
	GetByNonce(ctx context.Context, nonce string) (*model.ReplayRecord, error)
	CreateIfNotExists(ctx context.Context, record *model.ReplayRecord) (bool, error)
	CleanExpired(ctx context.Context, limit int) (int64, error)
}

type replayRecordRepository struct {
	db *Database
}

func NewReplayRecordRepository(db *Database) ReplayRecordRepository {
	return &replayRecordRepository{db: db}
}

func (r *replayRecordRepository) Create(ctx context.Context, record *model.ReplayRecord) error {
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	if err := r.db.GetDB().WithContext(ctx).Create(record).Error; err != nil {
		return appErr.DatabaseError("failed to create replay record", err)
	}
	return nil
}

func (r *replayRecordRepository) GetByNonce(ctx context.Context, nonce string) (*model.ReplayRecord, error) {
	var record model.ReplayRecord
	err := r.db.GetDB().WithContext(ctx).Where("nonce = ?", nonce).First(&record).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, appErr.DatabaseError("failed to get replay record", err)
	}
	return &record, nil
}

const ukNonceDuplicateCode = 1062

func (r *replayRecordRepository) CreateIfNotExists(ctx context.Context, record *model.ReplayRecord) (bool, error) {
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	err := r.db.GetDB().WithContext(ctx).Create(record).Error
	if err != nil {
		if isDuplicateEntryError(err) {
			return false, nil
		}
		return false, appErr.DatabaseError("failed to create replay record", err)
	}
	return true, nil
}

func isDuplicateEntryError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return len(errStr) > 0 && containsMySQLDuplicateCode(errStr)
}

func containsMySQLDuplicateCode(s string) bool {
	for i := 0; i <= len(s)-4; i++ {
		if s[i:i+4] == "1062" {
			return true
		}
	}
	return false
}

func (r *replayRecordRepository) CleanExpired(ctx context.Context, limit int) (int64, error) {
	result := r.db.GetDB().WithContext(ctx).
		Where("expire_at < ?", time.Now()).
		Limit(limit).
		Delete(&model.ReplayRecord{})
	if result.Error != nil {
		return 0, appErr.DatabaseError("failed to clean expired replay records", result.Error)
	}
	return result.RowsAffected, nil
}
