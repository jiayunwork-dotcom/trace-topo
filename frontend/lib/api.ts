import axios from 'axios';
import type {
  Span, SpanTreeNode, TraceSummary, SearchResponse, TopologyGraph, Metrics, TrendPoint, SamplingConfig, SearchFilters, ServiceDetail, Trace, SearchRequest, HealthScore, AlertRule, AlertEvent, TraceComparison, SLOOverview, SLODetail, SLOBudgetTrendPoint, SLOBurnRateAlert, BudgetPreviewResult } from '@/types';

const baseURL = process.env.NEXT_PUBLIC_API_BASE || '/api/v1';

const api = axios.create({
  baseURL,
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
});

export const traceApi = {
  searchTraces: async (filters: SearchRequest) => {
    const params = new URLSearchParams();
    Object.entries(filters).forEach(([key, value]) => {
      if (value !== undefined && value !== null && value !== '') {
        params.append(key, String(value));
      }
    });
    return api.get<SearchResponse>(`/traces?${params.toString()}`);
  },

  getTrace: async (traceId: string) => {
    return api.get<Trace>(`/traces/${traceId}`);
  },

  getTraceSpans: async (traceId: string, startTime?: string, endTime?: string) => {
    const params = new URLSearchParams();
    if (startTime) params.append('start_time', startTime);
    if (endTime) params.append('end_time', endTime);
    return api.get<{ spans: Span[]; tree: SpanTreeNode }>(`/traces/${traceId}/spans?${params.toString()}`);
  },

  getAnomalies: async (limit = 100) => {
    return api.get<SearchResponse>(`/anomalies?limit=${limit}`);
  },
};

export const topologyApi = {
  getGraph: async (window: '5m' | '1h' | '24h' = '5m') => {
    return api.get<TopologyGraph>(`/topology?window=${window}`);
  },

  getTopology: async (window: '5m' | '1h' | '24h' = '5m') => {
    return api.get<TopologyGraph>(`/topology?window=${window}`);
  },

  getServiceDetail: async (serviceName: string, window: '5m' | '1h' | '24h' = '5m') => {
    return api.get<ServiceDetail>(`/topology/services/${serviceName}?window=${window}`);
  },

  getServiceDetails: async (serviceName: string, window: '5m' | '1h' | '24h' = '5m') => {
    return api.get<ServiceDetail>(`/topology/services/${serviceName}?window=${window}`);
  },

  getServices: async () => {
    return api.get<{ data: string[] }>('/services');
  },

  getOperations: async (serviceName: string) => {
    return api.get<{ data: string[] }>(`/services/${serviceName}/operations`);
  },
};

export const metricsApi = {
  getRealtimeMetrics: async () => {
    return api.get<Metrics>('/metrics/realtime');
  },

  getTrend: async (duration: number = 1) => {
    return api.get<{ data: TrendPoint[]; duration: string }>(`/metrics/trend?duration=${duration}h`);
  },
};

export const samplingApi = {
  getConfig: async () => {
    return api.get<SamplingConfig>('/sampling');
  },

  updateConfig: async (config: SamplingConfig) => {
    return api.put<SamplingConfig>('/sampling', config);
  },

  getStats: async () => {
    return api.get<Record<string, unknown>>('/sampling/stats');
  },
};

export const internalApi = {
  getStats: async () => {
    return api.get<{ pending_traces: number; orphan_spans: number }>('/internal/stats');
  },
};

export const healthApi = {
  getAllScores: async () => {
    return api.get<{ data: Record<string, HealthScore> }>('/health/scores');
  },
  getServiceScores: async (serviceName: string, limit = 100) => {
    return api.get<{ data: HealthScore[] }>(`/health/scores/${serviceName}?limit=${limit}`);
  },
};

export const alertApi = {
  getRules: async () => {
    return api.get<{ data: AlertRule[] }>('/alerts/rules');
  },
  getRule: async (id: number) => {
    return api.get<AlertRule>(`/alerts/rules/${id}`);
  },
  createRule: async (rule: Partial<AlertRule>) => {
    return api.post<AlertRule>('/alerts/rules', rule);
  },
  updateRule: async (id: number, rule: Partial<AlertRule>) => {
    return api.put<AlertRule>(`/alerts/rules/${id}`, rule);
  },
  deleteRule: async (id: number) => {
    return api.delete(`/alerts/rules/${id}`);
  },
  getEvents: async (params?: { rule_id?: number; service?: string; limit?: number; offset?: number }) => {
    const searchParams = new URLSearchParams();
    if (params?.rule_id) searchParams.append('rule_id', String(params.rule_id));
    if (params?.service) searchParams.append('service', params.service);
    if (params?.limit) searchParams.append('limit', String(params.limit));
    if (params?.offset) searchParams.append('offset', String(params.offset));
    return api.get<{ data: AlertEvent[]; total: number; limit: number; offset: number }>(`/alerts/events?${searchParams.toString()}`);
  },
  getEvent: async (id: number) => {
    return api.get<AlertEvent>(`/alerts/events/${id}`);
  },
  acknowledgeEvent: async (id: number) => {
    return api.put(`/alerts/events/${id}/acknowledge`);
  },
};

export const compareApi = {
  compareTraces: async (traceA: string, traceB: string) => {
    return api.post<TraceComparison>('/traces/compare', { trace_a: traceA, trace_b: traceB });
  },
};

export const sloApi = {
  getSLOs: async () => {
    return api.get<{ data: SLOOverview[] }>('/slos');
  },
  getSLO: async (id: number) => {
    return api.get<SLODetail>(`/slos/${id}`);
  },
  createSLO: async (slo: Record<string, unknown>) => {
    return api.post('/slos', slo);
  },
  updateSLO: async (id: number, slo: Record<string, unknown>) => {
    return api.put(`/slos/${id}`, slo);
  },
  deleteSLO: async (id: number) => {
    return api.delete(`/slos/${id}`);
  },
  getTrend: async (id: number, grain: '5min' | 'hourly' | 'daily' = 'hourly', limit = 168) => {
    return api.get<{ data: SLOBudgetTrendPoint[]; grain: string }>(`/slos/${id}/trend?grain=${grain}&limit=${limit}`);
  },
  getBurnAlerts: async (id: number, limit = 50) => {
    return api.get<{ data: SLOBurnRateAlert[] }>(`/slos/${id}/burn-alerts?limit=${limit}`);
  },
  calculateBudgetPreview: async (targetValue: number, windowType: string) => {
    return api.post<BudgetPreviewResult>('/slos/calculate-budget', { target_value: targetValue, window_type: windowType });
  },
};

api.interceptors.response.use(
  (response) => response,
  (error) => {
    console.error('API Error:', error.response?.data || error.message);
    return Promise.reject(error);
  }
);

export default api;
