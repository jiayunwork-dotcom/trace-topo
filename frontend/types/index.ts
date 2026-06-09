export interface Span {
  trace_id: string;
  span_id: string;
  parent_span_id?: string;
  service_name: string;
  operation_name: string;
  start_time: number;
  end_time: number;
  duration_ms: number;
  status_code: number;
  attributes?: Record<string, unknown>;
  is_orphan?: boolean;
  is_critical_path?: boolean;
  created_at?: string;
}

export interface SpanTreeNode {
  trace_id: string;
  span_id: string;
  parent_span_id?: string;
  service_name: string;
  operation_name: string;
  start_time: number;
  duration_ms: number;
  status_code: number;
  attributes?: Record<string, unknown>;
  is_orphan?: boolean;
  is_critical_path?: boolean;
  children?: SpanTreeNode[];
}

export interface Trace {
  trace_id: string;
  spans: SpanTreeNode[];
  total_duration_ms: number;
  start_time: string;
  end_time: string;
  is_slow: boolean;
  is_anomaly: boolean;
  is_retry_storm: boolean;
  is_critical_path: boolean;
}

export interface TraceSummary {
  trace_id: string;
  root_service: string;
  root_operation: string;
  total_duration_ms: number;
  span_count: number;
  start_time: string;
  end_time: string;
  status_code: number;
  is_slow: boolean;
  is_anomaly: boolean;
  is_retry_storm: boolean;
}

export interface SearchRequest {
  service_name: string;
  operation_name: string;
  min_latency_ms: number;
  max_latency_ms: number;
  status: string;
  start_time: string;
  end_time: string;
  page: number;
  page_size: number;
}

export interface SearchResponse {
  data: TraceSummary[];
  total: number;
  limit: number;
  offset: number;
}

export interface TopologyNode {
  id: string;
  name: string;
  qps: number;
  status: 'healthy' | 'slow' | 'error' | 'inactive';
  is_active: boolean;
}

export interface TopologyEdge {
  source: string;
  target: string;
  call_count: number;
  avg_latency: number;
  p99_latency: number;
  error_rate: number;
  status: 'healthy' | 'slow' | 'error' | 'inactive';
  is_active: boolean;
}

export interface TopologyGraph {
  nodes: TopologyNode[];
  edges: TopologyEdge[];
}

export interface ServiceDetail {
  service_name: string;
  total_qps: number;
  avg_latency: number;
  error_rate: number;
  upstreams: ServiceEdge[];
  downstreams: ServiceEdge[];
}

export interface ServiceEdge {
  service: string;
  call_count: number;
  avg_latency: number;
  error_rate: number;
}

export interface Metrics {
  total_qps: number;
  avg_latency_ms: number;
  error_rate: number;
  active_services: number;
  timestamp: string;
}

export interface TrendPoint {
  timestamp: string;
  qps: number;
  avg_latency_ms: number;
  error_rate: number;
}

export interface SamplingConfig {
  head_sampling_rate: number;
  tail_normal_rate: number;
  tail_anomaly_rate: number;
}

export interface SearchFilters {
  service?: string;
  operation?: string;
  start_time?: string;
  end_time?: string;
  min_duration?: number;
  max_duration?: number;
  status?: number;
  only_anomaly?: boolean;
  limit?: number;
  offset?: number;
}

export const statusColorMap: Record<string, string> = {
  healthy: '#10b981',
  slow: '#f59e0b',
  error: '#ef4444',
  inactive: '#9ca3af',
};

export const statusLabelMap: Record<string, string> = {
  healthy: '正常',
  slow: '慢',
  error: '错误',
  inactive: '不活跃',
};

export interface HealthScore {
  service_name: string;
  score: number;
  error_rate: number;
  error_rate_score: number;
  p99_deviation: number;
  p99_deviation_score: number;
  upstream_success_rate: number;
  upstream_success_rate_score: number;
  p99_baseline: number;
  calculated_at: string;
}

export interface AlertRule {
  id: number;
  name: string;
  description?: string;
  type: 'threshold' | 'spike' | 'topology';
  enabled: boolean;
  severity: 'info' | 'warning' | 'critical';
  service_name?: string;
  metric: string;
  operator: string;
  threshold: number;
  duration_seconds: number;
  spike_window_minutes: number;
  spike_multiplier: number;
  topology_check?: string;
  cooldown_seconds: number;
  last_triggered_at?: string;
  created_at: string;
  updated_at: string;
}

export interface AlertEvent {
  id: number;
  rule_id: number;
  rule_name: string;
  severity: string;
  service_name?: string;
  metric_value: number;
  threshold: number;
  message?: string;
  trace_ids?: string[];
  fired_at: string;
  resolved_at?: string;
  acknowledged: boolean;
}

export interface SpanDiff {
  service_name: string;
  operation_name: string;
  duration_a_ms: number;
  duration_b_ms: number;
  diff_ms: number;
  slower: 'a' | 'b' | 'same';
}

export interface SpanDiffEntry {
  service_name: string;
  operation_name: string;
  duration_ms: number;
  span_id: string;
}

export interface TraceComparison {
  trace_a: TraceSummary;
  trace_b: TraceSummary;
  span_diffs: SpanDiff[];
  only_in_a: SpanDiffEntry[];
  only_in_b: SpanDiffEntry[];
  duration_diff_ms: number;
}

export interface BurnRateRule {
  window_minutes: number;
  threshold: number;
  severity: 'warning' | 'critical';
}

export interface SLODefinition {
  id: number;
  name: string;
  service_name: string;
  target_type: 'availability' | 'latency' | 'throughput';
  target_value: number;
  window_type: 'rolling_7d' | 'rolling_30d' | 'calendar_month';
  budget_total: number;
  budget_unit: string;
  latency_threshold_ms?: number;
  target_qps?: number;
  burn_rate_rules: BurnRateRule[];
  alert_rule_id?: number;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface SLOBudgetSnapshot {
  id: number;
  slo_id: number;
  window_start: string;
  window_end: string;
  total_events: number;
  bad_events: number;
  error_budget_consumed: number;
  error_budget_remaining_pct: number;
  current_measurement: number;
  grain: string;
  calculated_at: string;
}

export interface SLOBurnRateAlert {
  id: number;
  slo_id: number;
  window_minutes: number;
  burn_rate: number;
  threshold: number;
  severity: 'warning' | 'critical';
  alert_event_id?: number;
  fired_at: string;
  resolved_at?: string;
}

export interface SLODetail {
  definition: SLODefinition;
  current_snapshot: SLOBudgetSnapshot | null;
  remaining_budget_pct: number;
  estimated_exhaust_at?: string;
  status: 'healthy' | 'warning' | 'breached';
}

export interface SLOOverview {
  id: number;
  name: string;
  service_name: string;
  target_type: 'availability' | 'latency' | 'throughput';
  target_value: number;
  window_type: 'rolling_7d' | 'rolling_30d' | 'calendar_month';
  remaining_budget_pct: number;
  status: 'healthy' | 'warning' | 'breached';
}

export interface SLOBudgetTrendPoint {
  timestamp: string;
  error_budget_remaining_pct: number;
  ideal_budget_remaining_pct: number;
}

export interface BudgetPreviewResult {
  budget_absolute: number;
  budget_unit: string;
  description: string;
}
