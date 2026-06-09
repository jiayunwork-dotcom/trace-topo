package api

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"trace-topo/internal/config"
	"trace-topo/internal/model"
	"trace-topo/internal/slo"
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
	GetHealthScores(ctx context.Context, serviceName string, limit int) ([]*model.HealthScore, error)
	GetAllLatestHealthScores(ctx context.Context) (map[string]*model.HealthScore, error)
	GetAlertRules(ctx context.Context) ([]*model.AlertRule, error)
	GetAlertRule(ctx context.Context, id int) (*model.AlertRule, error)
	CreateAlertRule(ctx context.Context, rule *model.AlertRule) (*model.AlertRule, error)
	UpdateAlertRule(ctx context.Context, rule *model.AlertRule) (*model.AlertRule, error)
	DeleteAlertRule(ctx context.Context, id int) error
	GetAlertEvents(ctx context.Context, ruleID *int, serviceName string, limit, offset int) ([]*model.AlertEvent, int64, error)
	GetAlertEvent(ctx context.Context, id int) (*model.AlertEvent, error)
	AcknowledgeAlertEvent(ctx context.Context, id int) error
	GetSLODefinitions(ctx context.Context) ([]*model.SLODefinition, error)
	GetSLODefinition(ctx context.Context, id int) (*model.SLODefinition, error)
	CreateSLODefinition(ctx context.Context, def *model.SLODefinition) (*model.SLODefinition, error)
	UpdateSLODefinition(ctx context.Context, def *model.SLODefinition) (*model.SLODefinition, error)
	DeleteSLODefinition(ctx context.Context, id int) error
	GetSLOBudgetSnapshots(ctx context.Context, sloID int, grain string, limit int) ([]*model.SLOBudgetSnapshot, error)
	GetLatestSLOBudgetSnapshot(ctx context.Context, sloID int) (*model.SLOBudgetSnapshot, error)
	GetSLOBurnRateAlerts(ctx context.Context, sloID int, limit int) ([]*model.SLOBurnRateAlert, error)
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

type HealthProvider interface {
	GetHealthScore(serviceName string) *model.HealthScore
	GetAllHealthScores() map[string]*model.HealthScore
}

type Handler struct {
	store       Store
	topology    TopologyProvider
	sampling    SamplingConfigurer
	assembler   AssemblerStats
	health      HealthProvider
}

func NewHandler(store Store, topology TopologyProvider, sampling SamplingConfigurer, assembler AssemblerStats, health HealthProvider) *Handler {
	return &Handler{
		store:     store,
		topology:  topology,
		sampling:  sampling,
		assembler: assembler,
		health:    health,
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

		health := api.Group("/health")
		{
			health.GET("/scores", h.GetAllHealthScores)
			health.GET("/scores/:service", h.GetServiceHealthScores)
		}

		alerts := api.Group("/alerts")
		{
			alerts.GET("/rules", h.GetAlertRules)
			alerts.GET("/rules/:id", h.GetAlertRule)
			alerts.POST("/rules", h.CreateAlertRule)
			alerts.PUT("/rules/:id", h.UpdateAlertRule)
			alerts.DELETE("/rules/:id", h.DeleteAlertRule)
			alerts.GET("/events", h.GetAlertEvents)
			alerts.GET("/events/:id", h.GetAlertEvent)
			alerts.PUT("/events/:id/acknowledge", h.AcknowledgeAlertEvent)
		}

		slos := api.Group("/slos")
		{
			slos.GET("", h.GetSLOs)
			slos.GET("/:id", h.GetSLO)
			slos.POST("", h.CreateSLO)
			slos.PUT("/:id", h.UpdateSLO)
			slos.DELETE("/:id", h.DeleteSLO)
			slos.GET("/:id/trend", h.GetSLOTrend)
			slos.GET("/:id/burn-alerts", h.GetSLOBurnAlerts)
			slos.POST("/calculate-budget", h.CalculateBudgetPreview)
		}

		api.POST("/traces/compare", h.CompareTraces)

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

func (h *Handler) GetAllHealthScores(c *gin.Context) {
	scores := h.health.GetAllHealthScores()
	c.JSON(http.StatusOK, gin.H{
		"data": scores,
	})
}

func (h *Handler) GetServiceHealthScores(c *gin.Context) {
	serviceName := c.Param("service")
	limit := 100
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			limit = v
		}
	}

	scores, err := h.store.GetHealthScores(c.Request.Context(), serviceName, limit)
	if err != nil {
		logrus.Errorf("Get health scores error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": scores,
	})
}

