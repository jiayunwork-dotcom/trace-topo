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
