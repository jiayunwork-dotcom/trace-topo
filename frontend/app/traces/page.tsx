'use client';

import { useEffect, useState } from 'react';
import { Search, ChevronLeft, ChevronRight } from 'lucide-react';
import { format } from 'date-fns';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { Select } from '@/components/ui/select';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { traceApi, topologyApi } from '@/lib/api';
import { formatDuration } from '@/lib/utils';
import type { TraceSummary, SearchRequest } from '@/types';

export default function TracesPage() {
  const [traces, setTraces] = useState<TraceSummary[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [services, setServices] = useState<string[]>([]);
  const [operations, setOperations] = useState<string[]>([]);

  const [filters, setFilters] = useState<SearchRequest>({
    service_name: '',
    operation_name: '',
    min_latency_ms: 0,
    max_latency_ms: 0,
    status: '',
    start_time: new Date(Date.now() - 3600000).toISOString(),
    end_time: new Date().toISOString(),
    page: 1,
    page_size: 20,
  });

  const loadServices = async () => {
    try {
      const res = await topologyApi.getServices();
      setServices(res.data.data);
    } catch (error) {
      console.error('Failed to load services:', error);
    }
  };

  const loadOperations = async (service: string) => {
    if (!service) {
      setOperations([]);
      return;
    }
    try {
      const res = await topologyApi.getOperations(service);
      setOperations(res.data.data);
    } catch (error) {
      console.error('Failed to load operations:', error);
    }
  };

  const searchTraces = async () => {
    setLoading(true);
    try {
      const res = await traceApi.searchTraces(filters);
      setTraces(res.data.data);
      setTotal(res.data.total);
    } catch (error) {
      console.error('Failed to search traces:', error);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadServices();
  }, []);

  useEffect(() => {
    loadOperations(filters.service_name);
  }, [filters.service_name]);

  useEffect(() => {
    searchTraces();
  }, [filters]);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setFilters((prev) => ({ ...prev, page: 1 }));
  };

  const totalPages = Math.ceil(total / filters.page_size);

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Trace搜索</h1>

      <Card>
        <CardContent className="p-6">
          <form onSubmit={handleSubmit} className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                服务
              </label>
              <Select
                value={filters.service_name}
                onChange={(e) => setFilters((prev) => ({ ...prev, service_name: e.target.value, operation_name: '' }))}
              >
                <option value="">全部服务</option>
                {services.map((s) => (
                  <option key={s} value={s}>{s}</option>
                ))}
              </Select>
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                操作
              </label>
              <Select
                value={filters.operation_name}
                onChange={(e) => setFilters((prev) => ({ ...prev, operation_name: e.target.value }))}
              >
                <option value="">全部操作</option>
                {operations.map((o) => (
                  <option key={o} value={o}>{o}</option>
                ))}
              </Select>
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                最小延迟 (ms)
              </label>
              <Input
                type="number"
                value={filters.min_latency_ms || ''}
                onChange={(e) => setFilters((prev) => ({ ...prev, min_latency_ms: Number(e.target.value) || 0 }))}
                placeholder="0"
              />
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                状态
              </label>
              <Select
                value={filters.status}
                onChange={(e) => setFilters((prev) => ({ ...prev, status: e.target.value }))}
              >
                <option value="">全部状态</option>
                <option value="healthy">正常</option>
                <option value="slow">慢请求</option>
                <option value="error">错误</option>
              </Select>
            </div>

            <div className="lg:col-span-4 flex justify-end space-x-2">
              <Button
                type="button"
                variant="outline"
                onClick={() => setFilters({
                  service_name: '',
                  operation_name: '',
                  min_latency_ms: 0,
                  max_latency_ms: 0,
                  status: '',
                  start_time: new Date(Date.now() - 3600000).toISOString(),
                  end_time: new Date().toISOString(),
                  page: 1,
                  page_size: 20,
                })}
              >
                重置
              </Button>
              <Button type="submit">
                <Search className="h-4 w-4 mr-2" />
                搜索
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex justify-between items-center">
            <span>搜索结果</span>
            <span className="text-sm font-normal text-gray-500">共 {total} 条</span>
          </CardTitle>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="flex items-center justify-center h-64">
              <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600"></div>
            </div>
          ) : (
            <>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-gray-200 dark:border-gray-700">
                      <th className="text-left py-3 px-4 font-medium text-gray-500">Trace ID</th>
                      <th className="text-left py-3 px-4 font-medium text-gray-500">根服务</th>
                      <th className="text-left py-3 px-4 font-medium text-gray-500">根操作</th>
                      <th className="text-left py-3 px-4 font-medium text-gray-500">总耗时</th>
                      <th className="text-left py-3 px-4 font-medium text-gray-500">Span数量</th>
                      <th className="text-left py-3 px-4 font-medium text-gray-500">状态</th>
                      <th className="text-left py-3 px-4 font-medium text-gray-500">时间</th>
                    </tr>
                  </thead>
                  <tbody>
                    {traces.map((trace) => (
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
                        <td className="py-3 px-4">{trace.span_count}</td>
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
                            {!trace.is_slow && !trace.is_anomaly && !trace.is_retry_storm && (
                              <span className="px-2 py-0.5 text-xs rounded bg-green-100 text-green-700">正常</span>
                            )}
                          </div>
                        </td>
                        <td className="py-3 px-4 text-gray-500">
                          {format(new Date(trace.start_time), 'MM-dd HH:mm:ss')}
                        </td>
                      </tr>
                    ))}
                    {traces.length === 0 && (
                      <tr>
                        <td colSpan={7} className="py-12 text-center text-gray-500">
                          暂无Trace数据
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>

              {totalPages > 1 && (
                <div className="flex justify-center items-center space-x-2 mt-6">
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={filters.page === 1}
                    onClick={() => setFilters((prev) => ({ ...prev, page: prev.page - 1 }))}
                  >
                    <ChevronLeft className="h-4 w-4" />
                  </Button>
                  <span className="text-sm text-gray-600 dark:text-gray-400">
                    {filters.page} / {totalPages}
                  </span>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={filters.page === totalPages}
                    onClick={() => setFilters((prev) => ({ ...prev, page: prev.page + 1 }))}
                  >
                    <ChevronRight className="h-4 w-4" />
                  </Button>
                </div>
              )}
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
