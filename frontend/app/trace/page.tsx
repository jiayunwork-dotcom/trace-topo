'use client';

import { Suspense, useEffect, useState } from 'react';
import { useRouter, useSearchParams } from 'next/navigation';
import { ArrowLeft, XCircle } from 'lucide-react';
import { format } from 'date-fns';
import FlameChart from '@/components/FlameChart';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { traceApi } from '@/lib/api';
import { formatDuration } from '@/lib/utils';
import type { Trace, SpanTreeNode } from '@/types';

function TraceDetailContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const traceId = searchParams.get('id') || '';

  const [trace, setTrace] = useState<Trace | null>(null);
  const [selectedSpan, setSelectedSpan] = useState<SpanTreeNode | null>(null);
  const [loading, setLoading] = useState(true);

  const loadTrace = async () => {
    if (!traceId) return;
    try {
      const res = await traceApi.getTrace(traceId);
      setTrace(res.data);
    } catch (error) {
      console.error('Failed to load trace:', error);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadTrace();
  }, [traceId]);

  const handleSpanClick = (span: SpanTreeNode) => {
    setSelectedSpan(span);
  };

  const closeSpanDetail = () => {
    setSelectedSpan(null);
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600"></div>
      </div>
    );
  }

  if (!trace) {
    return (
      <div className="text-center py-12">
        <p className="text-gray-500">未找到该Trace</p>
        <Button variant="outline" className="mt-4" onClick={() => router.back()}>
          返回
        </Button>
      </div>
    );
  }

  const flatSpans: SpanTreeNode[] = [];
  const flatten = (node: SpanTreeNode) => {
    flatSpans.push(node);
    node.children?.forEach(flatten);
  };
  trace.spans.forEach(flatten);

  const errorsCount = flatSpans.filter((s) => s.status_code === 2).length;
  const hasErrors = errorsCount > 2;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center space-x-4">
          <Button variant="ghost" size="sm" onClick={() => router.back()}>
            <ArrowLeft className="h-4 w-4 mr-2" />
            返回
          </Button>
          <div>
            <h1 className="text-2xl font-bold text-gray-900 dark:text-white">
              Trace详情
            </h1>
            <p className="text-sm text-gray-500 font-mono">{traceId}</p>
          </div>
        </div>
        <div className="flex space-x-2">
          {trace.is_slow && (
            <span className="px-3 py-1 text-sm rounded bg-yellow-100 text-yellow-700">慢请求</span>
          )}
          {hasErrors && (
            <span className="px-3 py-1 text-sm rounded bg-red-100 text-red-700">异常链路</span>
          )}
          {trace.is_retry_storm && (
            <span className="px-3 py-1 text-sm rounded bg-orange-100 text-orange-700">重试风暴</span>
          )}
          {trace.is_critical_path && (
            <span className="px-3 py-1 text-sm rounded bg-purple-100 text-purple-700">关键路径</span>
          )}
        </div>
      </div>

      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <Card>
          <CardContent className="p-4">
            <p className="text-sm text-gray-500 dark:text-gray-400">总耗时</p>
            <p className="text-xl font-bold mt-1">{formatDuration(trace.total_duration_ms)}</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-4">
            <p className="text-sm text-gray-500 dark:text-gray-400">Span数量</p>
            <p className="text-xl font-bold mt-1">{flatSpans.length}</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-4">
            <p className="text-sm text-gray-500 dark:text-gray-400">错误数</p>
            <p className="text-xl font-bold mt-1">{errorsCount}</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-4">
            <p className="text-sm text-gray-500 dark:text-gray-400">开始时间</p>
            <p className="text-sm font-bold mt-1">
              {format(new Date(trace.start_time), 'MM-dd HH:mm:ss')}
            </p>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>火焰图</CardTitle>
        </CardHeader>
        <CardContent>
          <FlameChart spans={trace.spans} onSpanClick={handleSpanClick} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Span列表</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-gray-200 dark:border-gray-700">
                  <th className="text-left py-2 px-3 font-medium text-gray-500">服务</th>
                  <th className="text-left py-2 px-3 font-medium text-gray-500">操作</th>
                  <th className="text-left py-2 px-3 font-medium text-gray-500">耗时</th>
                  <th className="text-left py-2 px-3 font-medium text-gray-500">状态</th>
                  <th className="text-left py-2 px-3 font-medium text-gray-500">Span ID</th>
                </tr>
              </thead>
              <tbody>
                {flatSpans.map((span) => (
                  <tr
                    key={span.span_id}
                    className="border-b border-gray-100 dark:border-gray-800 hover:bg-gray-50 dark:hover:bg-gray-800 cursor-pointer"
                    onClick={() => handleSpanClick(span)}
                  >
                    <td className="py-2 px-3">{span.service_name}</td>
                    <td className="py-2 px-3">{span.operation_name}</td>
                    <td className="py-2 px-3">{formatDuration(span.duration_ms)}</td>
                    <td className="py-2 px-3">
                      {span.status_code === 2 ? (
                        <span className="text-red-600">错误</span>
                      ) : span.status_code === 1 ? (
                        <span className="text-green-600">OK</span>
                      ) : (
                        <span className="text-gray-500">未设置</span>
                      )}
                    </td>
                    <td className="py-2 px-3 font-mono text-xs text-gray-500">
                      {span.span_id.slice(0, 12)}...
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>

      {selectedSpan && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
          <div className="bg-white dark:bg-gray-900 rounded-lg shadow-xl max-w-2xl w-full max-h-[80vh] overflow-hidden">
            <div className="p-4 border-b border-gray-200 dark:border-gray-700 flex justify-between items-center">
              <h2 className="text-xl font-bold text-gray-900 dark:text-white">
                {selectedSpan.service_name}: {selectedSpan.operation_name}
              </h2>
              <button
                onClick={closeSpanDetail}
                className="p-1 hover:bg-gray-100 dark:hover:bg-gray-800 rounded"
              >
                <XCircle className="h-5 w-5" />
              </button>
            </div>
            <div className="p-4 overflow-y-auto max-h-[calc(80vh-60px)]">
              <div className="grid grid-cols-2 gap-4 mb-4">
                <div>
                  <p className="text-sm text-gray-500">Span ID</p>
                  <p className="font-mono text-sm">{selectedSpan.span_id}</p>
                </div>
                {selectedSpan.parent_span_id && (
                  <div>
                    <p className="text-sm text-gray-500">Parent Span ID</p>
                    <p className="font-mono text-sm">{selectedSpan.parent_span_id}</p>
                  </div>
                )}
                <div>
                  <p className="text-sm text-gray-500">开始时间</p>
                  <p className="font-medium">{format(new Date(selectedSpan.start_time), 'MM-dd HH:mm:ss.SSS')}</p>
                </div>
                <div>
                  <p className="text-sm text-gray-500">耗时</p>
                  <p className="font-medium">{formatDuration(selectedSpan.duration_ms)}</p>
                </div>
                <div>
                  <p className="text-sm text-gray-500">状态码</p>
                  <p className="font-medium">
                    {selectedSpan.status_code === 2 ? '错误' : selectedSpan.status_code === 1 ? 'OK' : '未设置'}
                  </p>
                </div>
                {selectedSpan.is_critical_path && (
                  <div>
                    <p className="text-sm text-gray-500">关键路径</p>
                    <p className="font-medium text-purple-600">是</p>
                  </div>
                )}
              </div>

              {selectedSpan.attributes && Object.keys(selectedSpan.attributes).length > 0 && (
                <div>
                  <h3 className="font-semibold mb-2">属性标签</h3>
                  <div className="bg-gray-50 dark:bg-gray-800 rounded-lg p-3">
                    {Object.entries(selectedSpan.attributes).map(([key, value]) => (
                      <div key={key} className="flex justify-between py-1 text-sm">
                        <span className="text-gray-500">{key}</span>
                        <span className="font-mono">{String(value)}</span>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export default function TraceDetailPage() {
  return (
    <Suspense
      fallback={
        <div className="flex items-center justify-center h-64">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600"></div>
        </div>
      }
    >
      <TraceDetailContent />
    </Suspense>
  );
}
