package anomaly

import (
	"context"
	"sync"

	"github.com/sirupsen/logrus"

	"trace-topo/internal/model"
)

type P99Querier interface {
	GetOperationP99(ctx context.Context, serviceName, operationName string) (float64, error)
}

type Detector struct {
	p99Querier P99Querier
	metricsMu  sync.Mutex
	slowCount  int64
	anomalyCount int64
	retryCount int64
}

func NewDetector(p99Querier P99Querier) *Detector {
	return &Detector{
		p99Querier: p99Querier,
	}
}

func (d *Detector) Detect(ctx context.Context, trace *model.Trace) error {
	if trace == nil || len(trace.Spans) == 0 {
		return nil
	}

	slow := d.detectSlowRequest(ctx, trace)
	anomaly := d.detectErrorSpans(trace)
	retryStorm := d.detectRetryStorm(trace)

	trace.IsSlow = slow
	trace.IsAnomaly = anomaly
	trace.IsRetryStorm = retryStorm

	if slow {
		d.metricsMu.Lock()
		d.slowCount++
		d.metricsMu.Unlock()
		logrus.Debugf("Trace %s marked as slow request", trace.TraceID)
	}

	if anomaly {
		d.metricsMu.Lock()
		d.anomalyCount++
		d.metricsMu.Unlock()
		logrus.Debugf("Trace %s marked as anomaly (too many errors)", trace.TraceID)
	}

	if retryStorm {
		d.metricsMu.Lock()
		d.retryCount++
		d.metricsMu.Unlock()
		logrus.Debugf("Trace %s marked as retry storm", trace.TraceID)
	}

	return nil
}

func (d *Detector) detectSlowRequest(ctx context.Context, trace *model.Trace) bool {
	rootOp := trace.RootOperation
	rootSvc := trace.RootService

	if rootOp == "" || rootSvc == "" {
		for _, span := range trace.Spans {
			if span.ParentSpanID == "" {
				rootOp = span.OperationName
				rootSvc = span.ServiceName
				break
			}
		}
	}

	if rootOp == "" || rootSvc == "" {
		return trace.TotalDuration > 5000
	}

	p99, err := d.p99Querier.GetOperationP99(ctx, rootSvc, rootOp)
	if err != nil {
		logrus.Warnf("Failed to get P99 for %s:%s: %v", rootSvc, rootOp, err)
		return trace.TotalDuration > 5000
	}

	if p99 == 0 {
		return trace.TotalDuration > 5000
	}

	return float64(trace.TotalDuration) > p99
}

func (d *Detector) detectErrorSpans(trace *model.Trace) bool {
	errorCount := 0
	for _, span := range trace.Spans {
		if span.StatusCode != 0 {
			errorCount++
			if errorCount > 2 {
				return true
			}
		}
	}
	return false
}

func (d *Detector) detectRetryStorm(trace *model.Trace) bool {
	operationCount := make(map[string]int)
	for _, span := range trace.Spans {
		key := span.ServiceName + ":" + span.OperationName
		operationCount[key]++
		if operationCount[key] > 3 {
			return true
		}
	}
	return false
}

func (d *Detector) GetMetrics() (slow, anomaly, retry int64) {
	d.metricsMu.Lock()
	defer d.metricsMu.Unlock()
	return d.slowCount, d.anomalyCount, d.retryCount
}

func (d *Detector) IsAnomaly(trace *model.Trace) bool {
	return trace.IsSlow || trace.IsAnomaly || trace.IsRetryStorm
}
