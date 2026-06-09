package slo

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"trace-topo/internal/model"
)

type Store interface {
	GetSLODefinitions(ctx context.Context) ([]*model.SLODefinition, error)
	GetSLODefinition(ctx context.Context, id int) (*model.SLODefinition, error)
	WriteSLOBudgetSnapshot(ctx context.Context, snap *model.SLOBudgetSnapshot) error
	GetLatestSLOBudgetSnapshot(ctx context.Context, sloID int) (*model.SLOBudgetSnapshot, error)
	GetSLOSpanCounts(ctx context.Context, serviceName string, windowStart, windowEnd time.Time) (total int64, errors int64, err error)
	GetSLOSlowSpanCounts(ctx context.Context, serviceName string, thresholdMs float64, windowStart, windowEnd time.Time) (total int64, slow int64, err error)
	GetSLOServiceQPS(ctx context.Context, serviceName string, windowStart, windowEnd time.Time) (float64, error)
	CreateAlertRule(ctx context.Context, rule *model.AlertRule) (*model.AlertRule, error)
	CreateAlertEvent(ctx context.Context, event *model.AlertEvent) (*model.AlertEvent, error)
	CreateSLOBurnRateAlert(ctx context.Context, a *model.SLOBurnRateAlert) (*model.SLOBurnRateAlert, error)
	ResolveSLOBurnRateAlerts(ctx context.Context, sloID int) error
	UpdateSLOAlertRuleID(ctx context.Context, sloID int, alertRuleID int) error
}

type Engine struct {
	store   Store
	mu      sync.RWMutex
	running bool
}

func NewEngine(store Store) *Engine {
	return &Engine{
		store: store,
	}
}

func (e *Engine) Start(ctx context.Context) {
	e.mu.Lock()
	e.running = true
	e.mu.Unlock()

	go e.calculateLoop(ctx)
	logrus.Info("SLO engine started")
}

func (e *Engine) calculateLoop(ctx context.Context) {
	e.calculateAll(ctx)

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.calculateAll(ctx)
		}
	}
}

func (e *Engine) calculateAll(ctx context.Context) {
	defs, err := e.store.GetSLODefinitions(ctx)
	if err != nil {
		logrus.Errorf("SLO engine: failed to get definitions: %v", err)
		return
	}

	for _, def := range defs {
		if !def.Enabled {
			continue
		}
		if err := e.calculateSLO(ctx, def); err != nil {
			logrus.Errorf("SLO engine: failed to calculate SLO %d (%s): %v", def.ID, def.Name, err)
		}
	}
}

func (e *Engine) getWindowDuration(windowType string) time.Duration {
	switch windowType {
	case "rolling_7d":
		return 7 * 24 * time.Hour
	case "rolling_30d":
		return 30 * 24 * time.Hour
	case "calendar_month":
		now := time.Now()
		startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		return now.Sub(startOfMonth)
	default:
		return 30 * 24 * time.Hour
	}
}

func (e *Engine) getWindowStart(windowType string) time.Time {
	now := time.Now()
	switch windowType {
	case "rolling_7d":
		return now.Add(-7 * 24 * time.Hour)
	case "rolling_30d":
		return now.Add(-30 * 24 * time.Hour)
	case "calendar_month":
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	default:
		return now.Add(-30 * 24 * time.Hour)
	}
}

