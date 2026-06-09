package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"trace-topo/internal/config"
	"trace-topo/internal/model"
)

type Store interface {
	GetTraceSummary(ctx context.Context, traceID string) (*model.TraceSummary, error)
	GetTraceSpans(ctx context.Context, traceID string, startTime, endTime time.Time) ([]*model.Span, error)
	SearchTraces(ctx context.Context, req *model.SearchRequest) ([]*model.TraceSummary, int64, error)
	GetServices(ctx context.Context) ([]string, error)
	GetOperations(ctx context.Context, serviceName string) ([]string, error)
	GetAnomalyTraces(ctx context.Context, limit int) ([]*model.TraceSummary, error)
	GetMetricsTrend(ctx context.Context, duration time.Duration) ([]*model.TrendPoint, error)
	GetRealtimeMetrics(ctx context.Context) (*model.Metrics, error)
	GetSamplingConfig(ctx context.Context) (*model.SamplingConfig, error)
	UpdateSamplingConfig(ctx context.Context, headRate, tailNormalRate, tailAnomalyRate float64) error
}

type TopologyProvider interface {
	GetTopology(window string) *model.TopologyGraph
	GetServiceDetails(serviceName string, window string) (incoming, outgoing []*model.TopologyEdge)
}

type SamplingConfigurer interface {
	UpdateConfig(ctx context.Context, cfg *model.SamplingConfig) error
	GetConfig() *model.SamplingConfig
	GetStats() map[string]interface{}
}

type AssemblerStats interface {
	GetPendingTraceCount() int
	GetOrphanCount() int
}

type Handler struct {
	store       Store
	topology    TopologyProvider
	sampling    SamplingConfigurer
	assembler   AssemblerStats
}

func NewHandler(store Store, topology TopologyProvider, sampling SamplingConfigurer, assembler AssemblerStats) *Handler {
	return &Handler{
		store:     store,
		topology:  topology,
		sampling:  sampling,
		assembler: assembler,
	}
}

func (h *Handler) SetupRouter() *gin.Engine {
	r := gin.Default()

	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "*")
		c.Header("Access-Control-Expose-Headers", "Content-Length")
		c.Header("Access-Control-Max-Age", "43200")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})

	api := r.Group("/api/v1")
	{
		api.GET("/health", h.HealthCheck)

		traces := api.Group("/traces")
		{
			traces.GET("", h.SearchTraces)
			traces.GET("/:id", h.GetTrace)
			traces.GET("/:id/spans", h.GetTraceSpans)
		}

		api.GET("/anomalies", h.GetAnomalies)

		topology := api.Group("/topology")
		{
			topology.GET("", h.GetTopology)
			topology.GET("/services/:name", h.GetServiceDetails)
		}

		api.GET("/services", h.GetServices)
		api.GET("/services/:name/operations", h.GetOperations)

		metrics := api.Group("/metrics")
		{
			metrics.GET("/realtime", h.GetRealtimeMetrics)
			metrics.GET("/trend", h.GetMetricsTrend)
		}

		sampling := api.Group("/sampling")
		{
			sampling.GET("", h.GetSamplingConfig)
			sampling.PUT("", h.UpdateSamplingConfig)
			sampling.GET("/stats", h.GetSamplingStats)
		}

		api.GET("/internal/stats", h.GetInternalStats)
	}

	return r
}

func (h *Handler) Start(ctx context.Context) error {
	r := h.SetupRouter()
	addr := fmt.Sprintf(":%d", config.AppConfig.HTTPPort)

	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		logrus.Infof("HTTP API server starting on port %d", config.AppConfig.HTTPPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logrus.Errorf("HTTP server error: %v", err)
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logrus.Errorf("HTTP server shutdown error: %v", err)
		}
		logrus.Info("HTTP server stopped gracefully")
	}()

	return nil
}

func (h *Handler) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"time":   time.Now().UTC(),
	})
}

