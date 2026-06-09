package trace

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	"trace-topo/internal/config"
	"trace-topo/internal/model"
)

type StorageWriter interface {
	WriteSpans(ctx context.Context, spans []*model.Span) error
	WriteTraceSummary(ctx context.Context, trace *model.Trace) error
}

type TopologyUpdater interface {
	UpdateFromSpan(ctx context.Context, span *model.Span)
}

type AnomalyDetector interface {
	Detect(ctx context.Context, trace *model.Trace) error
}

type SamplingEngine interface {
	ShouldKeepHead(traceID string) bool
	ShouldKeepTail(ctx context.Context, trace *model.Trace) bool
}

type Assembler struct {
	storage         StorageWriter
	topology        TopologyUpdater
	anomalyDetector AnomalyDetector
	sampling        SamplingEngine

	pendingTraces *expirable.LRU[string, *pendingTrace]
	orphanSpans   *expirable.LRU[string, []*model.Span]

	pendingMu    sync.Mutex
	orphanMu     sync.Mutex
	inflightMu   sync.Mutex
	inflightSize int64

	spanChan   chan *model.Span
	workerPool int
}

type pendingTrace struct {
	trace       *model.Trace
	spanMap     map[string]*model.Span
	childrenMap map[string][]*model.Span
	mu          sync.RWMutex
	lastUpdate  time.Time
}

func NewAssembler(
	storage StorageWriter,
	topology TopologyUpdater,
	anomalyDetector AnomalyDetector,
	sampling SamplingEngine,
) *Assembler {
	a := &Assembler{
		storage:         storage,
		topology:        topology,
		anomalyDetector: anomalyDetector,
		sampling:        sampling,
		workerPool:      8,
		spanChan:        make(chan *model.Span, 100000),
	}

	a.pendingTraces = expirable.NewLRU[string, *pendingTrace](
		10000,
		a.onTraceEvicted,
		config.AppConfig.TraceComplete+10*time.Second,
	)

	a.orphanSpans = expirable.NewLRU[string, []*model.Span](
		50000,
		a.onOrphanEvicted,
		config.AppConfig.OrphanTimeout,
	)

	return a
}

func (a *Assembler) Start(ctx context.Context) {
	g, ctx := errgroup.WithContext(ctx)

	for i := 0; i < a.workerPool; i++ {
		g.Go(func() error {
			a.worker(ctx)
			return nil
		})
	}

	go a.cleanupLoop(ctx)

	logrus.Info("Trace assembler started")
}

func (a *Assembler) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case span := <-a.spanChan:
			a.processSpan(ctx, span)
			a.decrementInflight()
		}
	}
}

func (a *Assembler) ProcessSpans(ctx context.Context, spans []*model.Span) error {
	for _, span := range spans {
		if !a.sampling.ShouldKeepHead(span.TraceID) {
			continue
		}

		a.incrementInflight()
		select {
		case a.spanChan <- span:
		default:
			a.decrementInflight()
			logrus.Warn("Span channel is full, dropping span")
		}
	}
	return nil
}

func (a *Assembler) ShouldBackpressure() bool {
	a.inflightMu.Lock()
	defer a.inflightMu.Unlock()
	return a.inflightSize > 50000
}

func (a *Assembler) incrementInflight() {
	a.inflightMu.Lock()
	defer a.inflightMu.Unlock()
	a.inflightSize++
}

func (a *Assembler) decrementInflight() {
	a.inflightMu.Lock()
	defer a.inflightMu.Unlock()
	a.inflightSize--
}

