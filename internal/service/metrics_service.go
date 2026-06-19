package service

import (
	"context"
	"time"

	"privacy-relay/internal/model"
	"privacy-relay/internal/repository"
)

type MetricsService interface {
	GetMetrics(ctx context.Context, req *model.MetricsRequest) (*model.MetricsResponse, error)
	ResetRealtimeCounters(ctx context.Context) error
}

type metricsService struct {
	metricsCollector MetricsCollector
	metricsRepo      repository.MetricsRepository
	relayRepo        repository.RelayRepository
}

func NewMetricsService(
	metricsCollector MetricsCollector,
	metricsRepo repository.MetricsRepository,
	relayRepo repository.RelayRepository,
) MetricsService {
	return &metricsService{
		metricsCollector: metricsCollector,
		metricsRepo:      metricsRepo,
		relayRepo:        relayRepo,
	}
}

func (s *metricsService) GetMetrics(ctx context.Context, req *model.MetricsRequest) (*model.MetricsResponse, error) {
	realtime, err := s.metricsCollector.GetRealtimeMetrics(ctx)
	if err != nil {
		return nil, err
	}

	historical, err := s.getHistoricalMetrics(ctx)
	if err != nil {
		return nil, err
	}

	daily, err := s.getDailyMetrics(ctx, req)
	if err != nil {
		return nil, err
	}

	return &model.MetricsResponse{
		Realtime:    realtime,
		Historical:  historical,
		Daily:       daily,
		GeneratedAt: time.Now().Unix(),
	}, nil
}

func (s *metricsService) getHistoricalMetrics(ctx context.Context) (*model.HistoricalMetrics, error) {
	statusDist, err := s.metricsRepo.GetStatusDistribution(ctx)
	if err != nil {
		return nil, err
	}

	totalDispatches, err := s.metricsRepo.GetTotalDispatches(ctx)
	if err != nil {
		return nil, err
	}

	totalRelays, err := s.metricsRepo.GetTotalRelays(ctx)
	if err != nil {
		return nil, err
	}

	statusMap := make(map[string]int64)
	var success, failed, retrying, processing, pending, expired int64

	for _, item := range statusDist {
		statusMap[item.Status] = item.Count
		switch item.Status {
		case string(model.RelayStatusSuccess):
			success = item.Count
		case string(model.RelayStatusFailed):
			failed = item.Count
		case string(model.RelayStatusRetrying):
			retrying = item.Count
		case string(model.RelayStatusDecrypting), string(model.RelayStatusDistributed):
			processing += item.Count
		case string(model.RelayStatusRegistered):
			pending = item.Count
		case string(model.RelayStatusExpired):
			expired = item.Count
		}
	}

	successRate := 0.0
	if success+failed > 0 {
		successRate = float64(success) / float64(success+failed) * 100
		successRate = float64(int(successRate*100)) / 100
	}

	return &model.HistoricalMetrics{
		TotalRelays:        totalRelays,
		SuccessRelays:      success,
		FailedRelays:       failed,
		RetryingRelays:     retrying,
		ProcessingRelays:   processing,
		PendingRelays:      pending,
		ExpiredRelays:      expired,
		TotalDispatches:    totalDispatches,
		SuccessRate:        successRate,
		StatusDistribution: statusMap,
	}, nil
}

func (s *metricsService) getDailyMetrics(ctx context.Context, req *model.MetricsRequest) ([]*model.DailyMetrics, error) {
	var startDate, endDate time.Time
	now := time.Now()

	days := req.Days
	if days <= 0 {
		days = 7
	}

	if req.StartDate != "" && req.EndDate != "" {
		var err error
		startDate, err = time.ParseInLocation("2006-01-02", req.StartDate, time.Local)
		if err != nil {
			return nil, err
		}
		endDate, err = time.ParseInLocation("2006-01-02", req.EndDate, time.Local)
		if err != nil {
			return nil, err
		}
	} else {
		endDate = now
		startDate = endDate.AddDate(0, 0, -days+1)
	}

	rawData, err := s.metricsRepo.GetDailyMetrics(ctx, startDate, endDate)
	if err != nil {
		return nil, err
	}

	dateMap := make(map[string]*model.DailyMetrics)

	for _, item := range rawData {
		dm, exists := dateMap[item.Date]
		if !exists {
			dm = &model.DailyMetrics{
				Date:               item.Date,
				StatusDistribution: make(map[string]int64),
			}
			dateMap[item.Date] = dm
		}
		dm.TotalRelays += item.Count
		dm.StatusDistribution[item.Status] = item.Count
		if item.Status == string(model.RelayStatusSuccess) {
			dm.SuccessRelays += item.Count
		} else if item.Status == string(model.RelayStatusFailed) {
			dm.FailedRelays += item.Count
		}
	}

	dates := make([]string, 0, len(dateMap))
	for d := range dateMap {
		dates = append(dates, d)
	}

	result := make([]*model.DailyMetrics, 0, len(dates))
	for _, d := range dates {
		result = append(result, dateMap[d])
	}

	return result, nil
}

func (s *metricsService) ResetRealtimeCounters(ctx context.Context) error {
	return s.metricsCollector.ResetCounters(ctx)
}
