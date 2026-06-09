'use client';

import { Suspense, useEffect, useState } from 'react';
import { useSearchParams } from 'next/navigation';
import { ArrowLeftRight, ArrowUp, ArrowDown, Minus, Plus } from 'lucide-react';
import { format } from 'date-fns';
import FlameChart from '@/components/FlameChart';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { compareApi, traceApi } from '@/lib/api';
import { formatDuration } from '@/lib/utils';
import type { TraceComparison, TraceSummary, SpanTreeNode } from '@/types';

function CompareContent() {
  const searchParams = useSearchParams();
  const traceAParam = searchParams.get('trace_a') || '';
  const traceBParam = searchParams.get('trace_b') || '';

  const [traceAId, setTraceAId] = useState(traceAParam);
  const [traceBId, setTraceBId] = useState(traceBParam);
  const [comparison, setComparison] = useState<TraceComparison | null>(null);
  const [traceASpans, setTraceASpans] = useState<SpanTreeNode[]>([]);
  const [traceBSpans, setTraceBSpans] = useState<SpanTreeNode[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const loadComparison = async () => {
    if (!traceAId || !traceBId) {
      setError('请输入两条Trace ID');
      return;
    }

    setLoading(true);
    setError('');

    try {
      const [compRes, spansARes, spansBRes] = await Promise.all([
        compareApi.compareTraces(traceAId, traceBId),
        traceApi.getTraceSpans(traceAId),
        traceApi.getTraceSpans(traceBId),
      ]);

      setComparison(compRes.data);
      setTraceASpans(spansARes.data.tree ? [spansARes.data.tree] : []);
      setTraceBSpans(spansBRes.data.tree ? [spansBRes.data.tree] : []);
    } catch (err) {
      console.error('Failed to compare traces:', err);
      setError('对比失败，请检查Trace ID是否正确');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (traceAParam && traceBParam) {
      loadComparison();
    }
  }, []);

  const handleCompare = (e: React.FormEvent) => {
    e.preventDefault();
    loadComparison();
  };

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Trace对比分析</h1>

      <Card>
        <CardContent className="p-6">
          <form onSubmit={handleCompare} className="flex items-end space-x-4">
            <div className="flex-1">
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Trace A (基准)
              </label>
              <Input
                value={traceAId}
                onChange={(e) => setTraceAId(e.target.value)}
                placeholder="输入Trace ID"
                className="font-mono text-sm"
              />
            </div>
            <div className="flex-shrink-0 pb-2">
              <ArrowLeftRight className="h-5 w-5 text-gray-400" />
            </div>
            <div className="flex-1">
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                Trace B (对比)
              </label>
              <Input
                value={traceBId}
                onChange={(e) => setTraceBId(e.target.value)}
                placeholder="输入Trace ID"
                className="font-mono text-sm"
              />
            </div>
            <Button type="submit" disabled={loading}>
              {loading ? '对比中...' : '开始对比'}
            </Button>
          </form>
        </CardContent>
      </Card>

      {error && (
        <div className="p-4 bg-red-50 dark:bg-red-900/20 text-red-600 dark:text-red-400 rounded-lg">
          {error}
        </div>
      )}

      {comparison && (
        <>
          <div className="grid grid-cols-2 gap-4">
            <Card>
              <CardContent className="p-4">
                <div className="flex items-center justify-between mb-2">
                  <span className="text-sm text-gray-500">Trace A (基准)</span>
                  <span className="text-xs font-mono text-gray-400">{traceAId.slice(0, 16)}...</span>
                </div>
                <div className="grid grid-cols-2 gap-2 text-sm">
                  <div>
                    <span className="text-gray-500">服务: </span>
                    <span className="font-medium">{comparison.trace_a.root_service}</span>
                  </div>
                  <div>
                    <span className="text-gray-500">耗时: </span>
                    <span className="font-medium">{formatDuration(comparison.trace_a.total_duration_ms)}</span>
                  </div>
                  <div>
                    <span className="text-gray-500">操作: </span>
                    <span className="font-medium">{comparison.trace_a.root_operation}</span>
                  </div>
                  <div>
                    <span className="text-gray-500">Span数: </span>
                    <span className="font-medium">{comparison.trace_a.span_count}</span>
                  </div>
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardContent className="p-4">
                <div className="flex items-center justify-between mb-2">
                  <span className="text-sm text-gray-500">Trace B (对比)</span>
                  <span className="text-xs font-mono text-gray-400">{traceBId.slice(0, 16)}...</span>
                </div>
                <div className="grid grid-cols-2 gap-2 text-sm">
                  <div>
                    <span className="text-gray-500">服务: </span>
                    <span className="font-medium">{comparison.trace_b.root_service}</span>
                  </div>
                  <div>
                    <span className="text-gray-500">耗时: </span>
                    <span className={`font-medium ${comparison.duration_diff_ms > 0 ? 'text-red-600' : comparison.duration_diff_ms < 0 ? 'text-green-600' : ''}`}>
                      {formatDuration(comparison.trace_b.total_duration_ms)}
                      {comparison.duration_diff_ms !== 0 && (
                        <span className="ml-1 text-xs">
                          ({comparison.duration_diff_ms > 0 ? '+' : ''}{formatDuration(comparison.duration_diff_ms)})
                        </span>
                      )}
                    </span>
                  </div>
                  <div>
                    <span className="text-gray-500">操作: </span>
                    <span className="font-medium">{comparison.trace_b.root_operation}</span>
                  </div>
                  <div>
                    <span className="text-gray-500">Span数: </span>
                    <span className="font-medium">{comparison.trace_b.span_count}</span>
                  </div>
                </div>
              </CardContent>
            </Card>
          </div>

          <div className="grid grid-cols-2 gap-4">
            <Card>
              <CardHeader>
                <CardTitle className="text-sm">Trace A 火焰图</CardTitle>
              </CardHeader>
              <CardContent>
                <FlameChart spans={traceASpans} />
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle className="text-sm">Trace B 火焰图</CardTitle>
              </CardHeader>
              <CardContent>
                <FlameChart spans={traceBSpans} />
              </CardContent>
            </Card>
          </div>

          <Card>
            <CardHeader>
              <CardTitle className="flex items-center">
                <ArrowLeftRight className="h-5 w-5 mr-2" />
                耗时差异对比
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-gray-200 dark:border-gray-700">
                      <th className="text-left py-3 px-4 font-medium text-gray-500">服务</th>
                      <th className="text-left py-3 px-4 font-medium text-gray-500">操作</th>
                      <th className="text-right py-3 px-4 font-medium text-gray-500">Trace A</th>
                      <th className="text-right py-3 px-4 font-medium text-gray-500">Trace B</th>
                      <th className="text-right py-3 px-4 font-medium text-gray-500">差异</th>
                      <th className="text-center py-3 px-4 font-medium text-gray-500">趋势</th>
                    </tr>
                  </thead>
                  <tbody>
                    {comparison.span_diffs.map((diff, idx) => (
                      <tr key={idx} className="border-b border-gray-100 dark:border-gray-800">
                        <td className="py-3 px-4 font-medium">{diff.service_name}</td>
                        <td className="py-3 px-4">{diff.operation_name}</td>
                        <td className="py-3 px-4 text-right font-mono">{formatDuration(diff.duration_a_ms)}</td>
                        <td className="py-3 px-4 text-right font-mono">{formatDuration(diff.duration_b_ms)}</td>
                        <td className={`py-3 px-4 text-right font-mono font-medium ${
                          diff.diff_ms > 0 ? 'text-red-600' : diff.diff_ms < 0 ? 'text-green-600' : 'text-gray-500'
                        }`}>
                          {diff.diff_ms > 0 ? '+' : ''}{formatDuration(diff.diff_ms)}
                        </td>
                        <td className="py-3 px-4 text-center">
                          {diff.slower === 'b' && (
                            <span className="inline-flex items-center text-red-600">
                              <ArrowUp className="h-4 w-4" />
                              <span className="text-xs ml-1">变慢</span>
                            </span>
                          )}
                          {diff.slower === 'a' && (
                            <span className="inline-flex items-center text-green-600">
                              <ArrowDown className="h-4 w-4" />
                              <span className="text-xs ml-1">变快</span>
                            </span>
                          )}
                          {diff.slower === 'same' && (
                            <span className="inline-flex items-center text-gray-400">
                              <Minus className="h-4 w-4" />
                            </span>
                          )}
                        </td>
                      </tr>
                    ))}
                    {comparison.span_diffs.length === 0 && (
                      <tr>
                        <td colSpan={6} className="py-8 text-center text-gray-500">
                          无共同Span节点
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </CardContent>
          </Card>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <Card>
              <CardHeader>
                <CardTitle className="text-sm flex items-center text-red-600">
                  <ArrowUp className="h-4 w-4 mr-2" />
                  仅在Trace A中存在 (已消失)
                </CardTitle>
              </CardHeader>
              <CardContent>
                {comparison.only_in_a.length > 0 ? (
                  <div className="space-y-2">
                    {comparison.only_in_a.map((entry, idx) => (
                      <div key={idx} className="flex items-center justify-between p-2 bg-red-50 dark:bg-red-900/10 rounded">
                        <span className="text-sm">
                          <span className="font-medium">{entry.service_name}</span>: {entry.operation_name}
                        </span>
                        <span className="text-sm text-red-600 font-mono">{formatDuration(entry.duration_ms)}</span>
                      </div>
                    ))}
                  </div>
                ) : (
                  <p className="text-gray-500 text-sm">无差异</p>
                )}
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle className="text-sm flex items-center text-green-600">
                  <Plus className="h-4 w-4 mr-2" />
                  仅在Trace B中存在 (新增)
                </CardTitle>
              </CardHeader>
              <CardContent>
                {comparison.only_in_b.length > 0 ? (
                  <div className="space-y-2">
                    {comparison.only_in_b.map((entry, idx) => (
                      <div key={idx} className="flex items-center justify-between p-2 bg-green-50 dark:bg-green-900/10 rounded">
                        <span className="text-sm">
                          <span className="font-medium">{entry.service_name}</span>: {entry.operation_name}
                        </span>
                        <span className="text-sm text-green-600 font-mono">{formatDuration(entry.duration_ms)}</span>
                      </div>
                    ))}
                  </div>
                ) : (
                  <p className="text-gray-500 text-sm">无差异</p>
                )}
              </CardContent>
            </Card>
          </div>

          <Card>
            <CardHeader>
              <CardTitle className="text-sm">差异摘要</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <div className="p-3 bg-gray-50 dark:bg-gray-800 rounded-lg text-center">
                  <p className="text-2xl font-bold">{comparison.span_diffs.length}</p>
                  <p className="text-xs text-gray-500 mt-1">共同节点</p>
                </div>
                <div className="p-3 bg-red-50 dark:bg-red-900/10 rounded-lg text-center">
                  <p className="text-2xl font-bold text-red-600">
                    {comparison.span_diffs.filter((d) => d.slower === 'b').length}
                  </p>
                  <p className="text-xs text-gray-500 mt-1">变慢节点</p>
                </div>
                <div className="p-3 bg-green-50 dark:bg-green-900/10 rounded-lg text-center">
                  <p className="text-2xl font-bold text-green-600">
                    {comparison.span_diffs.filter((d) => d.slower === 'a').length}
                  </p>
                  <p className="text-xs text-gray-500 mt-1">变快节点</p>
                </div>
                <div className="p-3 bg-blue-50 dark:bg-blue-900/10 rounded-lg text-center">
                  <p className={`text-2xl font-bold ${
                    comparison.duration_diff_ms > 0 ? 'text-red-600' : comparison.duration_diff_ms < 0 ? 'text-green-600' : 'text-gray-600'
                  }`}>
                    {comparison.duration_diff_ms > 0 ? '+' : ''}{formatDuration(comparison.duration_diff_ms)}
                  </p>
                  <p className="text-xs text-gray-500 mt-1">总耗时差异</p>
                </div>
              </div>
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}

export default function ComparePage() {
  return (
    <Suspense
      fallback={
        <div className="flex items-center justify-center h-64">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600"></div>
        </div>
      }
    >
      <CompareContent />
    </Suspense>
  );
}