func (a *Assembler) processSpan(ctx context.Context, span *model.Span) {
	a.pendingMu.Lock()
	pt, exists := a.pendingTraces.Get(span.TraceID)
	if !exists {
		pt = &pendingTrace{
			trace: &model.Trace{
				TraceID:      span.TraceID,
				Spans:        make([]*model.Span, 0),
				LastSpanTime: time.Now(),
			},
			spanMap:     make(map[string]*model.Span),
			childrenMap: make(map[string][]*model.Span),
		}
		a.pendingTraces.Add(span.TraceID, pt)
	}
	a.pendingMu.Unlock()

	pt.mu.Lock()
	defer pt.mu.Unlock()

	if len(pt.trace.Spans) >= config.AppConfig.MaxSpansPerTrace {
		logrus.Warnf("Trace %s exceeds max span limit %d, dropping span %s",
			span.TraceID, config.AppConfig.MaxSpansPerTrace, span.SpanID)
		return
	}

	pt.spanMap[span.SpanID] = span
	pt.trace.Spans = append(pt.trace.Spans, span)
	pt.lastUpdate = time.Now()
	pt.trace.LastSpanTime = time.Now()

	if span.ParentSpanID != "" {
		if parent, ok := pt.spanMap[span.ParentSpanID]; ok {
			pt.childrenMap[span.ParentSpanID] = append(pt.childrenMap[span.ParentSpanID], span)
			a.updateTraceSummary(pt.trace, parent, span)
		} else {
			a.addOrphanSpan(span)
		}
	} else {
		pt.trace.RootService = span.ServiceName
		pt.trace.RootOperation = span.OperationName
		pt.trace.StartTime = span.StartTime
	}

	a.adoptOrphans(pt, span)

	if a.topology != nil {
		a.topology.UpdateFromSpan(ctx, span)
	}

	a.updateTraceTimeRange(pt.trace, span)

	if pt.trace.StatusCode == 0 && span.StatusCode != 0 {
		pt.trace.StatusCode = span.StatusCode
	}
}

func (a *Assembler) addOrphanSpan(span *model.Span) {
	a.orphanMu.Lock()
	defer a.orphanMu.Unlock()

	key := span.TraceID + ":" + span.ParentSpanID
	spans, _ := a.orphanSpans.Get(key)
	spans = append(spans, span)
	a.orphanSpans.Add(key, spans)
}

func (a *Assembler) adoptOrphans(pt *pendingTrace, parentSpan *model.Span) {
	a.orphanMu.Lock()
	defer a.orphanMu.Unlock()

	key := pt.trace.TraceID + ":" + parentSpan.SpanID
	if orphans, ok := a.orphanSpans.Get(key); ok {
		for _, orphan := range orphans {
			orphan.IsOrphan = false
			pt.childrenMap[parentSpan.SpanID] = append(pt.childrenMap[parentSpan.SpanID], orphan)
			a.updateTraceSummary(pt.trace, parentSpan, orphan)
		}
		a.orphanSpans.Remove(key)
		logrus.Debugf("Adopted %d orphan spans for parent %s", len(orphans), parentSpan.SpanID)
	}
}

func (a *Assembler) updateTraceSummary(trace *model.Trace, parent, child *model.Span) {
	if child.StartTime.Before(trace.StartTime) {
		trace.StartTime = child.StartTime
	}
	if child.EndTime.After(trace.EndTime) {
		trace.EndTime = child.EndTime
	}
}

func (a *Assembler) updateTraceTimeRange(trace *model.Trace, span *model.Span) {
	if trace.StartTime.IsZero() || span.StartTime.Before(trace.StartTime) {
		trace.StartTime = span.StartTime
	}
	if span.EndTime.After(trace.EndTime) {
		trace.EndTime = span.EndTime
	}
	trace.TotalDuration = trace.EndTime.Sub(trace.StartTime).Milliseconds()
	trace.SpanCount = len(trace.Spans)
}

func (a *Assembler) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.checkCompleteTraces(ctx)
			a.markOrphanSpans()
		}
	}
}

func (a *Assembler) checkCompleteTraces(ctx context.Context) {
	a.pendingMu.Lock()
	defer a.pendingMu.Unlock()

	keys := a.pendingTraces.Keys()
	completeThreshold := time.Now().Add(-config.AppConfig.TraceComplete)

	for _, key := range keys {
		if pt, ok := a.pendingTraces.Peek(key); ok {
			pt.mu.RLock()
			isComplete := pt.lastUpdate.Before(completeThreshold)
			pt.mu.RUnlock()

			if isComplete {
				a.finalizeTrace(ctx, key)
			}
		}
	}
}