func (e *Engine) calculateSLO(ctx context.Context, def *model.SLODefinition) error {
	windowStart := e.getWindowStart(def.WindowType)
	windowEnd := time.Now()

	var totalEvents, badEvents int64
	var currentMeasurement float64

	switch def.TargetType {
	case "availability":
		total, errs, err := e.store.GetSLOSpanCounts(ctx, def.ServiceName, windowStart, windowEnd)
		if err != nil {
			return fmt.Errorf("get span counts: %w", err)
		}
		totalEvents = total
		badEvents = errs
		if totalEvents > 0 {
			currentMeasurement = 1.0 - float64(badEvents)/float64(totalEvents)
		} else {
			currentMeasurement = 1.0
		}

	case "latency":
		threshold := float64(200)
		if def.LatencyThresholdMs != nil {
			threshold = *def.LatencyThresholdMs
		}
		total, slow, err := e.store.GetSLOSlowSpanCounts(ctx, def.ServiceName, threshold, windowStart, windowEnd)
		if err != nil {
			return fmt.Errorf("get slow span counts: %w", err)
		}
		totalEvents = total
		badEvents = slow
		if totalEvents > 0 {
			currentMeasurement = 1.0 - float64(badEvents)/float64(totalEvents)
		} else {
			currentMeasurement = 1.0
		}

	case "throughput":
		actualQPS, err := e.store.GetSLOServiceQPS(ctx, def.ServiceName, windowStart, windowEnd)
		if err != nil {
			return fmt.Errorf("get service QPS: %w", err)
		}
		targetQPS := float64(100)
		if def.TargetQPS != nil {
			targetQPS = *def.TargetQPS
		}

		measurementIntervals := int64(windowEnd.Sub(windowStart) / time.Minute)
		if measurementIntervals <= 0 {
			currentMeasurement = 1.0
			totalEvents = measurementIntervals
			badEvents = 0
		} else {
			totalEvents = measurementIntervals
			belowThresholdCount := int64(0)
			if actualQPS < targetQPS {
				belowThresholdCount = measurementIntervals
			}
			badEvents = belowThresholdCount
			currentMeasurement = 1.0 - float64(badEvents)/float64(totalEvents)
		}
	}

	errorBudgetPct := 1.0 - def.TargetValue
	errorBudgetConsumed := 0.0
	errorBudgetRemainingPct := 100.0

	if totalEvents > 0 && errorBudgetPct > 0 {
		actualErrorRate := float64(badEvents) / float64(totalEvents)
		if actualErrorRate >= errorBudgetPct {
			errorBudgetConsumed = 100.0
			errorBudgetRemainingPct = 0.0
		} else {
			errorBudgetConsumed = (actualErrorRate / errorBudgetPct) * 100.0
			errorBudgetRemainingPct = 100.0 - errorBudgetConsumed
		}
	}

	snap := &model.SLOBudgetSnapshot{
		SLOID:                 def.ID,
		WindowStart:           windowStart,
		WindowEnd:             windowEnd,
		TotalEvents:           totalEvents,
		BadEvents:             badEvents,
		ErrorBudgetConsumed:   errorBudgetConsumed,
		ErrorBudgetRemainingPct: errorBudgetRemainingPct,
		CurrentMeasurement:    currentMeasurement,
		Grain:                 "5min",
		CalculatedAt:          time.Now(),
	}

	if err := e.store.WriteSLOBudgetSnapshot(ctx, snap); err != nil {
		return fmt.Errorf("write budget snapshot: %w", err)
	}

	if err := e.evaluateBurnRate(ctx, def, snap); err != nil {
		logrus.Errorf("SLO engine: burn rate evaluation failed for SLO %d: %v", def.ID, err)
	}

	return nil
}

func (e *Engine) evaluateBurnRate(ctx context.Context, def *model.SLODefinition, currentSnap *model.SLOBudgetSnapshot) error {
	allBreached := true
	var highestBurnRate float64
	var triggeredRule *model.BurnRateRule

	for i := range def.BurnRateRules {
		rule := &def.BurnRateRules[i]
		windowDuration := time.Duration(rule.WindowMinutes) * time.Minute
		windowStart := time.Now().Add(-windowDuration)
		windowEnd := time.Now()

		var totalEvents, badEvents int64

		switch def.TargetType {
		case "availability":
			total, errs, err := e.store.GetSLOSpanCounts(ctx, def.ServiceName, windowStart, windowEnd)
			if err != nil {
				continue
			}
			totalEvents = total
			badEvents = errs
		case "latency":
			threshold := float64(200)
			if def.LatencyThresholdMs != nil {
				threshold = *def.LatencyThresholdMs
			}
			total, slow, err := e.store.GetSLOSlowSpanCounts(ctx, def.ServiceName, threshold, windowStart, windowEnd)
			if err != nil {
				continue
			}
			totalEvents = total
			badEvents = slow
		case "throughput":
			actualQPS, err := e.store.GetSLOServiceQPS(ctx, def.ServiceName, windowStart, windowEnd)
			if err != nil {
				continue
			}
			targetQPS := float64(100)
			if def.TargetQPS != nil {
				targetQPS = *def.TargetQPS
			}
			measurementIntervals := int64(windowDuration / time.Minute)
			totalEvents = measurementIntervals
			if actualQPS < targetQPS {
				badEvents = measurementIntervals
			}
		}

		if totalEvents == 0 {
			allBreached = false
			continue
		}

		actualErrorRate := float64(badEvents) / float64(totalEvents)
		errorBudgetPct := 1.0 - def.TargetValue
		if errorBudgetPct <= 0 {
			allBreached = false
			continue
		}

		windowDurationHours := float64(rule.WindowMinutes) / 60.0
		totalWindowHours := e.getWindowDuration(def.WindowType).Hours()
		if totalWindowHours <= 0 {
			allBreached = false
			continue
		}

		allowedRate := errorBudgetPct / totalWindowHours
		actualRate := actualErrorRate / windowDurationHours

		if allowedRate == 0 {
			allBreached = false
			continue
		}

		burnRate := actualRate / allowedRate

		if burnRate < rule.Threshold {
			allBreached = false
		}

		if burnRate > highestBurnRate {
			highestBurnRate = burnRate
			triggeredRule = rule
		}
	}

	if allBreached && triggeredRule != nil && highestBurnRate > 0 {
		if def.AlertRuleID == nil {
			rule := &model.AlertRule{
				Name:            fmt.Sprintf("SLO燃烧率告警 - %s", def.Name),
				Type:            "threshold",
				Enabled:         true,
				Severity:        triggeredRule.Severity,
				ServiceName:     def.ServiceName,
				Metric:          "slo_burn_rate",
				Operator:        ">",
				Threshold:       triggeredRule.Threshold,
				DurationSeconds: triggeredRule.WindowMinutes * 60,
				CooldownSeconds: 300,
			}
			created, err := e.store.CreateAlertRule(ctx, rule)
			if err != nil {
				return fmt.Errorf("create alert rule: %w", err)
			}
			if err := e.store.UpdateSLOAlertRuleID(ctx, def.ID, created.ID); err != nil {
				logrus.Warnf("Failed to update SLO alert_rule_id: %v", err)
			}
		}

		event := &model.AlertEvent{
			RuleID:      0,
			RuleName:    fmt.Sprintf("SLO燃烧率告警 - %s", def.Name),
			Severity:    triggeredRule.Severity,
			ServiceName: def.ServiceName,
			MetricValue: highestBurnRate,
			Threshold:   triggeredRule.Threshold,
			Message:     fmt.Sprintf("SLO %s 燃烧率 %.1fx 超过阈值 %.1fx (%d分钟窗口)", def.Name, highestBurnRate, triggeredRule.Threshold, triggeredRule.WindowMinutes),
			FiredAt:     time.Now(),
		}
		if def.AlertRuleID != nil {
			event.RuleID = *def.AlertRuleID
		}
		createdEvent, err := e.store.CreateAlertEvent(ctx, event)
		if err != nil {
			return fmt.Errorf("create alert event: %w", err)
		}

		burnAlert := &model.SLOBurnRateAlert{
			SLOID:         def.ID,
			WindowMinutes: triggeredRule.WindowMinutes,
			BurnRate:      highestBurnRate,
			Threshold:     triggeredRule.Threshold,
			Severity:      triggeredRule.Severity,
			AlertEventID:  &createdEvent.ID,
			FiredAt:       time.Now(),
		}
		if _, err := e.store.CreateSLOBurnRateAlert(ctx, burnAlert); err != nil {
			return fmt.Errorf("create burn rate alert: %w", err)
		}

		logrus.Infof("SLO burn rate alert: %s %.1fx (threshold: %.1fx, window: %dm)",
			def.Name, highestBurnRate, triggeredRule.Threshold, triggeredRule.WindowMinutes)
	} else {
		if err := e.store.ResolveSLOBurnRateAlerts(ctx, def.ID); err != nil {
			logrus.Warnf("Failed to resolve burn rate alerts for SLO %d: %v", def.ID, err)
		}
	}

	return nil
}

