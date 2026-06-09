'use client';

import { useEffect, useState } from 'react';
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from 'recharts';
import { Activity, AlertTriangle, Clock, Server } from 'lucide-react';
import { format } from 'date-fns';

import MetricCard from '@/components/MetricCard';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { metricsApi, traceApi } from '@/lib/api';
import { formatDuration, formatNumber, formatPercent } from '@/lib/utils';
import type { Metrics, TrendPoint, TraceSummary } from '@/types';

export default function DashboardPage() {
  const [metrics, setMetrics] = useState<Metrics | null>(null);
  const [trendData, setTrendData] = useState<TrendPoint[]>([]);
  const [anomalies, setAnomalies] = useState<TraceSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [trendDuration, setTrendDuration] = useState(1);

  const loadData = async () => {
    try {
      const [metricsRes, trendRes, anomaliesRes] = await Promise.all([
        metricsApi.getRealtimeMetrics(),
        metricsApi.getTrend(trendDuration),
        traceApi.getAnomalies(10),
      ]);
      setMetrics(metricsRes.data);
      setTrendData(trendRes.data.data);
      setAnomalies(anomaliesRes.data.data);
    } catch (error) {
      console.error('Failed to load dashboard data:', error);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadData();
    const interval = setInterval(loadData, 5000);
    return () => clearInterval(interval);
  }, [trendDuration]);

  const chartData = trendData.map((point) => ({
    time: format(new Date(point.timestamp), 'HH:mm'),
    qps: point.qps,
    latency: point.avg_latency_ms,
    errorRate: point.error_rate * 100,
  }));

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600"></div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <h1 className="text-2xl font-bold text-gray-900 dark:text-white">仪表盘</h1>
        <div className="flex items-center space-x-2">
          {[1, 6, 24].map((h) => (
            <button
              key={h}
              onClick={() => setTrendDuration(h)}
              className={`px-3 py-1 text-sm rounded-md ${
                trendDuration === h
                  ? 'bg-blue-600 text-white'
                  : 'bg-gray-100 text-gray-700 hover:bg-gray-200 dark:bg-gray-800 dark:text-gray-300'
              }`}
            >
              {h}h
            </button>
          ))}
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        <MetricCard
          title="总QPS"
          value={formatNumber(metrics?.total_qps || 0)}
          icon={<Activity className="h-6 w-6" />}
          color="blue"
        />
        <MetricCard
          title="平均延迟"
          value={formatDuration(metrics?.avg_latency_ms || 0)}
          icon={<Clock className="h-6 w-6" />}
          color="green"
        />
        <MetricCard
          title="错误率"
          value={formatPercent(metrics?.error_rate || 0)}
          icon={<AlertTriangle className="h-6 w-6" />}
          color={metrics && metrics.error_rate > 0.1 ? 'red' : 'green'}
        />
        <MetricCard
          title="活跃服务"
          value={metrics?.active_services || 0}
          icon={<Server className="h-6 w-6" />}
          color="yellow"
        />
      </div>

      <Card>
        <CardHeader>
          <CardTitle>最近趋势</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="h-80">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={chartData}>
                <CartesianGrid strokeDasharray="3 3" stroke="#e5e7eb" />
                <XAxis dataKey="time" stroke="#6b7280" />
                <YAxis yAxisId="left" stroke="#6b7280" />
                <YAxis yAxisId="right" orientation="right" stroke="#6b7280" />
                <Tooltip
                  contentStyle={{
                    backgroundColor: '#fff',
                    border: '1px solid #e5e7eb',
                    borderRadius: '8px',
                  }}
                />
                <Legend />
                <Line
                  yAxisId="left"
                  type="monotone"
                  dataKey="qps"
                  stroke="#3b82f6"
                  strokeWidth={2}
                  dot={false}
                  name="QPS"
                />
                <Line
                  yAxisId="left"
                  type="monotone"
                  dataKey="latency"
                  stroke="#10b981"
                  strokeWidth={2}
                  dot={false}
                  name="延迟(ms)"
                />
                <Line
                  yAxisId="right"
                  type="monotone"
                  dataKey="errorRate"
                  stroke="#ef4444"
                  strokeWidth={2}
                  dot={false}
                  name="错误率(%)"
                />
              </LineChart>
            </ResponsiveContainer>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center">
            <AlertTriangle className="h-5 w-5 text-yellow-500 mr-2" />
            最近异常Trace
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-gray-200 dark:border-gray-700">
                  <th className="text-left py-2 px-4 font-medium text-gray-500">Trace ID</th>
                  <th className="text-left py-2 px-4 font-medium text-gray-500">服务</th>
                  <th className="text-left py-2 px-4 font-medium text-gray-500">操作</th>
                  <th className="text-left py-2 px-4 font-medium text-gray-500">耗时</th>
                  <th className="text-left py-2 px-4 font-medium text-gray-500">状态</th>
                  <th className="text-left py-2 px-4 font-medium text-gray-500">时间</th>
                </tr>
              </thead>
              <tbody>
                {anomalies.map((trace) => (
                  <tr
                    key={trace.trace_id}
                    className="border-b border-gray-100 dark:border-gray-800 hover:bg-gray-50 dark:hover:bg-gray-800 cursor-pointer"
                    onClick={() => (window.location.href = `/trace/?id=${trace.trace_id}`)}
                  >
                    <td className="py-3 px-4 font-mono text-xs text-blue-600">
                      {trace.trace_id.slice(0, 16)}...
                    </td>
                    <td className="py-3 px-4">{trace.root_service}</td>
                    <td className="py-3 px-4">{trace.root_operation}</td>
                    <td className="py-3 px-4">{formatDuration(trace.total_duration_ms)}</td>
                    <td className="py-3 px-4">
                      <div className="flex space-x-1">
                        {trace.is_slow && (
                          <span className="px-2 py-0.5 text-xs rounded bg-yellow-100 text-yellow-700">慢</span>
                        )}
                        {trace.is_anomaly && (
                          <span className="px-2 py-0.5 text-xs rounded bg-red-100 text-red-700">错误</span>
                        )}
                        {trace.is_retry_storm && (
                          <span className="px-2 py-0.5 text-xs rounded bg-orange-100 text-orange-700">重试</span>
                        )}
                      </div>
                    </td>
                    <td className="py-3 px-4 text-gray-500">
                      {format(new Date(trace.start_time), 'MM-dd HH:mm:ss')}
                    </td>
                  </tr>
                ))}
                {anomalies.length === 0 && (
                  <tr>
                    <td colSpan={6} className="py-8 text-center text-gray-500">
                      暂无异常Trace
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
