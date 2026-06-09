package model

import (
	"encoding/json"
	"time"
)

type Span struct {
	TraceID       string                 `json:"trace_id"`
	SpanID        string                 `json:"span_id"`
	ParentSpanID  string                 `json:"parent_span_id,omitempty"`
	ServiceName   string                 `json:"service_name"`
	OperationName string                 `json:"operation_name"`
	StartTime     time.Time              `json:"start_time"`
	EndTime       time.Time              `json:"end_time"`
	DurationMs    int64                  `json:"duration_ms"`
	StatusCode    int32                  `json:"status_code"`
	Attributes    map[string]interface{} `json:"attributes,omitempty"`
	IsOrphan      bool                   `json:"is_orphan,omitempty"`
	CreatedAt     time.Time              `json:"created_at,omitempty"`
}

type SpanTreeNode struct {
	*Span
	Children []*SpanTreeNode `json:"children,omitempty"`
	Depth    int             `json:"depth"`
}

type Trace struct {
	TraceID        string           `json:"trace_id"`
	RootService    string           `json:"root_service,omitempty"`
	RootOperation  string           `json:"root_operation,omitempty"`
	Spans          []*Span          `json:"spans"`
	SpanTree       *SpanTreeNode    `json:"span_tree,omitempty"`
	TotalDuration  int64            `json:"total_duration_ms"`
	SpanCount      int              `json:"span_count"`
	StartTime      time.Time        `json:"start_time"`
	EndTime        time.Time        `json:"end_time"`
	StatusCode     int32            `json:"status_code"`
	IsSlow         bool             `json:"is_slow,omitempty"`
	IsAnomaly      bool             `json:"is_anomaly,omitempty"`
	IsRetryStorm   bool             `json:"is_retry_storm,omitempty"`
	CriticalPath   []string         `json:"critical_path,omitempty"`
	LastSpanTime   time.Time        `json:"-"`
	IsComplete     bool             `json:"-"`
}

type TraceSummary struct {
	TraceID       string    `json:"trace_id"`
	RootService   string    `json:"root_service"`
	RootOperation string    `json:"root_operation"`
	TotalDuration int64     `json:"total_duration_ms"`
	SpanCount     int       `json:"span_count"`
	StartTime     time.Time `json:"start_time"`
	EndTime       time.Time `json:"end_time"`
	StatusCode    int32     `json:"status_code"`
	IsSlow        bool      `json:"is_slow"`
	IsAnomaly     bool      `json:"is_anomaly"`
	IsRetryStorm  bool      `json:"is_retry_storm"`
}

type SearchRequest struct {
	ServiceName   string    `json:"service_name,omitempty"`
	OperationName string    `json:"operation_name,omitempty"`
	StartTime     time.Time `json:"start_time"`
	EndTime       time.Time `json:"end_time"`
	MinDuration   int64     `json:"min_duration_ms,omitempty"`
	MaxDuration   int64     `json:"max_duration_ms,omitempty"`
	StatusCode    *int32    `json:"status_code,omitempty"`
	Limit         int       `json:"limit,omitempty"`
	Offset        int       `json:"offset,omitempty"`
	OnlyAnomaly   bool      `json:"only_anomaly,omitempty"`
}

type TopologyNode struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	QPS      int64  `json:"qps"`
	Status   string `json:"status"`
	IsActive bool   `json:"is_active"`
}

type TopologyEdge struct {
	Source      string  `json:"source"`
	Target      string  `json:"target"`
	CallCount   int64   `json:"call_count"`
	AvgLatency  float64 `json:"avg_latency"`
	P99Latency  float64 `json:"p99_latency"`
	ErrorRate   float64 `json:"error_rate"`
	Status      string  `json:"status"`
	IsActive    bool    `json:"is_active"`
}

type TopologyGraph struct {
	Nodes []*TopologyNode `json:"nodes"`
	Edges []*TopologyEdge `json:"edges"`
}

type Metrics struct {
	TotalQPS        int64   `json:"total_qps"`
	AvgLatency      float64 `json:"avg_latency_ms"`
	ErrorRate       float64 `json:"error_rate"`
	ActiveServices  int     `json:"active_services"`
	Timestamp       time.Time `json:"timestamp"`
}

type TrendPoint struct {
	Timestamp     time.Time `json:"timestamp"`
	QPS           int64     `json:"qps"`
	AvgLatency    float64   `json:"avg_latency_ms"`
	ErrorRate     float64   `json:"error_rate"`
}

