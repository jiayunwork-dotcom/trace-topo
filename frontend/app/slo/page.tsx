'use client';

import { useEffect, useState, useCallback } from 'react';
import { format } from 'date-fns';
import { Target, Plus, Trash2, Edit2, X, AlertTriangle, AlertCircle, CheckCircle, Clock, TrendingUp, GitCompare, Download } from 'lucide-react';
import { AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, ReferenceLine, LineChart, Line, Legend } from 'recharts';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select } from '@/components/ui/select';
import { sloApi, topologyApi } from '@/lib/api';
import type { SLOOverview, SLODetail, SLOBudgetTrendPoint, SLOBurnRateAlert, BudgetPreviewResult, BurnRateRule, SLOCompareResult } from '@/types';

const statusConfig: Record<string, { color: string; bgColor: string; icon: React.ReactNode; label: string }> = {
  healthy: { color: 'text-green-700', bgColor: 'bg-green-100', icon: <CheckCircle className="h-4 w-4" />, label: '健康' },
  warning: { color: 'text-yellow-700', bgColor: 'bg-yellow-100', icon: <AlertTriangle className="h-4 w-4" />, label: '警告' },
  breached: { color: 'text-red-700', bgColor: 'bg-red-100', icon: <AlertCircle className="h-4 w-4" />, label: '违约' },
  no_data: { color: 'text-gray-500', bgColor: 'bg-gray-100', icon: <Clock className="h-4 w-4" />, label: '暂无数据' },
};

const targetTypeLabels: Record<string, string> = {
  availability: '可用性',
  latency: '延迟',
  throughput: '吞吐量',
};

const windowTypeLabels: Record<string, string> = {
  rolling_7d: '滚动7天',
  rolling_30d: '滚动30天',
  calendar_month: '日历月',
};

const budgetUnitLabels: Record<string, string> = {
  minutes: '分钟',
  hours: '小时',
  分钟: '分钟',
  小时: '小时',
};

const compareColors = ['#3b82f6', '#10b981', '#f59e0b', '#ef4444'];

function CircularProgress({ percentage, size = 80, strokeWidth = 6 }: { percentage: number; size?: number; strokeWidth?: number }) {
  const radius = (size - strokeWidth) / 2;
  const circumference = radius * 2 * Math.PI;
  const offset = circumference - (percentage / 100) * circumference;
  const color = percentage <= 0 ? '#ef4444' : percentage <= 20 ? '#f59e0b' : '#10b981';

  return (
    <svg width={size} height={size} className="transform -rotate-90">
      <circle cx={size / 2} cy={size / 2} r={radius} stroke="#e5e7eb" strokeWidth={strokeWidth} fill="none" />
      <circle
        cx={size / 2} cy={size / 2} r={radius} stroke={color} strokeWidth={strokeWidth} fill="none"
        strokeDasharray={circumference} strokeDashoffset={offset}
        strokeLinecap="round" className="transition-all duration-500"
      />
      <text x={size / 2} y={size / 2} textAnchor="middle" dominantBaseline="central"
        className="fill-gray-900 dark:fill-white text-sm font-bold" transform={`rotate(90, ${size / 2}, ${size / 2})`}>
        {percentage.toFixed(1)}%
      </text>
    </svg>
  );
}