func CalculateErrorBudgetAbsolute(targetValue float64, windowType string) (float64, string) {
	errorBudgetPct := 1.0 - targetValue
	if errorBudgetPct <= 0 {
		return 0, "分钟"
	}

	var windowMinutes float64
	switch windowType {
	case "rolling_7d":
		windowMinutes = 7 * 24 * 60
	case "rolling_30d":
		windowMinutes = 30 * 24 * 60
	case "calendar_month":
		windowMinutes = 30 * 24 * 60
	default:
		windowMinutes = 30 * 24 * 60
	}

	allowedMinutes := errorBudgetPct * windowMinutes

	if allowedMinutes >= 60 {
		return math.Round(allowedMinutes/60*100) / 100, "小时"
	}
	return math.Round(allowedMinutes*100) / 100, "分钟"
}

func GetSLOStatus(remainingBudgetPct float64) string {
	if remainingBudgetPct <= 0 {
		return "breached"
	}
	if remainingBudgetPct <= 20 {
		return "warning"
	}
	return "healthy"
}

func EstimateExhaustTime(remainingBudgetPct float64, currentSnap *model.SLOBudgetSnapshot, windowType string) *time.Time {
	if currentSnap == nil || remainingBudgetPct <= 0 {
		return nil
	}

	windowDuration := time.Duration(0)
	switch windowType {
	case "rolling_7d":
		windowDuration = 7 * 24 * time.Hour
	case "rolling_30d":
		windowDuration = 30 * 24 * time.Hour
	case "calendar_month":
		windowDuration = 30 * 24 * time.Hour
	}
	if windowDuration == 0 {
		return nil
	}

	consumedPct := 100.0 - remainingBudgetPct
	if consumedPct <= 0 {
		return nil
	}

	elapsed := time.Since(currentSnap.WindowStart)
	if elapsed <= 0 {
		return nil
	}

	consumedPerSecond := consumedPct / elapsed.Seconds()
	if consumedPerSecond <= 0 {
		return nil
	}

	remainingSeconds := remainingBudgetPct / consumedPerSecond
	exhaustAt := time.Now().Add(time.Duration(remainingSeconds) * time.Second)

	if exhaustAt.After(currentSnap.WindowStart.Add(windowDuration)) {
		return nil
	}

	return &exhaustAt
}
