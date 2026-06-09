package sampling

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"

	"trace-topo/internal/config"
	"trace-topo/internal/model"
)

type ConfigStore interface {
	UpdateSamplingConfig(ctx context.Context, headRate, tailNormalRate, tailAnomalyRate float64) error
	GetSamplingConfig(ctx context.Context) (*model.SamplingConfig, error)
}

type AnomalyChecker interface {
	IsAnomaly(trace *model.Trace) bool
}

type Engine struct {
	configStore    ConfigStore
	anomalyChecker AnomalyChecker

	headRate        atomic.Value
	tailNormalRate  atomic.Value
	tailAnomalyRate atomic.Value

	headKept      int64
	headTotal     int64
	tailKept      int64
	tailTotal     int64
	mu            sync.Mutex
}

func NewEngine(configStore ConfigStore, anomalyChecker AnomalyChecker) *Engine {
	e := &Engine{
		configStore:    configStore,
		anomalyChecker: anomalyChecker,
	}

	e.headRate.Store(config.AppConfig.Sampling.HeadRate)
	e.tailNormalRate.Store(config.AppConfig.Sampling.TailNormalRate)
	e.tailAnomalyRate.Store(config.AppConfig.Sampling.TailAnomalyRate)

	go e.reloadLoop()

	logrus.Info("Sampling engine initialized")
	return e
}

func (e *Engine) reloadLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if cfg, err := e.configStore.GetSamplingConfig(ctx); err == nil {
			e.headRate.Store(cfg.HeadRate)
			e.tailNormalRate.Store(cfg.TailNormalRate)
			e.tailAnomalyRate.Store(cfg.TailAnomalyRate)
			logrus.Debugf("Reloaded sampling config: head=%.2f, tail_normal=%.2f, tail_anomaly=%.2f",
				cfg.HeadRate, cfg.TailNormalRate, cfg.TailAnomalyRate)
		}
		cancel()
	}
}

func (e *Engine) ShouldKeepHead(traceID string) bool {
	rate := e.headRate.Load().(float64)
	if rate >= 1.0 {
		e.mu.Lock()
		e.headKept++
		e.headTotal++
		e.mu.Unlock()
		return true
	}
	if rate <= 0 {
		e.mu.Lock()
		e.headTotal++
		e.mu.Unlock()
		return false
	}

	hash := sha256.Sum256([]byte(traceID))
	hashVal := binary.BigEndian.Uint64(hash[:8])
	normalized := float64(hashVal) / float64(^uint64(0))

	keep := normalized < rate

	e.mu.Lock()
	e.headTotal++
	if keep {
		e.headKept++
	}
	e.mu.Unlock()

	return keep
}

func (e *Engine) ShouldKeepTail(ctx context.Context, trace *model.Trace) bool {
	var rate float64

	if e.anomalyChecker != nil && e.anomalyChecker.IsAnomaly(trace) {
		rate = e.tailAnomalyRate.Load().(float64)
	} else {
		rate = e.tailNormalRate.Load().(float64)
	}

	if rate >= 1.0 {
		e.mu.Lock()
		e.tailKept++
		e.tailTotal++
		e.mu.Unlock()
		return true
	}
	if rate <= 0 {
		e.mu.Lock()
		e.tailTotal++
		e.mu.Unlock()
		return false
	}

	keep := rand.Float64() < rate

	e.mu.Lock()
	e.tailTotal++
	if keep {
		e.tailKept++
	}
	e.mu.Unlock()

	return keep
}

func (e *Engine) UpdateConfig(ctx context.Context, cfg *model.SamplingConfig) error {
	if cfg.HeadRate < 0 || cfg.HeadRate > 1 {
		cfg.HeadRate = 1.0
	}
	if cfg.TailNormalRate < 0 || cfg.TailNormalRate > 1 {
		cfg.TailNormalRate = 0.1
	}
	if cfg.TailAnomalyRate < 0 || cfg.TailAnomalyRate > 1 {
		cfg.TailAnomalyRate = 1.0
	}

	e.headRate.Store(cfg.HeadRate)
	e.tailNormalRate.Store(cfg.TailNormalRate)
	e.tailAnomalyRate.Store(cfg.TailAnomalyRate)

	if e.configStore != nil {
		if err := e.configStore.UpdateSamplingConfig(ctx, cfg.HeadRate, cfg.TailNormalRate, cfg.TailAnomalyRate); err != nil {
			logrus.Warnf("Failed to persist sampling config: %v", err)
		}
	}

	logrus.Infof("Sampling config updated: head=%.2f, tail_normal=%.2f, tail_anomaly=%.2f",
		cfg.HeadRate, cfg.TailNormalRate, cfg.TailAnomalyRate)

	return nil
}

func (e *Engine) GetConfig() *model.SamplingConfig {
	return &model.SamplingConfig{
		HeadRate:        e.headRate.Load().(float64),
		TailNormalRate:  e.tailNormalRate.Load().(float64),
		TailAnomalyRate: e.tailAnomalyRate.Load().(float64),
	}
}

func (e *Engine) GetStats() map[string]interface{} {
	e.mu.Lock()
	defer e.mu.Unlock()

	headKeepRate := float64(0)
	if e.headTotal > 0 {
		headKeepRate = float64(e.headKept) / float64(e.headTotal)
	}

	tailKeepRate := float64(0)
	if e.tailTotal > 0 {
		tailKeepRate = float64(e.tailKept) / float64(e.tailTotal)
	}

	return map[string]interface{}{
		"head_kept":       e.headKept,
		"head_total":      e.headTotal,
		"head_keep_rate":  headKeepRate,
		"tail_kept":       e.tailKept,
		"tail_total":      e.tailTotal,
		"tail_keep_rate":  tailKeepRate,
	}
}

func (e *Engine) ResetStats() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.headKept = 0
	e.headTotal = 0
	e.tailKept = 0
	e.tailTotal = 0
}