func (h *Handler) SearchTraces(c *gin.Context) {
	req := &model.SearchRequest{}

	if svc := c.Query("service"); svc != "" {
		req.ServiceName = svc
	}
	if op := c.Query("operation"); op != "" {
		req.OperationName = op
	}
	if minDur := c.Query("min_duration"); minDur != "" {
		if d, err := strconv.ParseInt(minDur, 10, 64); err == nil {
			req.MinDuration = d
		}
	}
	if maxDur := c.Query("max_duration"); maxDur != "" {
		if d, err := strconv.ParseInt(maxDur, 10, 64); err == nil {
			req.MaxDuration = d
		}
	}
	if status := c.Query("status"); status != "" {
		if s, err := strconv.ParseInt(status, 10, 32); err == nil {
			code := int32(s)
			req.StatusCode = &code
		}
	}
	if startTime := c.Query("start_time"); startTime != "" {
		if t, err := time.Parse(time.RFC3339, startTime); err == nil {
			req.StartTime = t
		}
	}
	if endTime := c.Query("end_time"); endTime != "" {
		if t, err := time.Parse(time.RFC3339, endTime); err == nil {
			req.EndTime = t
		}
	}
	if onlyAnomaly := c.Query("only_anomaly"); onlyAnomaly == "true" {
		req.OnlyAnomaly = true
	}
	if limit := c.Query("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil {
			req.Limit = l
		}
	}
	if offset := c.Query("offset"); offset != "" {
		if o, err := strconv.Atoi(offset); err == nil {
			req.Offset = o
		}
	}

	if req.StartTime.IsZero() {
		req.StartTime = time.Now().Add(-1 * time.Hour)
	}
	if req.EndTime.IsZero() {
		req.EndTime = time.Now()
	}

	results, total, err := h.store.SearchTraces(c.Request.Context(), req)
	if err != nil {
		logrus.Errorf("Search traces error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  results,
		"total": total,
		"limit": req.Limit,
		"offset": req.Offset,
	})
}

func (h *Handler) GetTrace(c *gin.Context) {
	traceID := c.Param("id")
	if traceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "trace id is required"})
		return
	}

	summary, err := h.store.GetTraceSummary(c.Request.Context(), traceID)
	if err != nil {
		logrus.Errorf("Get trace summary error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"summary": summary,
	})
}

func (h *Handler) GetTraceSpans(c *gin.Context) {
	traceID := c.Param("id")
	if traceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "trace id is required"})
		return
	}

	var startTime, endTime time.Time
	if s := c.Query("start_time"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			startTime = t
		}
	}
	if e := c.Query("end_time"); e != "" {
		if t, err := time.Parse(time.RFC3339, e); err == nil {
			endTime = t
		}
	}

	if startTime.IsZero() {
		startTime = time.Now().Add(-24 * time.Hour)
	}
	if endTime.IsZero() {
		endTime = time.Now()
	}

	spans, err := h.store.GetTraceSpans(c.Request.Context(), traceID, startTime, endTime)
	if err != nil {
		logrus.Errorf("Get trace spans error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	spanTree := buildSpanTree(spans)

	c.JSON(http.StatusOK, gin.H{
		"spans": spans,
		"tree":  spanTree,
	})
}

func buildSpanTree(spans []*model.Span) *model.SpanTreeNode {
	if len(spans) == 0 {
		return nil
	}

	spanMap := make(map[string]*model.SpanTreeNode)
	childrenMap := make(map[string][]*model.Span)

	for _, span := range spans {
		spanMap[span.SpanID] = &model.SpanTreeNode{Span: span}
		if span.ParentSpanID != "" {
			childrenMap[span.ParentSpanID] = append(childrenMap[span.ParentSpanID], span)
		}
	}

	var root *model.SpanTreeNode
	for _, span := range spans {
		if span.ParentSpanID == "" {
			root = spanMap[span.SpanID]
			break
		}
	}

	if root == nil {
		for _, span := range spans {
			if _, ok := spanMap[span.ParentSpanID]; !ok {
				root = spanMap[span.SpanID]
				break
			}
		}
	}

	if root == nil && len(spans) > 0 {
		root = spanMap[spans[0].SpanID]
	}

	if root != nil {
		buildChildren(root, spanMap, childrenMap, 0)
	}

	return root
}

func buildChildren(node *model.SpanTreeNode, spanMap map[string]*model.SpanTreeNode, childrenMap map[string][]*model.Span, depth int) {
	node.Depth = depth
	if children, ok := childrenMap[node.SpanID]; ok {
		for _, child := range children {
			if childNode, ok := spanMap[child.SpanID]; ok {
				node.Children = append(node.Children, childNode)
				buildChildren(childNode, spanMap, childrenMap, depth+1)
			}
		}
	}
}

func (h *Handler) GetAnomalies(c *gin.Context) {
	limit := 100
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			limit = v
		}
	}

	results, err := h.store.GetAnomalyTraces(c.Request.Context(), limit)
	if err != nil {
		logrus.Errorf("Get anomaly traces error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": results,
	})
}