type SamplingConfig struct {
	HeadRate        float64 `json:"head_sampling_rate"`
	TailNormalRate  float64 `json:"tail_normal_rate"`
	TailAnomalyRate float64 `json:"tail_anomaly_rate"`
}

type HealthScore struct {
	ServiceName           string    `json:"service_name"`
	Score                 int       `json:"score"`
	ErrorRate             float64   `json:"error_rate"`
	ErrorRateScore        int       `json:"error_rate_score"`
	P99Deviation          float64   `json:"p99_deviation"`
	P99DeviationScore     int       `json:"p99_deviation_score"`
	UpstreamSuccessRate   float64   `json:"upstream_success_rate"`
	UpstreamSuccessScore  int       `json:"upstream_success_rate_score"`
	P99Baseline           float64   `json:"p99_baseline"`
	CalculatedAt          time.Time `json:"calculated_at"`
}

type AlertRule struct {
	ID               int       `json:"id"`
	Name             string    `json:"name"`
	Description      string    `json:"description,omitempty"`
	Type             string    `json:"type"`
	Enabled          bool      `json:"enabled"`
	Severity         string    `json:"severity"`
	ServiceName      string    `json:"service_name,omitempty"`
	Metric           string    `json:"metric"`
	Operator         string    `json:"operator"`
	Threshold        float64   `json:"threshold"`
	DurationSeconds  int       `json:"duration_seconds"`
	SpikeWindowMin   int       `json:"spike_window_minutes"`
	SpikeMultiplier  float64   `json:"spike_multiplier"`
	TopologyCheck    string    `json:"topology_check,omitempty"`
	CooldownSeconds  int       `json:"cooldown_seconds"`
	LastTriggeredAt  *time.Time `json:"last_triggered_at,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type AlertEvent struct {
	ID          int        `json:"id"`
	RuleID      int        `json:"rule_id"`
	RuleName    string     `json:"rule_name"`
	Severity    string     `json:"severity"`
	ServiceName string     `json:"service_name,omitempty"`
	MetricValue float64    `json:"metric_value"`
	Threshold   float64    `json:"threshold"`
	Message     string     `json:"message,omitempty"`
	TraceIDs    []string   `json:"trace_ids,omitempty"`
	FiredAt     time.Time  `json:"fired_at"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
	Acknowledged bool      `json:"acknowledged"`
}

type TraceComparison struct {
	TraceA       *TraceSummary     `json:"trace_a"`
	TraceB       *TraceSummary     `json:"trace_b"`
	SpanDiffs    []*SpanDiff       `json:"span_diffs"`
	OnlyInA      []*SpanDiffEntry  `json:"only_in_a"`
	OnlyInB      []*SpanDiffEntry  `json:"only_in_b"`
	DurationDiff int64             `json:"duration_diff_ms"`
}

type SpanDiff struct {
	ServiceName    string  `json:"service_name"`
	OperationName  string  `json:"operation_name"`
	DurationA      int64   `json:"duration_a_ms"`
	DurationB      int64   `json:"duration_b_ms"`
	DiffMs         int64   `json:"diff_ms"`
	Slower         string  `json:"slower"`
}

type SpanDiffEntry struct {
	ServiceName   string `json:"service_name"`
	OperationName string `json:"operation_name"`
	DurationMs    int64  `json:"duration_ms"`
	SpanID        string `json:"span_id"`
}

type BurnRateRule struct {
	WindowMinutes int     `json:"window_minutes"`
	Threshold     float64 `json:"threshold"`
	Severity      string  `json:"severity"`
}

