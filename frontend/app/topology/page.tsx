'use client';

import { useEffect, useState } from 'react';
import { Activity, ArrowRight, Clock, XCircle } from 'lucide-react';
import TopologyGraph from '@/components/TopologyGraph';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { topologyApi, healthApi } from '@/lib/api';
import { formatDuration, formatNumber, formatPercent } from '@/lib/utils';
import type { TopologyGraph as TopologyGraphType, TopologyNode, ServiceDetail, HealthScore } from '@/types';

export default function TopologyPage() {
  const [topology, setTopology] = useState<TopologyGraphType | null>(null);
  const [selectedService, setSelectedService] = useState<string | null>(null);
  const [serviceDetail, setServiceDetail] = useState<ServiceDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [windowSize, setWindowSize] = useState<'5m' | '1h' | '24h'>('5m');
  const [healthScores, setHealthScores] = useState<Record<string, HealthScore>>({});

  const loadTopology = async () => {
    try {
      const [topoRes, healthRes] = await Promise.all([
        topologyApi.getGraph(windowSize),
        healthApi.getAllScores(),
      ]);
      setTopology(topoRes.data);
      setHealthScores(healthRes.data.data || {});
    } catch (error) {
      console.error('Failed to load topology:', error);
    } finally {
      setLoading(false);
    }
  };

  const loadServiceDetail = async (serviceName: string) => {
    try {
      const res = await topologyApi.getServiceDetail(serviceName, windowSize);
      setServiceDetail(res.data);
    } catch (error) {
      console.error('Failed to load service detail:', error);
    }
  };

  useEffect(() => {
    loadTopology();
    const interval = setInterval(loadTopology, 30000);
    return () => clearInterval(interval);
  }, [windowSize]);

  useEffect(() => {
    if (selectedService) {
      loadServiceDetail(selectedService);
    }
  }, [selectedService, windowSize]);

  const handleNodeClick = (node: TopologyNode) => {
    setSelectedService(node.name);
  };

  const closeDetail = () => {
    setSelectedService(null);
    setServiceDetail(null);
  };

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
        <h1 className="text-2xl font-bold text-gray-900 dark:text-white">服务拓扑</h1>
        <div className="flex items-center space-x-2">
          {(['5m', '1h', '24h'] as const).map((w) => (
            <button
              key={w}
              onClick={() => setWindowSize(w)}
              className={`px-3 py-1 text-sm rounded-md ${
                windowSize === w
                  ? 'bg-blue-600 text-white'
                  : 'bg-gray-100 text-gray-700 hover:bg-gray-200 dark:bg-gray-800 dark:text-gray-300'
              }`}
            >
              {w}
            </button>
          ))}
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-5 gap-4 mb-4">
        <Card>
          <CardContent className="p-4">
            <div className="flex items-center space-x-2">
              <div className="w-3 h-3 rounded-full bg-green-500"></div>
              <span className="text-sm text-gray-600 dark:text-gray-400">正常</span>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-4">
            <div className="flex items-center space-x-2">
              <div className="w-3 h-3 rounded-full bg-yellow-500"></div>
              <span className="text-sm text-gray-600 dark:text-gray-400">慢</span>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-4">
            <div className="flex items-center space-x-2">
              <div className="w-3 h-3 rounded-full bg-red-500"></div>
              <span className="text-sm text-gray-600 dark:text-gray-400">高错误率</span>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-4">
            <div className="flex items-center space-x-2">
              <div className="w-3 h-3 rounded-full bg-gray-400"></div>
              <span className="text-sm text-gray-600 dark:text-gray-400">不活跃</span>
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-4">
            <div className="flex items-center space-x-2">
              <div className="w-3 h-3 rounded-full bg-red-500 animate-pulse"></div>
              <span className="text-sm text-gray-600 dark:text-gray-400">健康度 &lt;60</span>
            </div>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardContent className="p-4">
          {topology && (
            <TopologyGraph
              data={topology}
              healthScores={healthScores}
              onNodeClick={handleNodeClick}
            />
          )}
          {!topology?.nodes.length && (
            <div className="h-96 flex items-center justify-center text-gray-500">
              暂无服务拓扑数据
            </div>
          )}
        </CardContent>
      </Card>

      {selectedService && serviceDetail && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
          <div className="bg-white dark:bg-gray-900 rounded-lg shadow-xl max-w-4xl w-full max-h-[80vh] overflow-hidden">
            <div className="p-4 border-b border-gray-200 dark:border-gray-700 flex justify-between items-center">
              <h2 className="text-xl font-bold text-gray-900 dark:text-white">
                {selectedService}
              </h2>
              <button
                onClick={closeDetail}
                className="p-1 hover:bg-gray-100 dark:hover:bg-gray-800 rounded"
              >
                <XCircle className="h-5 w-5" />
              </button>
            </div>
            <div className="p-4 overflow-y-auto max-h-[calc(80vh-60px)]">
              <div className="grid grid-cols-1 md:grid-cols-3 gap-4 mb-6">
                <Card>
                  <CardContent className="p-4">
                    <div className="flex items-center space-x-3">
                      <div className="p-2 bg-blue-100 dark:bg-blue-900/20 rounded-lg">
                        <Activity className="h-5 w-5 text-blue-600" />
                      </div>
                      <div>
                        <p className="text-sm text-gray-500 dark:text-gray-400">总QPS</p>
                        <p className="text-xl font-bold">{formatNumber(serviceDetail.total_qps)}</p>
                      </div>
                    </div>
                  </CardContent>
                </Card>
                <Card>
                  <CardContent className="p-4">
                    <div className="flex items-center space-x-3">
                      <div className="p-2 bg-green-100 dark:bg-green-900/20 rounded-lg">
                        <Clock className="h-5 w-5 text-green-600" />
                      </div>
                      <div>
                        <p className="text-sm text-gray-500 dark:text-gray-400">平均延迟</p>
                        <p className="text-xl font-bold">{formatDuration(serviceDetail.avg_latency)}</p>
                      </div>
                    </div>
                  </CardContent>
                </Card>
                <Card>
                  <CardContent className="p-4">
                    <div className="flex items-center space-x-3">
                      <div className="p-2 bg-red-100 dark:bg-red-900/20 rounded-lg">
                        <XCircle className="h-5 w-5 text-red-600" />
                      </div>
                      <div>
                        <p className="text-sm text-gray-500 dark:text-gray-400">错误率</p>
                        <p className="text-xl font-bold">{formatPercent(serviceDetail.error_rate)}</p>
                      </div>
                    </div>
                  </CardContent>
                </Card>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                <Card>
                  <CardHeader>
                    <CardTitle className="text-base">上游调用</CardTitle>
                  </CardHeader>
                  <CardContent>
                    <div className="space-y-3">
                      {serviceDetail.upstreams.length === 0 ? (
                        <p className="text-gray-500 text-sm">无上游服务</p>
                      ) : (
                        serviceDetail.upstreams.map((up, idx) => (
                          <div
                            key={idx}
                            className="flex items-center justify-between p-3 bg-gray-50 dark:bg-gray-800 rounded-lg"
                          >
                            <div className="flex items-center space-x-2">
                              <span className="font-medium">{up.service}</span>
                              <ArrowRight className="h-4 w-4 text-gray-400" />
                              <span className="text-gray-500">{selectedService}</span>
                            </div>
                            <div className="text-sm text-gray-500">
                              {formatNumber(up.call_count)} 次
                            </div>
                          </div>
                        ))
                      )}
                    </div>
                  </CardContent>
                </Card>

                <Card>
                  <CardHeader>
                    <CardTitle className="text-base">下游调用</CardTitle>
                  </CardHeader>
                  <CardContent>
                    <div className="space-y-3">
                      {serviceDetail.downstreams.length === 0 ? (
                        <p className="text-gray-500 text-sm">无下游服务</p>
                      ) : (
                        serviceDetail.downstreams.map((down, idx) => (
                          <div
                            key={idx}
                            className="flex items-center justify-between p-3 bg-gray-50 dark:bg-gray-800 rounded-lg"
                          >
                            <div className="flex items-center space-x-2">
                              <span className="text-gray-500">{selectedService}</span>
                              <ArrowRight className="h-4 w-4 text-gray-400" />
                              <span className="font-medium">{down.service}</span>
                            </div>
                            <div className="text-sm text-gray-500">
                              {formatNumber(down.call_count)} 次
                            </div>
                          </div>
                        ))
                      )}
                    </div>
                  </CardContent>
                </Card>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
