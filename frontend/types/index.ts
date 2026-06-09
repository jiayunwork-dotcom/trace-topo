export interface Span {
  trace_id: string;
  span_id: string;
  parent_span_id?: string;
  service_name: string;
  operation_name: string;
  start_time: string;
  end_time: string;
  duration_ms: number;
  status_code: number;
  attributes?: Record<string, unknown>;
  is_orphan?: boolean;
  created_at?: string;
}

export interface SpanTreeNode {
  Span: Span;
  children?: SpanTreeNode[];
  depth: number;
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

export interface ServiceDetails {
  incoming: TopologyEdge[];
  outgoing: TopologyEdge[];
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
