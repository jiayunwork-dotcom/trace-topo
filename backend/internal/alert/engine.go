package alert

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"trace-topo/internal/model"
)

type Store interface {
	GetAlertRules(ctx context.Context) ([]*model.AlertRule, error)
	GetAlertRule(ctx context.Context, id int) (*model.AlertRule, error)
	CreateAlertRule(ctx context.Context, rule *model.AlertRule) (*model.AlertRule, error)
	UpdateAlertRule(ctx context.Context, rule *model.AlertRule) (*model.AlertRule, error)
	DeleteAlertRule(ctx context.Context, id int) error
	CreateAlertEvent(ctx context.Context, event *model.AlertEvent) (*model.AlertEvent, error)
	GetAlertEvents(ctx context.Context, ruleID *int, serviceName string, limit, offset int) ([]*model.AlertEvent, int64, error)
	GetAlertEvent(ctx context.Context, id int) (*model.AlertEvent, error)
	AcknowledgeAlertEvent(ctx context.Context, id int) error
	UpdateRuleLastTriggered(ctx context.Context, ruleID int, triggeredAt time.Time) error
	GetServiceErrorRate(ctx context.Context, serviceName string, window time.Duration) (float64, error)
	GetServiceP99Latency(ctx context.Context, serviceName string, window time.Duration) (float64, error)
	GetServiceAvgMetric(ctx context.Context, serviceName, metric string, window time.Duration) (float64, error)
	GetRecentTraceIDs(ctx context.Context, serviceName string, window time.Duration) ([]string, error)
}

type TopologyProvider interface {
	GetTopology(window string) *model.TopologyGraph
}

type Engine struct {
	store    Store
	topology TopologyProvider
	mu       sync.RWMutex
	running  bool
}

func NewEngine(store Store, topology TopologyProvider) *Engine {
	return &Engine{
		store:    store,
		topology: topology,
	}
}

func (e *Engine) Start(ctx context.Context) {
	e.mu.Lock()
	e.running = true
	e.mu.Unlock()

	go e.evaluateLoop(ctx)
	logrus.Info("Alert rule engine started")
}

func (e *Engine) evaluateLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.evaluateAllRules(ctx)
		}
	}
}

func (e *Engine) evaluateAllRules(ctx context.Context) {
	rules, err := e.store.GetAlertRules(ctx)
	if err != nil {
		logrus.Errorf("Failed to get alert rules: %v", err)
		return
	}

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}

		if rule.LastTriggeredAt != nil {
			cooldown := time.Duration(rule.CooldownSeconds) * time.Second
			if time.Since(*rule.LastTriggeredAt) < cooldown {
				continue
			}
		}

		triggered, metricValue, message := e.evaluateRule(ctx, rule)
		if triggered {
			e.fireAlert(ctx, rule, metricValue, message)
		}
	}
}

func (e *Engine) evaluateRule(ctx context.Context, rule *model.AlertRule) (bool, float64, string) {
	switch rule.Type {
	case "threshold":
		return e.evaluateThreshold(ctx, rule)
	case "spike":
		return e.evaluateSpike(ctx, rule)
	case "topology":
		return e.evaluateTopology(ctx, rule)
	default:
		return false, 0, ""
	}
}

func (e *Engine) evaluateThreshold(ctx context.Context, rule *model.AlertRule) (bool, float64, string) {
	var metricValue float64
	var err error

	window := time.Duration(rule.DurationSeconds) * time.Second
	if window < time.Minute {
		window = 5 * time.Minute
	}

	switch rule.Metric {
	case "error_rate":
		metricValue, err = e.store.GetServiceErrorRate(ctx, rule.ServiceName, window)
	case "p99_latency":
		metricValue, err = e.store.GetServiceP99Latency(ctx, rule.ServiceName, window)
	default:
		metricValue, err = e.store.GetServiceAvgMetric(ctx, rule.ServiceName, rule.Metric, window)
	}

	if err != nil {
		logrus.Debugf("Failed to get metric %s for %s: %v", rule.Metric, rule.ServiceName, err)
		return false, 0, ""
	}

	triggered := compareValues(metricValue, rule.Operator, rule.Threshold)
	message := ""
	if triggered {
		message = fmt.Sprintf("%s %s %v (阈值: %v, 当前值: %.4f)",
			rule.ServiceName, rule.Metric, rule.Operator, rule.Threshold, metricValue)
	}

	return triggered, metricValue, message
}

