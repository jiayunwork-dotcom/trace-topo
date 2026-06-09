package topology

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"trace-topo/internal/config"
	"trace-topo/internal/model"
)

type P99Provider interface {
	GetOperationP99(ctx context.Context, serviceName, operationName string) (float64, error)
	GetAllOperationP99(ctx context.Context) (map[string]float64, error)
}

type edgeKey struct {
	source string
	target string
}

type windowData struct {
	callCount    int64
	totalLatency int64
	errorCount   int64
	latencies    []int64
	lastUpdate   time.Time
}

type windowType string

const (
	Window5Min  windowType = "5m"
	Window1Hour windowType = "1h"
	Window24Hour windowType = "24h"
)

type Discoverer struct {
	p99Provider P99Provider

	mu            sync.RWMutex
	windows       map[windowType]map[edgeKey]*windowData
	serviceQPS    map[string]int64
	serviceLastSeen map[string]time.Time
	edgeLastSeen  map[edgeKey]time.Time

	spanChan chan *model.Span
}

func NewDiscoverer(p99Provider P99Provider) *Discoverer {
	d := &Discoverer{
		p99Provider:     p99Provider,
		windows:         make(map[windowType]map[edgeKey]*windowData),
		serviceQPS:      make(map[string]int64),
		serviceLastSeen: make(map[string]time.Time),
		edgeLastSeen:    make(map[edgeKey]time.Time),
		spanChan:        make(chan *model.Span, 100000),
	}

	d.windows[Window5Min] = make(map[edgeKey]*windowData)
	d.windows[Window1Hour] = make(map[edgeKey]*windowData)
	d.windows[Window24Hour] = make(map[edgeKey]*windowData)

	return d
}

func (d *Discoverer) Start(ctx context.Context) {
	go d.processLoop(ctx)
	go d.cleanupLoop(ctx)

	logrus.Info("Topology discoverer started")
}

func (d *Discoverer) processLoop(ctx context.Context) {
	ticker := time.NewTicker(config.AppConfig.WindowUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case span := <-d.spanChan:
			d.processSpan(span)
		case <-ticker.C:
			d.aggregateWindows()
		}
	}
}

func (d *Discoverer) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.cleanupOldData()
		}
	}
}

func (d *Discoverer) UpdateFromSpan(ctx context.Context, span *model.Span) {
	select {
	case d.spanChan <- span:
	default:
		logrus.Warn("Topology span channel is full, dropping span")
	}
}

