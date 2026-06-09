package health

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"trace-topo/internal/model"
)

type Store interface {
	GetServices(ctx context.Context) ([]string, error)
	GetServiceErrorRate(ctx context.Context, serviceName string, window time.Duration) (float64, error)
	GetServiceP99Latency(ctx context.Context, serviceName string, window time.Duration) (float64, error)
	GetServiceUpstreamSuccessRate(ctx context.Context, serviceName string, window time.Duration) (float64, error)
	GetHealthBaseline(ctx context.Context, serviceName string) (float64, error)
	UpsertHealthBaseline(ctx context.Context, serviceName string, baseline float64) error
	WriteHealthScore(ctx context.Context, score *model.HealthScore) error
	GetHealthScores(ctx context.Context, serviceName string, limit int) ([]*model.HealthScore, error)
	GetAllLatestHealthScores(ctx context.Context) (map[string]*model.HealthScore, error)
	CleanupOldHealthScores(ctx context.Context) error
}

type TopologyProvider interface {
	GetTopology(window string) *model.TopologyGraph
}

type Scorer struct {
	store    Store
	topology TopologyProvider
	cache    map[string]*model.HealthScore
	mu       sync.RWMutex
}

func NewScorer(store Store, topology TopologyProvider) *Scorer {
	return &Scorer{
		store:    store,
		topology: topology,
		cache:    make(map[string]*model.HealthScore),
	}
}

func (s *Scorer) Start(ctx context.Context) {
	go s.scoreLoop(ctx)
	go s.cleanupLoop(ctx)
	go s.baselineLoop(ctx)
	logrus.Info("Health scorer started")
}

func (s *Scorer) scoreLoop(ctx context.Context) {
	s.calculateAllScores(ctx)
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.calculateAllScores(ctx)
		}
	}
}

func (s *Scorer) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.store.CleanupOldHealthScores(ctx); err != nil {
				logrus.Warnf("Failed to cleanup old health scores: %v", err)
			}
		}
	}
}

func (s *Scorer) baselineLoop(ctx context.Context) {
	s.updateBaselines(ctx)
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.updateBaselines(ctx)
		}
	}
}

func (s *Scorer) updateBaselines(ctx context.Context) {
	services, err := s.store.GetServices(ctx)
	if err != nil {
		logrus.Warnf("Failed to get services for baseline update: %v", err)
		return
	}

	for _, svc := range services {
		p99, err := s.store.GetServiceP99Latency(ctx, svc, 24*time.Hour)
		if err != nil {
			continue
		}
		if p99 > 0 {
			if err := s.store.UpsertHealthBaseline(ctx, svc, p99); err != nil {
				logrus.Warnf("Failed to update baseline for %s: %v", svc, err)
			}
		}
	}
}

func (s *Scorer) calculateAllScores(ctx context.Context) {
	services, err := s.store.GetServices(ctx)
	if err != nil {
		logrus.Errorf("Failed to get services for health scoring: %v", err)
		return
	}

	for _, svc := range services {
		score := s.calculateServiceScore(ctx, svc)
		if score == nil {
			continue
		}

		if err := s.store.WriteHealthScore(ctx, score); err != nil {
			logrus.Warnf("Failed to write health score for %s: %v", svc, err)
		}

		s.mu.Lock()
		s.cache[svc] = score
		s.mu.Unlock()
	}
}

func (s *Scorer) calculateServiceScore(ctx context.Context, serviceName string) *model.HealthScore {
	window := 5 * time.Minute

	errorRate, err := s.store.GetServiceErrorRate(ctx, serviceName, window)
	if err != nil {
		logrus.Debugf("Failed to get error rate for %s: %v", serviceName, err)
		errorRate = 0
	}

	p99Latency, err := s.store.GetServiceP99Latency(ctx, serviceName, window)
	if err != nil {
		logrus.Debugf("Failed to get P99 for %s: %v", serviceName, err)
		p99Latency = 0
	}

	upstreamRate, err := s.store.GetServiceUpstreamSuccessRate(ctx, serviceName, window)
	if err != nil {
		logrus.Debugf("Failed to get upstream success rate for %s: %v", serviceName, err)
		upstreamRate = 1.0
	}

	baseline, err := s.store.GetHealthBaseline(ctx, serviceName)
	if err != nil || baseline == 0 {
		baseline = p99Latency
	}

	errorRateScore := calcErrorRateScore(errorRate)
	p99DeviationScore, deviation := calcP99DeviationScore(p99Latency, baseline)
	upstreamScore := calcUpstreamSuccessScore(upstreamRate)

	totalScore := int(math.Round(
		float64(errorRateScore)*0.4 +
			float64(p99DeviationScore)*0.3 +
			float64(upstreamScore)*0.3,
	))

	if totalScore < 0 {
		totalScore = 0
	}
	if totalScore > 100 {
		totalScore = 100
	}

	return &model.HealthScore{
		ServiceName:          serviceName,
		Score:                totalScore,
		ErrorRate:            errorRate,
		ErrorRateScore:       errorRateScore,
		P99Deviation:         deviation,
		P99DeviationScore:    p99DeviationScore,
		UpstreamSuccessRate:  upstreamRate,
		UpstreamSuccessScore: upstreamScore,
		P99Baseline:          baseline,
		CalculatedAt:         time.Now(),
	}
}

func calcErrorRateScore(errorRate float64) int {
	if errorRate <= 0 {
		return 100
	}
	if errorRate >= 1.0 {
		return 0
	}
	score := 100 * (1 - errorRate*10)
	if score < 0 {
		score = 0
	}
	return int(math.Round(score))
}

func calcP99DeviationScore(p99, baseline float64) (int, float64) {
	if baseline == 0 || p99 == 0 {
		return 100, 0
	}
	deviation := (p99 - baseline) / baseline
	if deviation <= 0 {
		return 100, deviation
	}
	if deviation >= 3.0 {
		return 0, deviation
	}
	score := 100 * (1 - deviation/3.0)
	if score < 0 {
		score = 0
	}
	return int(math.Round(score)), deviation
}

func calcUpstreamSuccessScore(successRate float64) int {
	if successRate >= 1.0 {
		return 100
	}
	if successRate <= 0 {
		return 0
	}
	score := 100 * successRate
	return int(math.Round(score))
}

func (s *Scorer) GetHealthScore(serviceName string) *model.HealthScore {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cache[serviceName]
}

func (s *Scorer) GetAllHealthScores() map[string]*model.HealthScore {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]*model.HealthScore, len(s.cache))
	for k, v := range s.cache {
		result[k] = v
	}
	return result
}
