'use client';

import { useEffect, useState } from 'react';
import { format } from 'date-fns';
import { Bell, Plus, Trash2, Edit2, Check, AlertTriangle, Info, AlertCircle, ExternalLink } from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select } from '@/components/ui/select';
import { alertApi } from '@/lib/api';
import type { AlertRule, AlertEvent } from '@/types';

type Tab = 'rules' | 'events';

const severityConfig: Record<string, { color: string; icon: React.ReactNode }> = {
  info: { color: 'bg-blue-100 text-blue-700', icon: <Info className="h-4 w-4" /> },
  warning: { color: 'bg-yellow-100 text-yellow-700', icon: <AlertTriangle className="h-4 w-4" /> },
  critical: { color: 'bg-red-100 text-red-700', icon: <AlertCircle className="h-4 w-4" /> },
};

const typeLabels: Record<string, string> = {
  threshold: '阈值触发',
  spike: '环比突增',
  topology: '拓扑异常',
};

export default function AlertsPage() {
  const [tab, setTab] = useState<Tab>('rules');
  const [rules, setRules] = useState<AlertRule[]>([]);
  const [events, setEvents] = useState<AlertEvent[]>([]);
  const [eventsTotal, setEventsTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [showRuleForm, setShowRuleForm] = useState(false);
  const [editingRule, setEditingRule] = useState<AlertRule | null>(null);

  const [formState, setFormState] = useState<Partial<AlertRule>>({
    name: '',
    description: '',
    type: 'threshold',
    enabled: true,
    severity: 'warning',
    service_name: '',
    metric: 'error_rate',
    operator: '>',
    threshold: 0.05,
    duration_seconds: 180,
    spike_window_minutes: 60,
    spike_multiplier: 2.0,
    topology_check: 'all_downstream_inactive',
    cooldown_seconds: 300,
  });

  const loadRules = async () => {
    try {
      const res = await alertApi.getRules();
      setRules(res.data.data || []);
    } catch (error) {
      console.error('Failed to load alert rules:', error);
    }
  };

  const loadEvents = async (offset = 0) => {
    try {
      const res = await alertApi.getEvents({ limit: 50, offset });
      setEvents(res.data.data || []);
      setEventsTotal(res.data.total);
    } catch (error) {
      console.error('Failed to load alert events:', error);
    }
  };

  useEffect(() => {
    const loadData = async () => {
      setLoading(true);
      await Promise.all([loadRules(), loadEvents()]);
      setLoading(false);
    };
    loadData();
    const interval = setInterval(() => {
      if (tab === 'rules') loadRules();
      else loadEvents();
    }, 30000);
    return () => clearInterval(interval);
  }, [tab]);

  const handleSaveRule = async () => {
    try {
      if (editingRule) {
        await alertApi.updateRule(editingRule.id, formState);
      } else {
        await alertApi.createRule(formState);
      }
      setShowRuleForm(false);
      setEditingRule(null);
      loadRules();
    } catch (error) {
      console.error('Failed to save rule:', error);
    }
  };

  const handleDeleteRule = async (id: number) => {
    if (!confirm('确定删除此规则？')) return;
    try {
      await alertApi.deleteRule(id);
      loadRules();
    } catch (error) {
      console.error('Failed to delete rule:', error);
    }
  };

  const handleEditRule = (rule: AlertRule) => {
    setEditingRule(rule);
    setFormState({
      name: rule.name,
      description: rule.description,
      type: rule.type,
      enabled: rule.enabled,
      severity: rule.severity,
      service_name: rule.service_name,
      metric: rule.metric,
      operator: rule.operator,
      threshold: rule.threshold,
      duration_seconds: rule.duration_seconds,
      spike_window_minutes: rule.spike_window_minutes,
      spike_multiplier: rule.spike_multiplier,
      topology_check: rule.topology_check,
      cooldown_seconds: rule.cooldown_seconds,
    });
    setShowRuleForm(true);
  };

  const handleAcknowledge = async (id: number) => {
    try {
      await alertApi.acknowledgeEvent(id);
      loadEvents();
    } catch (error) {
      console.error('Failed to acknowledge event:', error);
    }
  };

  const resetForm = () => {
    setFormState({
      name: '',
      description: '',
      type: 'threshold',
      enabled: true,
      severity: 'warning',
      service_name: '',
      metric: 'error_rate',
      operator: '>',
      threshold: 0.05,
      duration_seconds: 180,
      spike_window_minutes: 60,
      spike_multiplier: 2.0,
      topology_check: 'all_downstream_inactive',
      cooldown_seconds: 300,
    });
    setEditingRule(null);
    setShowRuleForm(false);
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
        <h1 className="text-2xl font-bold text-gray-900 dark:text-white">告警管理</h1>
        <div className="flex items-center space-x-4">
          <div className="flex bg-gray-100 dark:bg-gray-800 rounded-lg p-1">
            <button
              onClick={() => setTab('rules')}
              className={`px-4 py-2 text-sm rounded-md ${
                tab === 'rules' ? 'bg-white dark:bg-gray-700 shadow-sm' : ''
              }`}
            >
              告警规则
            </button>
            <button
              onClick={() => setTab('events')}
              className={`px-4 py-2 text-sm rounded-md ${
                tab === 'events' ? 'bg-white dark:bg-gray-700 shadow-sm' : ''
              }`}
            >
              告警事件
            </button>
          </div>
          {tab === 'rules' && (
            <Button onClick={() => { resetForm(); setShowRuleForm(true); }}>
              <Plus className="h-4 w-4 mr-2" />
              新建规则
            </Button>
          )}
        </div>
      </div>

      {showRuleForm && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{editingRule ? '编辑规则' : '新建规则'}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">规则名称</label>
                <Input
                  value={formState.name || ''}
                  onChange={(e) => setFormState((prev) => ({ ...prev, name: e.target.value }))}
                  placeholder="规则名称"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">描述</label>
                <Input
                  value={formState.description || ''}
                  onChange={(e) => setFormState((prev) => ({ ...prev, description: e.target.value }))}
                  placeholder="描述"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">规则类型</label>
                <Select
                  value={formState.type || 'threshold'}
                  onChange={(e) => setFormState((prev) => ({ ...prev, type: e.target.value as AlertRule['type'] }))}
                >
                  <option value="threshold">阈值触发</option>
                  <option value="spike">环比突增</option>
                  <option value="topology">拓扑异常</option>
                </Select>
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">严重级别</label>
                <Select
                  value={formState.severity || 'warning'}
                  onChange={(e) => setFormState((prev) => ({ ...prev, severity: e.target.value as AlertRule['severity'] }))}
                >
                  <option value="info">信息</option>
                  <option value="warning">警告</option>
                  <option value="critical">严重</option>
                </Select>
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">服务名称</label>
                <Input
                  value={formState.service_name || ''}
                  onChange={(e) => setFormState((prev) => ({ ...prev, service_name: e.target.value }))}
                  placeholder="留空表示所有服务"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">监控指标</label>
                <Select
                  value={formState.metric || 'error_rate'}
                  onChange={(e) => setFormState((prev) => ({ ...prev, metric: e.target.value }))}
                >
                  <option value="error_rate">错误率</option>
                  <option value="p99_latency">P99延迟</option>
                  <option value="avg_latency">平均延迟</option>
                </Select>
              </div>
              {formState.type === 'threshold' && (
                <>
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">比较运算符</label>
                    <Select
                      value={formState.operator || '>'}
                      onChange={(e) => setFormState((prev) => ({ ...prev, operator: e.target.value }))}
                    >
                      <option value=">">&gt;</option>
                      <option value=">=">&gt;=</option>
                      <option value="<">&lt;</option>
                      <option value="<=">&lt;=</option>
                    </Select>
                  </div>
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">阈值</label>
                    <Input
                      type="number"
                      step="0.01"
                      value={formState.threshold || 0}
                      onChange={(e) => setFormState((prev) => ({ ...prev, threshold: Number(e.target.value) }))}
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">持续时长(秒)</label>
                    <Input
                      type="number"
                      value={formState.duration_seconds || 0}
                      onChange={(e) => setFormState((prev) => ({ ...prev, duration_seconds: Number(e.target.value) }))}
                    />
                  </div>
                </>
              )}
              {formState.type === 'spike' && (
                <>
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">环比窗口(分钟)</label>
                    <Input
                      type="number"
                      value={formState.spike_window_minutes || 60}
                      onChange={(e) => setFormState((prev) => ({ ...prev, spike_window_minutes: Number(e.target.value) }))}
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">突增倍数</label>
                    <Input
                      type="number"
                      step="0.1"
                      value={formState.spike_multiplier || 2.0}
                      onChange={(e) => setFormState((prev) => ({ ...prev, spike_multiplier: Number(e.target.value) }))}
                    />
                  </div>
                </>
              )}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">冷却时间(秒)</label>
                <Input
                  type="number"
                  value={formState.cooldown_seconds || 300}
                  onChange={(e) => setFormState((prev) => ({ ...prev, cooldown_seconds: Number(e.target.value) }))}
                />
              </div>
              <div className="flex items-center space-x-2">
                <input
                  type="checkbox"
                  checked={formState.enabled !== false}
                  onChange={(e) => setFormState((prev) => ({ ...prev, enabled: e.target.checked }))}
                  className="rounded border-gray-300"
                />
                <label className="text-sm font-medium text-gray-700 dark:text-gray-300">启用</label>
              </div>
            </div>
            <div className="flex justify-end space-x-2 mt-4">
              <Button variant="outline" onClick={resetForm}>取消</Button>
              <Button onClick={handleSaveRule}>{editingRule ? '更新' : '创建'}</Button>
            </div>
          </CardContent>
        </Card>
      )}

      {tab === 'rules' && (
        <Card>
          <CardHeader>
            <CardTitle>告警规则列表</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-gray-200 dark:border-gray-700">
                    <th className="text-left py-3 px-4 font-medium text-gray-500">名称</th>
                    <th className="text-left py-3 px-4 font-medium text-gray-500">类型</th>
                    <th className="text-left py-3 px-4 font-medium text-gray-500">严重级别</th>
                    <th className="text-left py-3 px-4 font-medium text-gray-500">服务</th>
                    <th className="text-left py-3 px-4 font-medium text-gray-500">指标/条件</th>
                    <th className="text-left py-3 px-4 font-medium text-gray-500">状态</th>
                    <th className="text-left py-3 px-4 font-medium text-gray-500">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {rules.map((rule) => (
                    <tr key={rule.id} className="border-b border-gray-100 dark:border-gray-800">
                      <td className="py-3 px-4 font-medium">{rule.name}</td>
                      <td className="py-3 px-4">
                        <span className="px-2 py-0.5 text-xs rounded bg-gray-100 dark:bg-gray-800">
                          {typeLabels[rule.type] || rule.type}
                        </span>
                      </td>
                      <td className="py-3 px-4">
                        <span className={`inline-flex items-center px-2 py-0.5 text-xs rounded ${severityConfig[rule.severity]?.color || ''}`}>
                          {severityConfig[rule.severity]?.icon}
                          <span className="ml-1">{rule.severity}</span>
                        </span>
                      </td>
                      <td className="py-3 px-4">{rule.service_name || '所有服务'}</td>
                      <td className="py-3 px-4 text-gray-600 dark:text-gray-400">
                        {rule.type === 'threshold' && `${rule.metric} ${rule.operator} ${rule.threshold}`}
                        {rule.type === 'spike' && `${rule.metric} 上涨 ${rule.spike_multiplier}x`}
                        {rule.type === 'topology' && rule.topology_check}
                      </td>
                      <td className="py-3 px-4">
                        <span className={`px-2 py-0.5 text-xs rounded ${rule.enabled ? 'bg-green-100 text-green-700' : 'bg-gray-100 text-gray-500'}`}>
                          {rule.enabled ? '启用' : '禁用'}
                        </span>
                      </td>
                      <td className="py-3 px-4">
                        <div className="flex space-x-2">
                          <button
                            onClick={() => handleEditRule(rule)}
                            className="p-1 hover:bg-gray-100 dark:hover:bg-gray-800 rounded"
                          >
                            <Edit2 className="h-4 w-4 text-gray-500" />
                          </button>
                          <button
                            onClick={() => handleDeleteRule(rule.id)}
                            className="p-1 hover:bg-gray-100 dark:hover:bg-gray-800 rounded"
                          >
                            <Trash2 className="h-4 w-4 text-red-500" />
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                  {rules.length === 0 && (
                    <tr>
                      <td colSpan={7} className="py-12 text-center text-gray-500">
                        暂无告警规则，点击"新建规则"添加
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </CardContent>
        </Card>
      )}

      {tab === 'events' && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center">
              <Bell className="h-5 w-5 mr-2" />
              告警事件
              <span className="ml-2 text-sm font-normal text-gray-500">共 {eventsTotal} 条</span>
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-gray-200 dark:border-gray-700">
                    <th className="text-left py-3 px-4 font-medium text-gray-500">级别</th>
                    <th className="text-left py-3 px-4 font-medium text-gray-500">规则名称</th>
                    <th className="text-left py-3 px-4 font-medium text-gray-500">服务</th>
                    <th className="text-left py-3 px-4 font-medium text-gray-500">当前值</th>
                    <th className="text-left py-3 px-4 font-medium text-gray-500">阈值</th>
                    <th className="text-left py-3 px-4 font-medium text-gray-500">信息</th>
                    <th className="text-left py-3 px-4 font-medium text-gray-500">关联Trace</th>
                    <th className="text-left py-3 px-4 font-medium text-gray-500">触发时间</th>
                    <th className="text-left py-3 px-4 font-medium text-gray-500">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {events.map((event) => (
                    <tr
                      key={event.id}
                      className={`border-b border-gray-100 dark:border-gray-800 ${
                        event.acknowledged ? 'opacity-60' : ''
                      }`}
                    >
                      <td className="py-3 px-4">
                        <span className={`inline-flex items-center px-2 py-0.5 text-xs rounded ${severityConfig[event.severity]?.color || ''}`}>
                          {severityConfig[event.severity]?.icon}
                        </span>
                      </td>
                      <td className="py-3 px-4 font-medium">{event.rule_name}</td>
                      <td className="py-3 px-4">{event.service_name || '-'}</td>
                      <td className="py-3 px-4 font-mono text-sm">{event.metric_value.toFixed(4)}</td>
                      <td className="py-3 px-4 font-mono text-sm">{event.threshold}</td>
                      <td className="py-3 px-4 text-gray-600 dark:text-gray-400 max-w-xs truncate">
                        {event.message || '-'}
                      </td>
                      <td className="py-3 px-4">
                        {event.trace_ids && event.trace_ids.length > 0 ? (
                          <div className="flex flex-wrap gap-1">
                            {event.trace_ids.slice(0, 3).map((tid) => (
                              <a
                                key={tid}
                                href={`/trace/?id=${tid}`}
                                className="text-xs text-blue-600 hover:underline flex items-center"
                              >
                                {tid.slice(0, 8)}...
                                <ExternalLink className="h-3 w-3 ml-0.5" />
                              </a>
                            ))}
                            {event.trace_ids.length > 3 && (
                              <span className="text-xs text-gray-400">+{event.trace_ids.length - 3}</span>
                            )}
                          </div>
                        ) : (
                          <span className="text-gray-400 text-xs">-</span>
                        )}
                      </td>
                      <td className="py-3 px-4 text-gray-500">
                        {format(new Date(event.fired_at), 'MM-dd HH:mm:ss')}
                      </td>
                      <td className="py-3 px-4">
                        {!event.acknowledged && (
                          <button
                            onClick={() => handleAcknowledge(event.id)}
                            className="p-1 hover:bg-gray-100 dark:hover:bg-gray-800 rounded"
                            title="确认"
                          >
                            <Check className="h-4 w-4 text-green-500" />
                          </button>
                        )}
                      </td>
                    </tr>
                  ))}
                  {events.length === 0 && (
                    <tr>
                      <td colSpan={9} className="py-12 text-center text-gray-500">
                        暂无告警事件
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