func (e *Engine) evaluateSpike(ctx context.Context, rule *model.AlertRule) (bool, float64, string) {
	window := time.Duration(rule.SpikeWindowMin) * time.Minute
	if window < time.Minute {
		window = time.Hour
	}

	currentWindow := 5 * time.Minute
	var currentValue, baselineValue float64
	var err error

	switch rule.Metric {
	case "error_rate":
		currentValue, err = e.store.GetServiceErrorRate(ctx, rule.ServiceName, currentWindow)
		if err == nil {
			baselineValue, err = e.store.GetServiceErrorRate(ctx, rule.ServiceName, window)
		}
	case "p99_latency":
		currentValue, err = e.store.GetServiceP99Latency(ctx, rule.ServiceName, currentWindow)
		if err == nil {
			baselineValue, err = e.store.GetServiceP99Latency(ctx, rule.ServiceName, window)
		}
	default:
		currentValue, err = e.store.GetServiceAvgMetric(ctx, rule.ServiceName, rule.Metric, currentWindow)
		if err == nil {
			baselineValue, err = e.store.GetServiceAvgMetric(ctx, rule.ServiceName, rule.Metric, window)
		}
	}

	if err != nil {
		return false, 0, ""
	}

	if baselineValue == 0 {
		return currentValue > 0, currentValue, ""
	}

	spikeRatio := currentValue / baselineValue
	triggered := spikeRatio >= rule.SpikeMultiplier

	message := ""
	if triggered {
		message = fmt.Sprintf("%s %s 环比突增 %.1f%% (基线: %.4f, 当前: %.4f, 倍数: %.1f)",
			rule.ServiceName, rule.Metric, (spikeRatio-1)*100, baselineValue, currentValue, spikeRatio)
	}

	return triggered, currentValue, message
}

func (e *Engine) evaluateTopology(ctx context.Context, rule *model.AlertRule) (bool, float64, string) {
	graph := e.topology.GetTopology("5m")
	if graph == nil {
		return false, 0, ""
	}

	if rule.TopologyCheck == "all_downstream_inactive" {
		downstreamEdges := make([]*model.TopologyEdge, 0)
		for _, edge := range graph.Edges {
			if edge.Source == rule.ServiceName {
				downstreamEdges = append(downstreamEdges, edge)
			}
		}

		if len(downstreamEdges) == 0 {
			return false, 0, ""
		}

		allInactive := true
		for _, edge := range downstreamEdges {
			if edge.IsActive {
				allInactive = false
				break
			}
		}

		if allInactive {
			return true, 0, fmt.Sprintf("%s 的所有下游服务均不活跃", rule.ServiceName)
		}
	}

	return false, 0, ""
}

func (e *Engine) fireAlert(ctx context.Context, rule *model.AlertRule, metricValue float64, message string) {
	traceIDs, _ := e.store.GetRecentTraceIDs(ctx, rule.ServiceName, 5*time.Minute)
	if len(traceIDs) > 10 {
		traceIDs = traceIDs[:10]
	}

	event := &model.AlertEvent{
		RuleID:      rule.ID,
		RuleName:    rule.Name,
		Severity:    rule.Severity,
		ServiceName: rule.ServiceName,
		MetricValue: metricValue,
		Threshold:   rule.Threshold,
		Message:     message,
		TraceIDs:    traceIDs,
		FiredAt:     time.Now(),
	}

	created, err := e.store.CreateAlertEvent(ctx, event)
	if err != nil {
		logrus.Errorf("Failed to create alert event: %v", err)
		return
	}

	if err := e.store.UpdateRuleLastTriggered(ctx, rule.ID, time.Now()); err != nil {
		logrus.Warnf("Failed to update rule last triggered: %v", err)
	}

	logrus.Infof("Alert fired: [%s] %s - %s (event #%d)", rule.Severity, rule.Name, message, created.ID)
}

func compareValues(value float64, operator string, threshold float64) bool {
	switch operator {
	case ">":
		return value > threshold
	case ">=":
		return value >= threshold
	case "<":
		return value < threshold
	case "<=":
		return value <= threshold
	case "==":
		return value == threshold
	case "!=":
		return value != threshold
	default:
		return value > threshold
	}
}