func (h *Handler) GetTopology(c *gin.Context) {
	window := c.DefaultQuery("window", "5m")

	graph := h.topology.GetTopology(window)
	c.JSON(http.StatusOK, graph)
}

func (h *Handler) GetServiceDetails(c *gin.Context) {
	serviceName := c.Param("name")
	window := c.DefaultQuery("window", "5m")

	incoming, outgoing := h.topology.GetServiceDetails(serviceName, window)
	c.JSON(http.StatusOK, gin.H{
		"incoming": incoming,
		"outgoing": outgoing,
	})
}

func (h *Handler) GetServices(c *gin.Context) {
	services, err := h.store.GetServices(c.Request.Context())
	if err != nil {
		logrus.Errorf("Get services error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": services,
	})
}

func (h *Handler) GetOperations(c *gin.Context) {
	serviceName := c.Param("name")
	if serviceName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "service name is required"})
		return
	}

	operations, err := h.store.GetOperations(c.Request.Context(), serviceName)
	if err != nil {
		logrus.Errorf("Get operations error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": operations,
	})
}

func (h *Handler) GetRealtimeMetrics(c *gin.Context) {
	metrics, err := h.store.GetRealtimeMetrics(c.Request.Context())
	if err != nil {
		logrus.Errorf("Get realtime metrics error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, metrics)
}

func (h *Handler) GetMetricsTrend(c *gin.Context) {
	duration := 1 * time.Hour
	if d := c.Query("duration"); d != "" {
		parts := strings.Split(d, "h")
		if len(parts) == 2 {
			if hours, err := strconv.Atoi(parts[0]); err == nil {
				duration = time.Duration(hours) * time.Hour
			}
		}
	}

	points, err := h.store.GetMetricsTrend(c.Request.Context(), duration)
	if err != nil {
		logrus.Errorf("Get metrics trend error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":     points,
		"duration": duration.String(),
	})
}

func (h *Handler) GetSamplingConfig(c *gin.Context) {
	cfg := h.sampling.GetConfig()
	c.JSON(http.StatusOK, cfg)
}

func (h *Handler) UpdateSamplingConfig(c *gin.Context) {
	var cfg model.SamplingConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.sampling.UpdateConfig(c.Request.Context(), &cfg); err != nil {
		logrus.Errorf("Update sampling config error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, cfg)
}

func (h *Handler) GetSamplingStats(c *gin.Context) {
	stats := h.sampling.GetStats()
	c.JSON(http.StatusOK, stats)
}

func (h *Handler) GetInternalStats(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"pending_traces": h.assembler.GetPendingTraceCount(),
		"orphan_spans":   h.assembler.GetOrphanCount(),
	})
}