export default function SLOPage() {
  const [slos, setSlos] = useState<SLOOverview[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedSLOId, setSelectedSLOId] = useState<number | null>(null);
  const [detail, setDetail] = useState<SLODetail | null>(null);
  const [trend, setTrend] = useState<SLOBudgetTrendPoint[]>([]);
  const [burnAlerts, setBurnAlerts] = useState<SLOBurnRateAlert[]>([]);
  const [showForm, setShowForm] = useState(false);
  const [editingSLO, setEditingSLO] = useState<SLODetail | null>(null);
  const [services, setServices] = useState<string[]>([]);
  const [budgetPreview, setBudgetPreview] = useState<BudgetPreviewResult | null>(null);

  const [compareMode, setCompareMode] = useState(false);
  const [selectedSLOIds, setSelectedSLOIds] = useState<Set<number>>(new Set());
  const [compareResult, setCompareResult] = useState<SLOCompareResult | null>(null);
  const [compareLoading, setCompareLoading] = useState(false);

  const [showExportDialog, setShowExportDialog] = useState(false);
  const [exportConfig, setExportConfig] = useState({
    time_range: '7d' as '7d' | '30d' | 'custom',
    format: 'json' as 'json' | 'csv',
    start_date: '',
    end_date: '',
  });
  const [exportLoading, setExportLoading] = useState(false);

  const [formState, setFormState] = useState({
    name: '',
    service_name: '',
    target_type: 'availability' as 'availability' | 'latency' | 'throughput',
    target_value: 99.9,
    window_type: 'rolling_30d' as 'rolling_7d' | 'rolling_30d' | 'calendar_month',
    latency_threshold_ms: 200,
    target_qps: 100,
    enabled: true,
  });

  const defaultValueByType: Record<string, number> = {
    availability: 99.9,
    latency: 99,
    throughput: 99,
  };

  const loadSLOs = async () => {
    try {
      const res = await sloApi.getSLOs();
      setSlos(res.data.data || []);
    } catch (error) {
      console.error('Failed to load SLOs:', error);
    }
  };

  const loadDetail = async (id: number) => {
    try {
      const [detailRes, trendRes, burnRes] = await Promise.all([
        sloApi.getSLO(id),
        sloApi.getTrend(id, 'hourly'),
        sloApi.getBurnAlerts(id),
      ]);
      setDetail(detailRes.data);
      setTrend(trendRes.data.data || []);
      setBurnAlerts(burnRes.data.data || []);
    } catch (error) {
      console.error('Failed to load SLO detail:', error);
    }
  };

  const loadServices = async () => {
    try {
      const res = await topologyApi.getServices();
      setServices(res.data.data || []);
    } catch (error) {
      console.error('Failed to load services:', error);
    }
  };

  useEffect(() => {
    const loadData = async () => {
      setLoading(true);
      await loadSLOs();
      setLoading(false);
    };
    loadData();
    loadServices();
    const interval = setInterval(loadSLOs, 30000);
    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    if (selectedSLOId !== null) {
      loadDetail(selectedSLOId);
    }
  }, [selectedSLOId]);

  const previewBudget = useCallback(async (targetValue: number, windowType: string) => {
    const decimalValue = targetValue / 100;
    if (decimalValue <= 0 || decimalValue > 1) {
      setBudgetPreview(null);
      return;
    }
    try {
      const res = await sloApi.calculateBudgetPreview(decimalValue, windowType);
      setBudgetPreview(res.data);
    } catch {
      setBudgetPreview(null);
    }
  }, []);

  useEffect(() => {
    if (showForm) {
      previewBudget(formState.target_value, formState.window_type);
    }
  }, [showForm, formState.target_value, formState.window_type, previewBudget]);

  const handleSave = async () => {
    const decimalValue = formState.target_value / 100;
    if (decimalValue <= 0 || decimalValue > 1) {
      alert('目标值(%)必须在0到100之间');
      return;
    }
    const payload: Record<string, unknown> = {
      name: formState.name,
      service_name: formState.service_name,
      target_type: formState.target_type,
      target_value: decimalValue,
      window_type: formState.window_type,
      enabled: formState.enabled,
    };
    if (formState.target_type === 'latency') {
      payload.latency_threshold_ms = formState.latency_threshold_ms;
    }
    if (formState.target_type === 'throughput') {
      payload.target_qps = formState.target_qps;
    }

    try {
      if (editingSLO) {
        await sloApi.updateSLO(editingSLO.definition.id, payload);
      } else {
        await sloApi.createSLO(payload);
      }
      setShowForm(false);
      setEditingSLO(null);
      loadSLOs();
      if (selectedSLOId) loadDetail(selectedSLOId);
    } catch (error) {
      console.error('Failed to save SLO:', error);
    }
  };

  const handleDelete = async (id: number) => {
    if (!confirm('确定删除此SLO？')) return;
    try {
      await sloApi.deleteSLO(id);
      if (selectedSLOId === id) {
        setSelectedSLOId(null);
        setDetail(null);
      }
      loadSLOs();
    } catch (error) {
      console.error('Failed to delete SLO:', error);
    }
  };

  const handleEdit = (sloDetail: SLODetail) => {
    setEditingSLO(sloDetail);
    const def = sloDetail.definition;
    setFormState({
      name: def.name,
      service_name: def.service_name,
      target_type: def.target_type,
      target_value: def.target_value * 100,
      window_type: def.window_type,
      latency_threshold_ms: def.latency_threshold_ms || 200,
      target_qps: def.target_qps || 100,
      enabled: def.enabled,
    });
    setShowForm(true);
  };

  const resetForm = () => {
    setFormState({
      name: '',
      service_name: '',
      target_type: 'availability',
      target_value: 99.9,
      window_type: 'rolling_30d',
      latency_threshold_ms: 200,
      target_qps: 100,
      enabled: true,
    });
    setEditingSLO(null);
    setBudgetPreview(null);
    setShowForm(false);
  };

  const openCreateForm = () => {
    setFormState({
      name: '',
      service_name: '',
      target_type: 'availability',
      target_value: 99.9,
      window_type: 'rolling_30d',
      latency_threshold_ms: 200,
      target_qps: 100,
      enabled: true,
    });
    setEditingSLO(null);
    setShowForm(true);
  };

  const toggleCompareMode = () => {
    if (compareMode) {
      setSelectedSLOIds(new Set());
      setCompareResult(null);
    }
    setCompareMode(!compareMode);
  };

  const toggleSLOSelection = (id: number) => {
    const next = new Set(selectedSLOIds);
    if (next.has(id)) {
      next.delete(id);
    } else if (next.size < 4) {
      next.add(id);
    }
    setSelectedSLOIds(next);
  };

  const handleCompare = async () => {
    if (selectedSLOIds.size < 2) return;
    setCompareLoading(true);
    try {
      const res = await sloApi.compareTrends(Array.from(selectedSLOIds));
      setCompareResult(res.data);
    } catch (error) {
      console.error('Failed to compare SLOs:', error);
    }
    setCompareLoading(false);
  };

  const handleExport = async () => {
    if (!selectedSLOId) return;
    setExportLoading(true);
    try {
      const params: Record<string, string> = {
        time_range: exportConfig.time_range,
        format: exportConfig.format,
      };
      if (exportConfig.time_range === 'custom') {
        if (exportConfig.start_date) params.start_date = exportConfig.start_date;
        if (exportConfig.end_date) params.end_date = exportConfig.end_date;
      }
      const res = await sloApi.exportReport(selectedSLOId, params as any);
      const blob = new Blob([res.data], {
        type: exportConfig.format === 'csv' ? 'text/csv' : 'application/json',
      });
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `slo_report_${selectedSLOId}.${exportConfig.format}`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      window.URL.revokeObjectURL(url);
      setShowExportDialog(false);
    } catch (error) {
      console.error('Failed to export report:', error);
    }
    setExportLoading(false);
  };

  const buildCompareChartData = () => {
    if (!compareResult || compareResult.series.length === 0) return [];
    const timeMap = new Map<string, Record<string, number>>();
    for (const s of compareResult.series) {
      if (!s.points) continue;
      for (const p of s.points) {
        const key = format(new Date(p.timestamp), 'MM-dd HH:mm');
        const existing = timeMap.get(key) || {};
        existing[s.slo_name] = p.error_budget_remaining_pct;
        timeMap.set(key, existing);
      }
    }
    const sorted = Array.from(timeMap.entries()).sort(([a], [b]) => a.localeCompare(b));
    return sorted.map(([time, values]) => ({ time, ...values }));
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
        <h1 className="text-2xl font-bold text-gray-900 dark:text-white">SLO 管理</h1>
        <div className="flex space-x-2">
          <Button
            variant={compareMode ? 'default' : 'outline'}
            onClick={toggleCompareMode}
          >
            <GitCompare className="h-4 w-4 mr-2" />
            {compareMode ? '退出对比' : '对比模式'}
          </Button>
          <Button onClick={openCreateForm}>
            <Plus className="h-4 w-4 mr-2" />
            新建SLO
          </Button>
        </div>
      </div>

      {showForm && (
        <Card>
          <CardHeader>
            <div className="flex justify-between items-center">
              <CardTitle className="text-base">{editingSLO ? '编辑SLO' : '新建SLO'}</CardTitle>
              <button onClick={resetForm} className="p-1 hover:bg-gray-100 dark:hover:bg-gray-800 rounded">
                <X className="h-4 w-4" />
              </button>
            </div>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">目标名称</label>
                <Input value={formState.name} onChange={(e) => setFormState((p) => ({ ...p, name: e.target.value }))} placeholder="如: API可用性SLO" />
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">目标服务</label>
                <Select value={formState.service_name} onChange={(e) => setFormState((p) => ({ ...p, service_name: e.target.value }))}>
                  <option value="">选择服务</option>
                  {services.map((s) => <option key={s} value={s}>{s}</option>)}
                </Select>
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">目标类型</label>
                <Select value={formState.target_type} onChange={(e) => {
                  const newType = e.target.value as typeof formState.target_type;
                  setFormState((p) => ({ ...p, target_type: newType, target_value: defaultValueByType[newType] || 99 }));
                }}>
                  <option value="availability">可用性</option>
                  <option value="latency">延迟</option>
                  <option value="throughput">吞吐量</option>
                </Select>
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
                  目标成功率 (%)
                </label>
                <Input type="number" step="0.1" value={formState.target_value}
                  onChange={(e) => setFormState((p) => ({ ...p, target_value: Number(e.target.value) }))} />
                <p className="text-xs text-gray-400 mt-1">
                  {formState.target_type === 'availability' && '如99.9表示99.9%的请求需成功'}
                  {formState.target_type === 'latency' && '如99表示99%的请求延迟需低于阈值'}
                  {formState.target_type === 'throughput' && '如99表示99%的时间QPS需达标'}
                </p>
              </div>
              {formState.target_type === 'latency' && (
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">延迟阈值 (ms)</label>
                  <Input type="number" value={formState.latency_threshold_ms}
                    onChange={(e) => setFormState((p) => ({ ...p, latency_threshold_ms: Number(e.target.value) }))} />
                  <p className="text-xs text-gray-400 mt-1">超过此值的请求计为不达标</p>
                </div>
              )}
              {formState.target_type === 'throughput' && (
                <div>
                  <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">目标 QPS</label>
                  <Input type="number" value={formState.target_qps}
                    onChange={(e) => setFormState((p) => ({ ...p, target_qps: Number(e.target.value) }))} />
                  <p className="text-xs text-gray-400 mt-1">低于此QPS的时间计为不达标</p>
                </div>
              )}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">计算窗口</label>
                <Select value={formState.window_type} onChange={(e) => setFormState((p) => ({ ...p, window_type: e.target.value as typeof formState.window_type }))}>
                  <option value="rolling_7d">滚动7天</option>
                  <option value="rolling_30d">滚动30天</option>
                  <option value="calendar_month">日历月</option>
                </Select>
              </div>
              <div className="flex items-center space-x-2">
                <input type="checkbox" checked={formState.enabled}
                  onChange={(e) => setFormState((p) => ({ ...p, enabled: e.target.checked }))}
                  className="rounded border-gray-300" />
                <label className="text-sm font-medium text-gray-700 dark:text-gray-300">启用</label>
              </div>
            </div>

            {budgetPreview ? (
              <div className="mt-4 p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
                <div className="flex items-center">
                  <Target className="h-4 w-4 text-blue-600 mr-2 flex-shrink-0" />
                  <span className="text-sm font-medium text-blue-800 dark:text-blue-300">
                    错误预算预览: {budgetPreview.description}
                  </span>
                </div>
              </div>
            ) : (
              <div className="mt-4 p-3 bg-gray-50 dark:bg-gray-800/50 border border-gray-200 dark:border-gray-700 rounded-lg">
                <div className="flex items-center">
                  <Target className="h-4 w-4 text-gray-400 mr-2 flex-shrink-0" />
                  <span className="text-sm text-gray-500 dark:text-gray-400">
                    请填写目标成功率和计算窗口以预览错误预算
                  </span>
                </div>
              </div>
            )}

            <div className="flex justify-end space-x-2 mt-4">
              <Button variant="outline" onClick={resetForm}>取消</Button>
              <Button onClick={handleSave}>{editingSLO ? '更新' : '创建'}</Button>
            </div>
          </CardContent>
        </Card>
      )}

      <div className="flex gap-6">
        <div className={`${detail || (compareMode && compareResult) ? 'w-1/3' : 'w-full'} transition-all duration-300`}>
          <div className="grid grid-cols-1 gap-4">
            {slos.map((slo) => {
              const status = statusConfig[slo.status] || statusConfig.healthy;
              return (
                <Card
                  key={slo.id}
                  className={`cursor-pointer transition-all hover:shadow-md ${
                    selectedSLOId === slo.id ? 'ring-2 ring-blue-500' : ''
                  }`}
                  onClick={() => {
                    if (compareMode) return;
                    setSelectedSLOId(slo.id);
                  }}
                >
                  <CardContent className="p-4">
                    <div className="flex items-center justify-between">
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2 mb-1">
                          {compareMode && (
                            <input
                              type="checkbox"
                              checked={selectedSLOIds.has(slo.id)}
                              onChange={(e) => {
                                e.stopPropagation();
                                toggleSLOSelection(slo.id);
                              }}
                              className="rounded border-gray-300"
                              disabled={!selectedSLOIds.has(slo.id) && selectedSLOIds.size >= 4}
                            />
                          )}
                          <h3 className="text-sm font-semibold text-gray-900 dark:text-white truncate">{slo.name}</h3>
                          <span className={`inline-flex items-center px-2 py-0.5 text-xs rounded ${status.bgColor} ${status.color}`}>
                            {status.icon}
                            <span className="ml-1">{status.label}</span>
                          </span>
                        </div>
                        <p className="text-xs text-gray-500 dark:text-gray-400">{slo.service_name} · {targetTypeLabels[slo.target_type]} · {windowTypeLabels[slo.window_type]}</p>
                        <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                          目标成功率: {(slo.target_value * 100).toFixed(1)}%
                        </p>
                      </div>
                      <div className="ml-4 flex-shrink-0">
                        <CircularProgress percentage={slo.remaining_budget_pct} size={70} strokeWidth={5} />
                      </div>
                    </div>
                  </CardContent>
                </Card>
              );
            })}
            {slos.length === 0 && (
              <div className="text-center py-12 text-gray-500">
                <Target className="h-12 w-12 mx-auto mb-3 text-gray-300" />
                <p>暂无SLO定义，点击"新建SLO"添加</p>
              </div>
            )}
          </div>

          {compareMode && (
            <div className="mt-4">
              <div className="flex items-center gap-2 mb-2">
                <span className="text-sm text-gray-600 dark:text-gray-400">
                  已选 {selectedSLOIds.size}/4 个SLO
                </span>
                <Button
                  size="sm"
                  disabled={selectedSLOIds.size < 2 || compareLoading}
                  onClick={handleCompare}
                >
                  {compareLoading ? '加载中...' : '开始对比'}
                </Button>
              </div>
              {selectedSLOIds.size < 2 && (
                <p className="text-xs text-gray-400">请勾选至少2个SLO进行对比</p>
              )}
            </div>
          )}
        </div>

        {!compareMode && detail && (
          <div className="w-2/3 space-y-4">
            <Card>
              <CardHeader>
                <div className="flex justify-between items-start">
                  <div>
                    <CardTitle className="flex items-center gap-2">
                      {detail.definition.name}
                      <span className={`inline-flex items-center px-2 py-0.5 text-xs rounded ${
                        statusConfig[detail.status]?.bgColor || ''
                      } ${statusConfig[detail.status]?.color || ''}`}>
                        {statusConfig[detail.status]?.icon}
                        <span className="ml-1">{statusConfig[detail.status]?.label}</span>
                      </span>
                    </CardTitle>
                    <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
                      {detail.definition.service_name} · {targetTypeLabels[detail.definition.target_type]} · {windowTypeLabels[detail.definition.window_type]}
                    </p>
                  </div>
                  <div className="flex space-x-2">
                    <button
                      onClick={() => setShowExportDialog(true)}
                      className="p-1 hover:bg-gray-100 dark:hover:bg-gray-800 rounded"
                      title="导出报告"
                    >
                      <Download className="h-4 w-4 text-gray-500" />
                    </button>
                    <button onClick={() => handleEdit(detail)} className="p-1 hover:bg-gray-100 dark:hover:bg-gray-800 rounded">
                      <Edit2 className="h-4 w-4 text-gray-500" />
                    </button>
                    <button onClick={() => handleDelete(detail.definition.id)} className="p-1 hover:bg-gray-100 dark:hover:bg-gray-800 rounded">
                      <Trash2 className="h-4 w-4 text-red-500" />
                    </button>
                    <button onClick={() => { setSelectedSLOId(null); setDetail(null); }} className="p-1 hover:bg-gray-100 dark:hover:bg-gray-800 rounded">
                      <X className="h-4 w-4 text-gray-500" />
                    </button>
                  </div>
                </div>
              </CardHeader>
              <CardContent>
                <div className="grid grid-cols-3 gap-4">
                  <div className="text-center">
                    <CircularProgress percentage={detail.remaining_budget_pct} size={100} strokeWidth={7} />
                    <p className="text-xs text-gray-500 mt-1">剩余预算</p>
                  </div>
                  <div className="space-y-2">
                    <div>
                      <p className="text-xs text-gray-500">目标成功率</p>
                      <p className="text-sm font-medium">
                        {(detail.definition.target_value * 100).toFixed(2)}%
                        {detail.definition.target_type === 'latency' && detail.definition.latency_threshold_ms &&
                          ` (延迟阈值: ${detail.definition.latency_threshold_ms}ms)`}
                        {detail.definition.target_type === 'throughput' && detail.definition.target_qps &&
                          ` (目标QPS: ${detail.definition.target_qps})`}
                      </p>
                    </div>
                    <div>
                      <p className="text-xs text-gray-500">错误预算</p>
                      <p className="text-sm font-medium">
                        允许 {detail.definition.budget_total}{budgetUnitLabels[detail.definition.budget_unit] || detail.definition.budget_unit}不可用
                      </p>
                    </div>
                    <div>
                      <p className="text-xs text-gray-500">当前测量值</p>
                      <p className="text-sm font-medium">
                        {detail.current_snapshot
                          ? `${(detail.current_snapshot.current_measurement * 100).toFixed(2)}%`
                          : '-'}
                      </p>
                    </div>
                  </div>
                  <div className="space-y-2">
                    <div>
                      <p className="text-xs text-gray-500">计算窗口</p>
                      <p className="text-sm font-medium">{windowTypeLabels[detail.definition.window_type]}</p>
                    </div>
                    <div>
                      <p className="text-xs text-gray-500">预计耗尽</p>
                      <p className="text-sm font-medium">
                        {detail.estimated_exhaust_at
                          ? format(new Date(detail.estimated_exhaust_at), 'MM-dd HH:mm')
                          : '暂无预估'}
                      </p>
                    </div>
                    <div>
                      <p className="text-xs text-gray-500">燃烧率告警规则</p>
                      <p className="text-sm font-medium">
                        {(detail.definition.burn_rate_rules || []).map((r: BurnRateRule) => `${r.window_minutes}min>${r.threshold}x`).join(', ') || '默认'}
                      </p>
                    </div>
                  </div>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle className="text-sm">错误预算消耗趋势</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="h-64">
                  <ResponsiveContainer width="100%" height="100%">
                    <AreaChart data={trend} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
                      <CartesianGrid strokeDasharray="3 3" />
                      <XAxis dataKey="timestamp" tickFormatter={(v: string) => format(new Date(v), 'MM-dd HH:mm')} tick={{ fontSize: 10 }} />
                      <YAxis domain={[0, 100]} tickFormatter={(v: number) => `${v}%`} tick={{ fontSize: 10 }} />
                      <Tooltip
                        labelFormatter={(v: string) => format(new Date(v), 'yyyy-MM-dd HH:mm')}
                        formatter={(value: number, name: string) => [
                          `${value.toFixed(1)}%`,
                          name === 'error_budget_remaining_pct' ? '实际剩余' : '理想消耗',
                        ]}
                      />
                      <Area type="monotone" dataKey="ideal_budget_remaining_pct" stroke="#9ca3af" fill="#f3f4f6"
                        strokeDasharray="5 5" name="ideal_budget_remaining_pct" />
                      <Area type="monotone" dataKey="error_budget_remaining_pct" stroke="#3b82f6" fill="#dbeafe"
                        name="error_budget_remaining_pct" />
                      <ReferenceLine y={20} stroke="#f59e0b" strokeDasharray="3 3" label={{ value: '警告线', position: 'right', fontSize: 10 }} />
                      <ReferenceLine y={0} stroke="#ef4444" strokeDasharray="3 3" label={{ value: '违约线', position: 'right', fontSize: 10 }} />
                    </AreaChart>
                  </ResponsiveContainer>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle className="text-sm flex items-center">
                  <TrendingUp className="h-4 w-4 mr-2" />
                  燃烧率告警事件
                </CardTitle>
              </CardHeader>
              <CardContent>
                {burnAlerts.length === 0 ? (
                  <p className="text-sm text-gray-500 text-center py-4">暂无燃烧率告警</p>
                ) : (
                  <div className="space-y-3">
                    {burnAlerts.map((alert) => (
                      <div key={alert.id}
                        className={`flex items-center justify-between p-3 rounded-lg border ${
                          alert.resolved_at
                            ? 'bg-gray-50 dark:bg-gray-800/50 border-gray-200 dark:border-gray-700 opacity-60'
                            : alert.severity === 'critical'
                            ? 'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800'
                            : 'bg-yellow-50 dark:bg-yellow-900/20 border-yellow-200 dark:border-yellow-800'
                        }`}>
                        <div className="flex items-center gap-3">
                          {alert.severity === 'critical' ? (
                            <AlertCircle className="h-4 w-4 text-red-500" />
                          ) : (
                            <AlertTriangle className="h-4 w-4 text-yellow-500" />
                          )}
                          <div>
                            <p className="text-sm font-medium">
                              {alert.window_minutes}分钟窗口 燃烧率 {alert.burn_rate.toFixed(1)}x (阈值: {alert.threshold}x)
                            </p>
                            <p className="text-xs text-gray-500">
                              {format(new Date(alert.fired_at), 'yyyy-MM-dd HH:mm:ss')}
                              {alert.resolved_at && ` → 已恢复 ${format(new Date(alert.resolved_at), 'HH:mm:ss')}`}
                            </p>
                          </div>
                        </div>
                        <span className={`px-2 py-0.5 text-xs rounded ${
                          alert.resolved_at ? 'bg-gray-100 text-gray-500' :
                          alert.severity === 'critical' ? 'bg-red-100 text-red-700' : 'bg-yellow-100 text-yellow-700'
                        }`}>
                          {alert.resolved_at ? '已恢复' : alert.severity === 'critical' ? '紧急' : '警告'}
                        </span>
                      </div>
                    ))}
                  </div>
                )}
              </CardContent>
            </Card>
          </div>
        )}

        {compareMode && compareResult && (
          <div className="w-2/3 space-y-4">
            <Card>
              <CardHeader>
                <CardTitle className="text-sm">错误预算消耗趋势对比</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="h-72">
                  <ResponsiveContainer width="100%" height="100%">
                    <LineChart data={buildCompareChartData()} margin={{ top: 5, right: 20, left: 0, bottom: 5 }}>
                      <CartesianGrid strokeDasharray="3 3" />
                      <XAxis dataKey="time" tick={{ fontSize: 10 }} />
                      <YAxis domain={[0, 100]} tickFormatter={(v: number) => `${v}%`} tick={{ fontSize: 10 }} />
                      <Tooltip formatter={(value: number) => `${value.toFixed(1)}%`} />
                      <Legend />
                      {compareResult.series.map((s, idx) => (
                        <Line
                          key={s.slo_id}
                          type="monotone"
                          dataKey={s.slo_name}
                          stroke={compareColors[idx % compareColors.length]}
                          strokeWidth={2}
                          dot={false}
                        />
                      ))}
                      <ReferenceLine y={20} stroke="#f59e0b" strokeDasharray="3 3" label={{ value: '警告线', position: 'right', fontSize: 10 }} />
                      <ReferenceLine y={0} stroke="#ef4444" strokeDasharray="3 3" label={{ value: '违约线', position: 'right', fontSize: 10 }} />
                    </LineChart>
                  </ResponsiveContainer>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle className="text-sm">关键指标对比</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="overflow-x-auto">
                  <table className="w-full text-sm">
                    <thead>
                      <tr className="border-b border-gray-200 dark:border-gray-700">
                        <th className="text-left py-2 px-3 font-medium text-gray-600 dark:text-gray-400">SLO名称</th>
                        <th className="text-right py-2 px-3 font-medium text-gray-600 dark:text-gray-400">目标成功率</th>
                        <th className="text-right py-2 px-3 font-medium text-gray-600 dark:text-gray-400">当前测量值</th>
                        <th className="text-right py-2 px-3 font-medium text-gray-600 dark:text-gray-400">剩余预算%</th>
                        <th className="text-right py-2 px-3 font-medium text-gray-600 dark:text-gray-400">燃烧率(1h)</th>
                      </tr>
                    </thead>
                    <tbody>
                      {compareResult.metrics.map((m, idx) => (
                        <tr key={m.slo_id} className="border-b border-gray-100 dark:border-gray-800">
                          <td className="py-2 px-3 flex items-center gap-2">
                            <span
                              className="w-3 h-3 rounded-full flex-shrink-0"
                              style={{ backgroundColor: compareColors[idx % compareColors.length] }}
                            />
                            {m.slo_name}
                          </td>
                          <td className={`text-right py-2 px-3 ${(m.target_value * 100) < 99 ? 'bg-red-50 dark:bg-red-900/20' : ''}`}>
                            {(m.target_value * 100).toFixed(2)}%
                          </td>
                          <td className={`text-right py-2 px-3 ${m.current_measurement < m.target_value ? 'bg-red-50 dark:bg-red-900/20' : ''}`}>
                            {(m.current_measurement * 100).toFixed(2)}%
                          </td>
                          <td className={`text-right py-2 px-3 ${m.remaining_budget_pct <= 20 ? 'bg-red-50 dark:bg-red-900/20' : ''}`}>
                            {m.remaining_budget_pct.toFixed(1)}%
                          </td>
                          <td className={`text-right py-2 px-3 ${m.burn_rate_1h > 14 ? 'bg-red-50 dark:bg-red-900/20' : m.burn_rate_1h > 6 ? 'bg-yellow-50 dark:bg-yellow-900/20' : ''}`}>
                            {m.burn_rate_1h.toFixed(1)}x
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </CardContent>
            </Card>
          </div>
        )}
      </div>

      {showExportDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={() => setShowExportDialog(false)}>
          <div className="bg-white dark:bg-gray-900 rounded-lg shadow-xl p-6 w-full max-w-md" onClick={(e) => e.stopPropagation()}>
            <div className="flex justify-between items-center mb-4">
              <h3 className="text-lg font-semibold text-gray-900 dark:text-white">导出合规报告</h3>
              <button onClick={() => setShowExportDialog(false)} className="p-1 hover:bg-gray-100 dark:hover:bg-gray-800 rounded">
                <X className="h-4 w-4" />
              </button>
            </div>

            <div className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">时间范围</label>
                <Select
                  value={exportConfig.time_range}
                  onChange={(e) => setExportConfig((p) => ({ ...p, time_range: e.target.value as typeof exportConfig.time_range }))}
                >
                  <option value="7d">最近7天</option>
                  <option value="30d">最近30天</option>
                  <option value="custom">自定义</option>
                </Select>
              </div>

              {exportConfig.time_range === 'custom' && (
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">开始日期</label>
                    <input
                      type="date"
                      value={exportConfig.start_date}
                      max={exportConfig.end_date || format(new Date(), 'yyyy-MM-dd')}
                      onChange={(e) => setExportConfig((p) => ({ ...p, start_date: e.target.value }))}
                      className="flex h-10 w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent dark:border-gray-700 dark:bg-gray-800 dark:text-white"
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">结束日期</label>
                    <input
                      type="date"
                      value={exportConfig.end_date}
                      min={exportConfig.start_date || undefined}
                      max={format(new Date(), 'yyyy-MM-dd')}
                      onChange={(e) => setExportConfig((p) => ({ ...p, end_date: e.target.value }))}
                      className="flex h-10 w-full rounded-md border border-gray-300 bg-white px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent dark:border-gray-700 dark:bg-gray-800 dark:text-white"
                    />
                  </div>
                  <p className="col-span-2 text-xs text-gray-400">日期格式: YYYY-MM-DD</p>
                </div>
              )}

              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">报告格式</label>
                <Select
                  value={exportConfig.format}
                  onChange={(e) => setExportConfig((p) => ({ ...p, format: e.target.value as typeof exportConfig.format }))}
                >
                  <option value="json">JSON</option>
                  <option value="csv">CSV</option>
                </Select>
                <p className="text-xs text-gray-400 mt-1">
                  {exportConfig.format === 'json'
                    ? '包含SLO定义、每日明细和汇总统计'
                    : '每天一行: 日期、剩余预算%、消耗%、违约时长、平均测量值'}
                </p>
              </div>
            </div>

            <div className="flex justify-end space-x-2 mt-6">
              <Button variant="outline" onClick={() => setShowExportDialog(false)}>取消</Button>
              <Button onClick={handleExport} disabled={exportLoading}>
                <Download className="h-4 w-4 mr-2" />
                {exportLoading ? '导出中...' : '导出'}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
