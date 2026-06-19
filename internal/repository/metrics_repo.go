package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"privacy-relay/internal/model"
	appErr "privacy-relay/pkg/errors"
)

type StatusCount struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

type DailyStatusCount struct {
	Date   string `json:"date"`
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

type MetricsRepository interface {
	GetStatusDistribution(ctx context.Context) ([]*StatusCount, error)
	GetTotalDispatches(ctx context.Context) (int64, error)
	GetDailyMetrics(ctx context.Context, startDate, endDate time.Time) ([]*DailyStatusCount, error)
	GetTotalRelays(ctx context.Context) (int64, error)
}

type metricsRepository struct {
	db *Database
}

func NewMetricsRepository(db *Database) MetricsRepository {
	return &metricsRepository{db: db}
}

func (r *metricsRepository) GetStatusDistribution(ctx context.Context) ([]*StatusCount, error) {
	var results []*StatusCount
	err := r.db.GetDB().WithContext(ctx).
		Model(&model.RelayRecord{}).
		Select("status, COUNT(*) as count").
		Group("status").
		Scan(&results).Error
	if err != nil {
		return nil, appErr.DatabaseError("failed to get status distribution", err)
	}
	return results, nil
}

func (r *metricsRepository) GetTotalRelays(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.GetDB().WithContext(ctx).
		Model(&model.RelayRecord{}).
		Count(&count).Error
	if err != nil {
		return 0, appErr.DatabaseError("failed to count total relays", err)
	}
	return count, nil
}

func (r *metricsRepository) GetTotalDispatches(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.GetDB().WithContext(ctx).
		Model(&model.StateTransition{}).
		Where("to_status = ?", model.RelayStatusDistributed).
		Count(&count).Error
	if err != nil {
		return 0, appErr.DatabaseError("failed to count total dispatches", err)
	}
	return count, nil
}

func (r *metricsRepository) GetDailyMetrics(ctx context.Context, startDate, endDate time.Time) ([]*DailyStatusCount, error) {
	start := startDate.Truncate(24 * time.Hour)
	end := endDate.Truncate(24 * time.Hour).Add(24 * time.Hour).Add(-time.Second)

	var results []*DailyStatusCount
	err := r.db.GetDB().WithContext(ctx).
		Model(&model.RelayRecord{}).
		Select("DATE_FORMAT(created_at, '%Y-%m-%d') as date, status, COUNT(*) as count").
		Where("created_at >= ? AND created_at <= ?", start, end).
		Group("DATE_FORMAT(created_at, '%Y-%m-%d'), status").
		Order("date ASC").
		Scan(&results).Error
	if err != nil {
		return nil, appErr.DatabaseError("failed to get daily metrics", err)
	}
	return results, nil
}

func (r *metricsRepository) GetTotalRelaysForStatus(ctx context.Context, status model.RelayStatus) (int64, error) {
	var count int64
	err := r.db.GetDB().WithContext(ctx).
		Model(&model.RelayRecord{}).
		Where("status = ?", status).
		Count(&count).Error
	if err != nil {
		return 0, appErr.DatabaseError("failed to count relays by status", err)
	}
	return count, nil
}

func (r *metricsRepository) WithMetricsTx(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return r.db.WithTransaction(ctx, fn)
}
