import axios from 'axios';
import type {
  Span, SpanTreeNode, TraceSummary, SearchResponse, TopologyGraph, TopologyEdge, Metrics, TrendPoint, SamplingConfig, SearchFilters, ServiceDetails } from '@/types';

const baseURL = process.env.NEXT_PUBLIC_API_BASE || '/api/v1';

const api = axios.create({
  baseURL,
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
});

export const traceApi = {
  searchTraces: async (filters: SearchFilters) => {
    const params = new URLSearchParams();
    Object.entries(filters).forEach(([key, value]) => {
      if (value !== undefined && value !== null && value !== '') {
        params.append(key, String(value));
      }
    });
    return api.get<SearchResponse>(`/traces?${params.toString()}`);
  },

  getTrace: async (traceId: string) => {
    return api.get<{ summary: TraceSummary }>(`/traces/${traceId}`);
  },

  getTraceSpans: async (traceId: string, startTime?: string, endTime?: string) => {
    const params = new URLSearchParams();
    if (startTime) params.append('start_time', startTime);
    if (endTime) params.append('end_time', endTime);
    return api.get<{ spans: Span[]; tree: SpanTreeNode }>(`/traces/${traceId}/spans?${params.toString()}`);
  },

  getAnomalies: async (limit = 100) => {
    return api.get<{ data: TraceSummary[] }>(`/anomalies?limit=${limit}`);
  },
};

export const topologyApi = {
  getTopology: async (window: '5m' | '1h' | '24h' = '5m') => {
    return api.get<TopologyGraph>(`/topology?window=${window}`);
  },

  getServiceDetails: async (serviceName: string, window: '5m' | '1h' | '24h' = '5m') => {
    return api.get<ServiceDetails>(`/topology/services/${serviceName}?window=${window}`);
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

api.interceptors.response.use(
  (response) => response,
  (error) => {
    console.error('API Error:', error.response?.data || error.message);
    return Promise.reject(error);
  }
);

export default api;