func (d *Discoverer) processSpan(span *model.Span) {
	if span == nil || span.ParentSpanID == "" {
		return
	}

	sourceService := d.getSourceService(span)
	if sourceService == "" {
		return
	}

	key := edgeKey{
		source: sourceService,
		target: span.ServiceName,
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	for wt := range d.windows {
		if _, ok := d.windows[wt][key]; !ok {
			d.windows[wt][key] = &windowData{
				latencies: make([]int64, 0, 1000),
			}
		}

		wd := d.windows[wt][key]
		wd.callCount++
		wd.totalLatency += span.DurationMs
		wd.lastUpdate = time.Now()

		if len(wd.latencies) < 10000 {
			wd.latencies = append(wd.latencies, span.DurationMs)
		}

		if span.StatusCode != 0 {
			wd.errorCount++
		}
	}

	d.serviceQPS[span.ServiceName]++
	d.serviceQPS[sourceService]++
	d.serviceLastSeen[span.ServiceName] = time.Now()
	d.serviceLastSeen[sourceService] = time.Now()
	d.edgeLastSeen[key] = time.Now()
}

func (d *Discoverer) getSourceService(span *model.Span) string {
	if span.Attributes != nil {
		if caller, ok := span.Attributes["service.caller"]; ok {
			if s, ok := caller.(string); ok {
				return s
			}
		}
		if peer, ok := span.Attributes["peer.service"]; ok {
			if s, ok := peer.(string); ok {
				return s
			}
		}
	}
	return ""
}

func (d *Discoverer) aggregateWindows() {
	d.mu.Lock()
	defer d.mu.Unlock()

	cutoff5m := time.Now().Add(-5 * time.Minute)
	cutoff1h := time.Now().Add(-1 * time.Hour)
	cutoff24h := time.Now().Add(-24 * time.Hour)

	for key, wd := range d.windows[Window5Min] {
		if wd.lastUpdate.Before(cutoff5m) {
			delete(d.windows[Window5Min], key)
		}
	}

	for key, wd := range d.windows[Window1Hour] {
		if wd.lastUpdate.Before(cutoff1h) {
			delete(d.windows[Window1Hour], key)
		}
	}

	for key, wd := range d.windows[Window24Hour] {
		if wd.lastUpdate.Before(cutoff24h) {
			delete(d.windows[Window24Hour], key)
		}
	}
}

func (d *Discoverer) cleanupOldData() {
	d.mu.Lock()
	defer d.mu.Unlock()

	activeCutoff := time.Now().Add(-time.Duration(config.AppConfig.TopologyInactiveHours) * time.Hour)

	for svc, lastSeen := range d.serviceLastSeen {
		if lastSeen.Before(activeCutoff) {
			delete(d.serviceLastSeen, svc)
			delete(d.serviceQPS, svc)
		}
	}

	for key, lastSeen := range d.edgeLastSeen {
		if lastSeen.Before(activeCutoff) {
			delete(d.edgeLastSeen, key)
		}
	}
}

func (d *Discoverer) GetTopology(window string) *model.TopologyGraph {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var wt windowType
	switch window {
	case "1h":
		wt = Window1Hour
	case "24h":
		wt = Window24Hour
	default:
		wt = Window5Min
	}

	windowData := d.windows[wt]
	nodes := make(map[string]*model.TopologyNode)
	edges := make([]*model.TopologyEdge, 0, len(windowData))

	activeCutoff := time.Now().Add(-time.Duration(config.AppConfig.TopologyInactiveHours) * time.Hour)

	for key, wd := range windowData {
		if _, ok := nodes[key.source]; !ok {
			qps := d.calculateQPS(key.source, wt)
			nodes[key.source] = &model.TopologyNode{
				ID:       key.source,
				Name:     key.source,
				QPS:      qps,
				IsActive: d.isServiceActive(key.source, activeCutoff),
			}
		}
		if _, ok := nodes[key.target]; !ok {
			qps := d.calculateQPS(key.target, wt)
			nodes[key.target] = &model.TopologyNode{
				ID:       key.target,
				Name:     key.target,
				QPS:      qps,
				IsActive: d.isServiceActive(key.target, activeCutoff),
			}
		}

		edge := d.buildEdge(key, wd, activeCutoff)
		edges = append(edges, edge)
	}

	nodeList := make([]*model.TopologyNode, 0, len(nodes))
	for _, node := range nodes {
		node.Status = d.getNodeStatus(node, edges)
		nodeList = append(nodeList, node)
	}

	return &model.TopologyGraph{
		Nodes: nodeList,
		Edges: edges,
	}
}

func (d *Discoverer) calculateQPS(service string, wt windowType) int64 {
	windowSeconds := int64(300)
	if wt == Window1Hour {
		windowSeconds = 3600
	} else if wt == Window24Hour {
		windowSeconds = 86400
	}

	totalCalls := int64(0)
	for key, wd := range d.windows[wt] {
		if key.source == service || key.target == service {
			totalCalls += wd.callCount
		}
	}

	return totalCalls / windowSeconds
}

func (d *Discoverer) buildEdge(key edgeKey, wd *windowData, activeCutoff time.Time) *model.TopologyEdge {
	avgLatency := float64(0)
	if wd.callCount > 0 {
		avgLatency = float64(wd.totalLatency) / float64(wd.callCount)
	}

	errorRate := float64(0)
	if wd.callCount > 0 {
		errorRate = float64(wd.errorCount) / float64(wd.callCount)
	}

	p99 := d.calculateP99(wd.latencies)

	isActive := true
	if lastSeen, ok := d.edgeLastSeen[key]; ok {
		isActive = lastSeen.After(activeCutoff)
	}

	status := "healthy"
	if errorRate > 0.1 {
		status = "error"
	} else if p99 > 1000 {
		status = "slow"
	}

	if !isActive {
		status = "inactive"
	}

	return &model.TopologyEdge{
		Source:     key.source,
		Target:     key.target,
		CallCount:  wd.callCount,
		AvgLatency: avgLatency,
		P99Latency: p99,
		ErrorRate:  errorRate,
		Status:     status,
		IsActive:   isActive,
	}
}

func (d *Discoverer) calculateP99(latencies []int64) float64 {
	if len(latencies) == 0 {
		return 0
	}

	sorted := make([]int64, len(latencies))
	copy(sorted, latencies)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	idx := int(float64(len(sorted)) * 0.99)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}

	return float64(sorted[idx])
}

func (d *Discoverer) isServiceActive(service string, cutoff time.Time) bool {
	if lastSeen, ok := d.serviceLastSeen[service]; ok {
		return lastSeen.After(cutoff)
	}
	return false
}

func (d *Discoverer) getNodeStatus(node *model.TopologyNode, edges []*model.TopologyEdge) string {
	if !node.IsActive {
		return "inactive"
	}

	hasError := false
	hasSlow := false

	for _, edge := range edges {
		if edge.Source == node.ID || edge.Target == node.ID {
			if edge.Status == "error" {
				hasError = true
			} else if edge.Status == "slow" {
				hasSlow = true
			}
		}
	}

	if hasError {
		return "error"
	}
	if hasSlow {
		return "slow"
	}
	return "healthy"
}

func (d *Discoverer) GetServiceDetails(serviceName string, window string) (incoming, outgoing []*model.TopologyEdge) {
	graph := d.GetTopology(window)

	for _, edge := range graph.Edges {
		if edge.Target == serviceName {
			incoming = append(incoming, edge)
		}
		if edge.Source == serviceName {
			outgoing = append(outgoing, edge)
		}
	}

	return incoming, outgoing
}

func (d *Discoverer) PersistToDB(ctx context.Context) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	p99Cache, _ := d.p99Provider.GetAllOperationP99(ctx)

	for key := range d.windows[Window5Min] {
		opKey := key.source + ":" + key.target
		if p99, ok := p99Cache[opKey]; ok {
			d.p99Provider.(interface{ UpdateOperationP99(context.Context, string, string, float64) error }).
				UpdateOperationP99(ctx, key.source, key.target, p99)
		}
	}
}