func (h *Handler) GetAlertRules(c *gin.Context) {
	rules, err := h.store.GetAlertRules(c.Request.Context())
	if err != nil {
		logrus.Errorf("Get alert rules error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": rules,
	})
}

func (h *Handler) GetAlertRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid rule id"})
		return
	}

	rule, err := h.store.GetAlertRule(c.Request.Context(), id)
	if err != nil {
		logrus.Errorf("Get alert rule error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rule)
}

func (h *Handler) CreateAlertRule(c *gin.Context) {
	var rule model.AlertRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if rule.Type == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type is required"})
		return
	}

	if rule.Severity == "" {
		rule.Severity = "warning"
	}

	if rule.Operator == "" {
		rule.Operator = ">"
	}

	if rule.Metric == "" {
		rule.Metric = "error_rate"
	}

	created, err := h.store.CreateAlertRule(c.Request.Context(), &rule)
	if err != nil {
		logrus.Errorf("Create alert rule error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (h *Handler) UpdateAlertRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid rule id"})
		return
	}

	var rule model.AlertRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rule.ID = id

	if rule.Type == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type is required"})
		return
	}

	if rule.Severity == "" {
		rule.Severity = "warning"
	}

	if rule.Operator == "" {
		rule.Operator = ">"
	}

	if rule.Metric == "" {
		rule.Metric = "error_rate"
	}

	updated, err := h.store.UpdateAlertRule(c.Request.Context(), &rule)
	if err != nil {
		logrus.Errorf("Update alert rule error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (h *Handler) DeleteAlertRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid rule id"})
		return
	}

	if err := h.store.DeleteAlertRule(c.Request.Context(), id); err != nil {
		logrus.Errorf("Delete alert rule error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

func (h *Handler) GetAlertEvents(c *gin.Context) {
	var ruleID *int
	if rid := c.Query("rule_id"); rid != "" {
		if v, err := strconv.Atoi(rid); err == nil {
			ruleID = &v
		}
	}
	serviceName := c.Query("service")
	limit := 50
	offset := 0
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			limit = v
		}
	}
	if o := c.Query("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil {
			offset = v
		}
	}

	events, total, err := h.store.GetAlertEvents(c.Request.Context(), ruleID, serviceName, limit, offset)
	if err != nil {
		logrus.Errorf("Get alert events error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":   events,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *Handler) GetAlertEvent(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid event id"})
		return
	}

	event, err := h.store.GetAlertEvent(c.Request.Context(), id)
	if err != nil {
		logrus.Errorf("Get alert event error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, event)
}

func (h *Handler) AcknowledgeAlertEvent(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid event id"})
		return
	}

	if err := h.store.AcknowledgeAlertEvent(c.Request.Context(), id); err != nil {
		logrus.Errorf("Acknowledge alert event error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "acknowledged"})
}

func (h *Handler) CompareTraces(c *gin.Context) {
	var req struct {
		TraceIDA string `json:"trace_a"`
		TraceIDB string `json:"trace_b"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.TraceIDA == "" || req.TraceIDB == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "both trace_a and trace_b are required"})
		return
	}

	summaryA, err := h.store.GetTraceSummary(c.Request.Context(), req.TraceIDA)
	if err != nil {
		logrus.Errorf("Get trace A summary error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get trace A"})
		return
	}

	summaryB, err := h.store.GetTraceSummary(c.Request.Context(), req.TraceIDB)
	if err != nil {
		logrus.Errorf("Get trace B summary error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get trace B"})
		return
	}

	spansA, err := h.store.GetTraceSpans(c.Request.Context(), req.TraceIDA, summaryA.StartTime.Add(-1*time.Minute), summaryA.EndTime.Add(1*time.Minute))
	if err != nil {
		logrus.Errorf("Get trace A spans error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get trace A spans"})
		return
	}

	spansB, err := h.store.GetTraceSpans(c.Request.Context(), req.TraceIDB, summaryB.StartTime.Add(-1*time.Minute), summaryB.EndTime.Add(1*time.Minute))
	if err != nil {
		logrus.Errorf("Get trace B spans error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get trace B spans"})
		return
	}

	comparison := buildTraceComparison(summaryA, summaryB, spansA, spansB)
	c.JSON(http.StatusOK, comparison)
}

func buildTraceComparison(summaryA, summaryB *model.TraceSummary, spansA, spansB []*model.Span) *model.TraceComparison {
	type spanKey struct {
		service   string
		operation string
	}

	durationsA := make(map[spanKey][]int64)
	durationsB := make(map[spanKey][]int64)
	spanIDsA := make(map[spanKey]string)
	spanIDsB := make(map[spanKey]string)

	for _, s := range spansA {
		key := spanKey{service: s.ServiceName, operation: s.OperationName}
		durationsA[key] = append(durationsA[key], s.DurationMs)
		if _, exists := spanIDsA[key]; !exists {
			spanIDsA[key] = s.SpanID
		}
	}

	for _, s := range spansB {
		key := spanKey{service: s.ServiceName, operation: s.OperationName}
		durationsB[key] = append(durationsB[key], s.DurationMs)
		if _, exists := spanIDsB[key]; !exists {
			spanIDsB[key] = s.SpanID
		}
	}

	avgDuration := func(durations []int64) int64 {
		if len(durations) == 0 {
			return 0
		}
		var sum int64
		for _, d := range durations {
			sum += d
		}
		return sum / int64(len(durations))
	}

	allKeys := make(map[spanKey]bool)
	for k := range durationsA {
		allKeys[k] = true
	}
	for k := range durationsB {
		allKeys[k] = true
	}

	var spanDiffs []*model.SpanDiff
	var onlyInA []*model.SpanDiffEntry
	var onlyInB []*model.SpanDiffEntry

	for key := range allKeys {
		durA := avgDuration(durationsA[key])
		durB := avgDuration(durationsB[key])

		_, inA := durationsA[key]
		_, inB := durationsB[key]

		if inA && inB {
			diff := durB - durA
			slower := "same"
			if diff > 0 {
				slower = "b"
			} else if diff < 0 {
				slower = "a"
			}
			spanDiffs = append(spanDiffs, &model.SpanDiff{
				ServiceName:   key.service,
				OperationName: key.operation,
				DurationA:     durA,
				DurationB:     durB,
				DiffMs:        diff,
				Slower:        slower,
			})
		} else if inA {
			onlyInA = append(onlyInA, &model.SpanDiffEntry{
				ServiceName:   key.service,
				OperationName: key.operation,
				DurationMs:    durA,
				SpanID:        spanIDsA[key],
			})
		} else {
			onlyInB = append(onlyInB, &model.SpanDiffEntry{
				ServiceName:   key.service,
				OperationName: key.operation,
				DurationMs:    durB,
				SpanID:        spanIDsB[key],
			})
		}
	}

	return &model.TraceComparison{
		TraceA:       summaryA,
		TraceB:       summaryB,
		SpanDiffs:    spanDiffs,
		OnlyInA:      onlyInA,
		OnlyInB:      onlyInB,
		DurationDiff: summaryB.TotalDuration - summaryA.TotalDuration,
	}
}

func (h *Handler) GetSLOs(c *gin.Context) {
	defs, err := h.store.GetSLODefinitions(c.Request.Context())
	if err != nil {
		logrus.Errorf("Get SLO definitions error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var overviews []*model.SLOOverview
	for _, def := range defs {
		snap, _ := h.store.GetLatestSLOBudgetSnapshot(c.Request.Context(), def.ID)
		remainingPct := 100.0
		status := slo.GetSLOStatusNoData()
		if snap != nil {
			remainingPct = snap.ErrorBudgetRemainingPct
			if snap.TotalEvents > 0 {
				status = slo.GetSLOStatus(remainingPct)
			}
		}
		overviews = append(overviews, &model.SLOOverview{
			ID:                 def.ID,
			Name:               def.Name,
			ServiceName:        def.ServiceName,
			TargetType:         def.TargetType,
			TargetValue:        def.TargetValue,
			WindowType:         def.WindowType,
			RemainingBudgetPct: remainingPct,
			Status:             status,
		})
	}

	c.JSON(http.StatusOK, gin.H{"data": overviews})
}

func (h *Handler) GetSLO(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid SLO id"})
		return
	}

	def, err := h.store.GetSLODefinition(c.Request.Context(), id)
	if err != nil {
		logrus.Errorf("Get SLO definition error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	snap, _ := h.store.GetLatestSLOBudgetSnapshot(c.Request.Context(), id)
	remainingPct := 100.0
	status := slo.GetSLOStatusNoData()
	if snap != nil {
		remainingPct = snap.ErrorBudgetRemainingPct
		if snap.TotalEvents > 0 {
			status = slo.GetSLOStatus(remainingPct)
		}
	}
	exhaustAt := slo.EstimateExhaustTime(remainingPct, snap, def.WindowType)

	detail := &model.SLODetail{
		Definition:         def,
		CurrentSnapshot:    snap,
		RemainingBudgetPct: remainingPct,
		EstimatedExhaustAt: exhaustAt,
		Status:             status,
	}
	c.JSON(http.StatusOK, detail)
}

func (h *Handler) CreateSLO(c *gin.Context) {
	var def model.SLODefinition
	if err := c.ShouldBindJSON(&def); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if def.Name == "" || def.ServiceName == "" || def.TargetType == "" || def.WindowType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name, service_name, target_type, and window_type are required"})
		return
	}

	if def.TargetValue <= 0 || def.TargetValue > 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target_value must be between 0 and 1"})
		return
	}

	absValue, unit := slo.CalculateErrorBudgetAbsolute(def.TargetValue, def.WindowType)
	def.BudgetTotal = absValue
	def.BudgetUnit = unit

	if len(def.BurnRateRules) == 0 {
		def.BurnRateRules = []model.BurnRateRule{
			{WindowMinutes: 60, Threshold: 14.0, Severity: "critical"},
			{WindowMinutes: 360, Threshold: 6.0, Severity: "warning"},
		}
	}

	created, err := h.store.CreateSLODefinition(c.Request.Context(), &def)
	if err != nil {
		logrus.Errorf("Create SLO definition error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (h *Handler) UpdateSLO(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid SLO id"})
		return
	}

	var def model.SLODefinition
	if err := c.ShouldBindJSON(&def); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	def.ID = id

	if def.TargetValue > 0 && def.TargetValue <= 1 {
		absValue, unit := slo.CalculateErrorBudgetAbsolute(def.TargetValue, def.WindowType)
		def.BudgetTotal = absValue
		def.BudgetUnit = unit
	}

	updated, err := h.store.UpdateSLODefinition(c.Request.Context(), &def)
	if err != nil {
		logrus.Errorf("Update SLO definition error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (h *Handler) DeleteSLO(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid SLO id"})
		return
	}

	if err := h.store.DeleteSLODefinition(c.Request.Context(), id); err != nil {
		logrus.Errorf("Delete SLO definition error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

func (h *Handler) GetSLOTrend(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid SLO id"})
		return
	}

	grain := c.DefaultQuery("grain", "hourly")
	limit := 168
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			limit = v
		}
	}

	snaps, err := h.store.GetSLOBudgetSnapshots(c.Request.Context(), id, grain, limit)
	if err != nil {
		logrus.Errorf("Get SLO budget snapshots error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	def, err := h.store.GetSLODefinition(c.Request.Context(), id)
	if err != nil {
		logrus.Errorf("Get SLO definition for trend error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var windowDuration time.Duration
	switch def.WindowType {
	case "rolling_7d":
		windowDuration = 7 * 24 * time.Hour
	case "rolling_30d":
		windowDuration = 30 * 24 * time.Hour
	case "calendar_month":
		windowDuration = 30 * 24 * time.Hour
	}

	var trend []*model.SLOBudgetTrendPoint
	for i := len(snaps) - 1; i >= 0; i-- {
		snap := snaps[i]
		elapsed := snap.CalculatedAt.Sub(snap.WindowStart)
		var idealPct float64
		if windowDuration > 0 && elapsed > 0 {
			idealPct = math.Max(0, 100.0*(1.0-elapsed.Seconds()/windowDuration.Seconds()))
		} else {
			idealPct = 100.0
		}

		trend = append(trend, &model.SLOBudgetTrendPoint{
			Timestamp:               snap.CalculatedAt,
			ErrorBudgetRemainingPct: snap.ErrorBudgetRemainingPct,
			IdealBudgetRemainingPct: idealPct,
		})
	}

	c.JSON(http.StatusOK, gin.H{"data": trend, "grain": grain})
}

func (h *Handler) GetSLOBurnAlerts(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid SLO id"})
		return
	}

	limit := 50
	if l := c.Query("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			limit = v
		}
	}

	alerts, err := h.store.GetSLOBurnRateAlerts(c.Request.Context(), id, limit)
	if err != nil {
		logrus.Errorf("Get SLO burn rate alerts error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": alerts})
}

func (h *Handler) CalculateBudgetPreview(c *gin.Context) {
	var req struct {
		TargetValue float64 `json:"target_value"`
		WindowType  string  `json:"window_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.TargetValue <= 0 || req.TargetValue > 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target_value must be between 0 and 1"})
		return
	}
	if req.WindowType == "" {
		req.WindowType = "rolling_30d"
	}

	absValue, unit := slo.CalculateErrorBudgetAbsolute(req.TargetValue, req.WindowType)

	c.JSON(http.StatusOK, gin.H{
		"budget_absolute": absValue,
		"budget_unit":     unit,
		"description":     fmt.Sprintf("允许 %.2f%s不可用", absValue, unit),
	})
}