func (a *Assembler) finalizeTrace(ctx context.Context, traceID string) {
	pt, exists := a.pendingTraces.Get(traceID)
	if !exists {
		return
	}

	pt.mu.Lock()
	defer pt.mu.Unlock()

	if pt.trace.IsComplete {
		return
	}

	if a.anomalyDetector != nil {
		if err := a.anomalyDetector.Detect(ctx, pt.trace); err != nil {
			logrus.Errorf("Anomaly detection failed for trace %s: %v", traceID, err)
		}
	}

	a.buildSpanTree(pt)
	a.findCriticalPath(pt.trace)

	if a.sampling != nil && !a.sampling.ShouldKeepTail(ctx, pt.trace) {
		logrus.Debugf("Trace %s dropped by tail sampling", traceID)
		a.pendingTraces.Remove(traceID)
		return
	}

	if err := a.storage.WriteTraceSummary(ctx, pt.trace); err != nil {
		logrus.Errorf("Failed to write trace summary %s: %v", traceID, err)
	}

	if err := a.storage.WriteSpans(ctx, pt.trace.Spans); err != nil {
		logrus.Errorf("Failed to write spans for trace %s: %v", traceID, err)
	}

	pt.trace.IsComplete = true
	a.pendingTraces.Remove(traceID)

	logrus.Debugf("Trace %s finalized with %d spans", traceID, len(pt.trace.Spans))
}

func (a *Assembler) buildSpanTree(pt *pendingTrace) {
	if len(pt.trace.Spans) == 0 {
		return
	}

	var root *model.Span
	for _, span := range pt.trace.Spans {
		if span.ParentSpanID == "" {
			root = span
			break
		}
	}

	if root == nil {
		for _, span := range pt.trace.Spans {
			if _, ok := pt.spanMap[span.ParentSpanID]; !ok {
				root = span
				break
			}
		}
	}

	if root == nil && len(pt.trace.Spans) > 0 {
		root = pt.trace.Spans[0]
	}

	if root != nil {
		pt.trace.SpanTree = a.buildSubTree(pt, root, 0)
	}
}

func (a *Assembler) buildSubTree(pt *pendingTrace, span *model.Span, depth int) *model.SpanTreeNode {
	node := &model.SpanTreeNode{
		Span:  span,
		Depth: depth,
	}

	if children, ok := pt.childrenMap[span.SpanID]; ok {
		sort.Slice(children, func(i, j int) bool {
			return children[i].StartTime.Before(children[j].StartTime)
		})

		for _, child := range children {
			node.Children = append(node.Children, a.buildSubTree(pt, child, depth+1))
		}
	}

	return node
}

func (a *Assembler) findCriticalPath(trace *model.Trace) {
	if trace.SpanTree == nil {
		return
	}

	var maxPath []string
	var maxDuration int64

	var dfs func(node *model.SpanTreeNode, path []string, duration int64)
	dfs = func(node *model.SpanTreeNode, path []string, duration int64) {
		currentPath := append(path, node.SpanID)
		currentDuration := duration + node.DurationMs

		if len(node.Children) == 0 {
			if currentDuration > maxDuration {
				maxDuration = currentDuration
				maxPath = make([]string, len(currentPath))
				copy(maxPath, currentPath)
			}
			return
		}

		for _, child := range node.Children {
			dfs(child, currentPath, currentDuration)
		}
	}

	dfs(trace.SpanTree, []string{}, 0)
	trace.CriticalPath = maxPath
}

func (a *Assembler) markOrphanSpans() {
	a.orphanMu.Lock()
	defer a.orphanMu.Unlock()

	keys := a.orphanSpans.Keys()
	orphanThreshold := time.Now().Add(-config.AppConfig.OrphanTimeout)

	for _, key := range keys {
		if spans, ok := a.orphanSpans.Peek(key); ok && len(spans) > 0 {
			firstSpan := spans[0]
			if firstSpan.CreatedAt.Before(orphanThreshold) {
				for _, span := range spans {
					span.IsOrphan = true
					logrus.Debugf("Marked span %s as orphan (parent %s not found)",
						span.SpanID, span.ParentSpanID)
				}
				a.orphanSpans.Remove(key)
			}
		}
	}
}

func (a *Assembler) onTraceEvicted(key string, pt *pendingTrace, reason expirable.EvictReason) {
	if !pt.trace.IsComplete {
		ctx := context.Background()
		a.finalizeTrace(ctx, key)
	}
}

func (a *Assembler) onOrphanEvicted(key string, spans []*model.Span, reason expirable.EvictReason) {
	for _, span := range spans {
		span.IsOrphan = true
	}
	logrus.Debugf("Evicted %d orphan spans", len(spans))
}

func (a *Assembler) GetPendingTraceCount() int {
	return a.pendingTraces.Len()
}

func (a *Assembler) GetOrphanCount() int {
	return a.orphanSpans.Len()
}