type SLODefinition struct {
	ID               int             `json:"id"`
	Name             string          `json:"name"`
	ServiceName      string          `json:"service_name"`
	TargetType       string          `json:"target_type"`
	TargetValue      float64         `json:"target_value"`
	WindowType       string          `json:"window_type"`
	BudgetTotal      float64         `json:"budget_total"`
	BudgetUnit       string          `json:"budget_unit"`
	LatencyThresholdMs *float64      `json:"latency_threshold_ms,omitempty"`
	TargetQPS        *float64        `json:"target_qps,omitempty"`
	BurnRateRules    []BurnRateRule  `json:"burn_rate_rules"`
	AlertRuleID      *int            `json:"alert_rule_id,omitempty"`
	Enabled          bool            `json:"enabled"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type SLOBudgetSnapshot struct {
	ID                    int       `json:"id"`
	SLOID                 int       `json:"slo_id"`
	WindowStart           time.Time `json:"window_start"`
	WindowEnd             time.Time `json:"window_end"`
	TotalEvents           int64     `json:"total_events"`
	BadEvents             int64     `json:"bad_events"`
	ErrorBudgetConsumed   float64   `json:"error_budget_consumed"`
	ErrorBudgetRemainingPct float64 `json:"error_budget_remaining_pct"`
	CurrentMeasurement    float64   `json:"current_measurement"`
	Grain                 string    `json:"grain"`
	CalculatedAt          time.Time `json:"calculated_at"`
}

type SLOBurnRateAlert struct {
	ID            int        `json:"id"`
	SLOID         int        `json:"slo_id"`
	WindowMinutes int        `json:"window_minutes"`
	BurnRate      float64    `json:"burn_rate"`
	Threshold     float64    `json:"threshold"`
	Severity      string     `json:"severity"`
	AlertEventID  *int       `json:"alert_event_id,omitempty"`
	FiredAt       time.Time  `json:"fired_at"`
	ResolvedAt    *time.Time `json:"resolved_at,omitempty"`
}

type SLODetail struct {
	Definition          *SLODefinition        `json:"definition"`
	CurrentSnapshot     *SLOBudgetSnapshot    `json:"current_snapshot"`
	RemainingBudgetPct  float64               `json:"remaining_budget_pct"`
	EstimatedExhaustAt  *time.Time            `json:"estimated_exhaust_at,omitempty"`
	Status              string                `json:"status"`
}

type SLOOverview struct {
	ID                  int      `json:"id"`
	Name                string   `json:"name"`
	ServiceName         string   `json:"service_name"`
	TargetType          string   `json:"target_type"`
	TargetValue         float64  `json:"target_value"`
	WindowType          string   `json:"window_type"`
	RemainingBudgetPct  float64  `json:"remaining_budget_pct"`
	Status              string   `json:"status"`
}

type SLOBudgetTrendPoint struct {
	Timestamp              time.Time `json:"timestamp"`
	ErrorBudgetRemainingPct float64  `json:"error_budget_remaining_pct"`
	IdealBudgetRemainingPct float64  `json:"ideal_budget_remaining_pct"`
}

type SLOComplianceReportDay struct {
	Date              string  `json:"date"`
	RemainingBudgetPct float64 `json:"remaining_budget_pct"`
	ConsumedPct       float64 `json:"consumed_pct"`
	BreachMinutes     float64 `json:"breach_minutes"`
	AvgMeasurement    float64 `json:"avg_measurement"`
	HasData           bool    `json:"has_data"`
}

type SLOComplianceReportSummary struct {
	AvgComplianceRate float64 `json:"avg_compliance_rate"`
	MaxDailyConsumed  float64 `json:"max_daily_consumed"`
	BreachDays        int     `json:"breach_days"`
	DataCoverage      float64 `json:"data_coverage"`
}

type SLOComplianceReport struct {
	Definition *SLODefinition            `json:"definition"`
	TimeStart  time.Time                 `json:"time_start"`
	TimeEnd    time.Time                 `json:"time_end"`
	Days       []*SLOComplianceReportDay `json:"days"`
	Summary    *SLOComplianceReportSummary `json:"summary"`
}

type SLOCompareSeries struct {
	SLOID   int                    `json:"slo_id"`
	SLOName string                 `json:"slo_name"`
	Points  []*SLOBudgetTrendPoint `json:"points"`
}

type SLOCompareMetrics struct {
	SLOID               int     `json:"slo_id"`
	SLOName             string  `json:"slo_name"`
	TargetValue         float64 `json:"target_value"`
	CurrentMeasurement  float64 `json:"current_measurement"`
	RemainingBudgetPct  float64 `json:"remaining_budget_pct"`
	BurnRate1h          float64 `json:"burn_rate_1h"`
}

type SLOCompareResult struct {
	Series  []*SLOCompareSeries  `json:"series"`
	Metrics []*SLOCompareMetrics `json:"metrics"`
}

func (s *Span) MarshalJSON() ([]byte, error) {
	type Alias Span
	return json.Marshal(&struct {
		*Alias
		StartTimeUnixNano int64 `json:"start_time_unix_nano"`
		EndTimeUnixNano   int64 `json:"end_time_unix_nano"`
	}{
		Alias:             (*Alias)(s),
		StartTimeUnixNano: s.StartTime.UnixNano(),
		EndTimeUnixNano:   s.EndTime.UnixNano(),
	})
}
