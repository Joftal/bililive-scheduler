import { useEffect, useState, useCallback } from 'react';
import { Table, Button, Space, Popconfirm, message, Typography, Switch, Tooltip } from 'antd';
import {
  PlusOutlined, DeleteOutlined, EditOutlined, ReloadOutlined,
  RedoOutlined,
} from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import { api } from '../services/api';
import StatusBadge from '../components/StatusBadge';
import type { ScheduleTask, ScheduleEntry, TaskExecution } from '../types/task';
import dayjs from 'dayjs';

const DAY_NAMES = ['日', '一', '二', '三', '四', '五', '六'];

function formatDays(days: number[]): string {
  if (days.length === 0) return '';
  const sorted = [...days].sort((a, b) => a - b);
  const ranges: string[] = [];
  let start = sorted[0];
  let end = sorted[0];
  for (let i = 1; i < sorted.length; i++) {
    if (sorted[i] === end + 1) {
      end = sorted[i];
    } else {
      ranges.push(start === end ? `周${DAY_NAMES[start]}` : `周${DAY_NAMES[start]}-${DAY_NAMES[end]}`);
      start = sorted[i];
      end = sorted[i];
    }
  }
  ranges.push(start === end ? `周${DAY_NAMES[start]}` : `周${DAY_NAMES[start]}-${DAY_NAMES[end]}`);
  return ranges.join(' ');
}

function formatScheduleSummary(schedules: ScheduleEntry[]): string {
  return schedules.map((s) => {
    const days = formatDays(s.days);
    const duration = s.duration_min > 0 ? ` (${s.duration_min}min)` : '';
    return `${days} ${s.start_time}${duration}`;
  }).join(' / ');
}

export default function TaskList() {
  const [tasks, setTasks] = useState<ScheduleTask[]>([]);
  const [loading, setLoading] = useState(true);
  const [expandedRowKeys, setExpandedRowKeys] = useState<React.Key[]>([]);
  const [historyCache, setHistoryCache] = useState<Record<number, TaskExecution[]>>({});
  const [loadingHistory, setLoadingHistory] = useState<Record<number, boolean>>({});
  const navigate = useNavigate();

  const load = useCallback(() => {
    setLoading(true);
    api.listTasks()
      .then(setTasks)
      .catch((e) => message.error('加载失败: ' + e.message))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => { load(); }, [load]);

  const loadHistory = async (taskId: number) => {
    setLoadingHistory((prev) => ({ ...prev, [taskId]: true }));
    try {
      const data = await api.getTaskHistory(taskId);
      setHistoryCache((prev) => ({ ...prev, [taskId]: data }));
    } catch (e: unknown) {
      message.error('获取历史失败: ' + (e instanceof Error ? e.message : String(e)));
    } finally {
      setLoadingHistory((prev) => ({ ...prev, [taskId]: false }));
    }
  };

  const toggleExpand = (taskId: number) => {
    if (expandedRowKeys.includes(taskId)) {
      setExpandedRowKeys(expandedRowKeys.filter((k) => k !== taskId));
    } else {
      setExpandedRowKeys([...expandedRowKeys, taskId]);
      if (!historyCache[taskId]) {
        loadHistory(taskId);
      }
    }
  };

  const handleDelete = async (id: number) => {
    try {
      await api.deleteTask(id);
      message.success('已删除');
      load();
    } catch (e: unknown) {
      message.error('删除失败: ' + (e instanceof Error ? e.message : String(e)));
    }
  };

  const handleToggle = async (task: ScheduleTask) => {
    try {
      if (task.enabled) {
        await api.disableTask(task.id);
        message.success('已禁用');
      } else {
        await api.enableTask(task.id);
        message.success('已启用');
      }
      load();
    } catch (e: unknown) {
      message.error('操作失败: ' + (e instanceof Error ? e.message : String(e)));
    }
  };

  const handleRetry = async (id: number) => {
    try {
      await api.retryTask(id);
      message.success('已重试');
      load();
    } catch (e: unknown) {
      message.error('重试失败: ' + (e instanceof Error ? e.message : String(e)));
    }
  };

  const columns = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (name: string, r: ScheduleTask) => name || `任务 #${r.id}`,
    },
    {
      title: '房间',
      render: (_: unknown, r: ScheduleTask) => {
        const display = r.room_url || r.room_id || '-';
        return (
          <Tooltip title={display}>
            <Typography.Text style={{ maxWidth: 200 }} ellipsis>{display}</Typography.Text>
          </Tooltip>
        );
      },
    },
    {
      title: '录制计划',
      render: (_: unknown, r: ScheduleTask) => {
        const summary = formatScheduleSummary(r.schedules || []);
        return (
          <Tooltip title={summary}>
            <Typography.Text style={{ maxWidth: 280 }} ellipsis>{summary}</Typography.Text>
          </Tooltip>
        );
      },
    },
    {
      title: '状态',
      dataIndex: 'state',
      width: 100,
      render: (state: ScheduleTask['state'], r: ScheduleTask) => (
        <StatusBadge state={state} enabled={r.enabled} />
      ),
    },
    {
      title: '下次触发',
      dataIndex: 'next_fire_at',
      width: 170,
      render: (t: string | null) => t ? dayjs(t).format('MM-DD HH:mm:ss') : '-',
    },
    {
      title: '启用',
      dataIndex: 'enabled',
      width: 60,
      render: (enabled: boolean, r: ScheduleTask) => (
        <Switch checked={enabled} onChange={() => handleToggle(r)} size="small" />
      ),
    },
    {
      title: '操作',
      width: 160,
      render: (_: unknown, r: ScheduleTask) => (
        <Space size="small">
          <Button size="small" icon={<EditOutlined />} onClick={(e) => { e.stopPropagation(); navigate(`/tasks/${r.id}/edit`); }} />
          {r.state === 'error' && (
            <Button size="small" icon={<RedoOutlined />} onClick={(e) => { e.stopPropagation(); handleRetry(r.id); }} type="primary" danger>
              重试
            </Button>
          )}
          <Popconfirm title="确认删除此任务？" onConfirm={() => handleDelete(r.id)} onCancel={(e) => e?.stopPropagation()}>
            <Button size="small" icon={<DeleteOutlined />} danger onClick={(e) => e.stopPropagation()} />
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Space style={{ justifyContent: 'space-between', width: '100%' }}>
        <Typography.Title level={4} style={{ margin: 0 }}>任务列表</Typography.Title>
        <Space>
          <Button icon={<ReloadOutlined />} onClick={load}>刷新</Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate('/tasks/new')}>
            新建任务
          </Button>
        </Space>
      </Space>

      <Table
        dataSource={tasks}
        columns={columns}
        rowKey="id"
        loading={loading}
        pagination={false}
        size="middle"
        expandedRowKeys={expandedRowKeys}
        expandable={{
          expandedRowRender: (r) => {
            const execs = historyCache[r.id];
            if (loadingHistory[r.id]) {
              return <Typography.Text type="secondary">加载中...</Typography.Text>;
            }
            if (!execs || execs.length === 0) {
              return <Typography.Text type="secondary">暂无执行记录</Typography.Text>;
            }
            return (
              <Table
                dataSource={execs}
                rowKey="id"
                size="small"
                pagination={false}
                style={{ margin: '0 16px' }}
                columns={[
                  { title: '开始时间', dataIndex: 'start_time', render: (t: string) => dayjs(t).format('MM-DD HH:mm:ss') },
                  { title: '结束时间', dataIndex: 'end_time', render: (t: string | null) => t ? dayjs(t).format('MM-DD HH:mm:ss') : '-', width: 170 },
                  { title: '状态', dataIndex: 'state', width: 100, render: (s: string) => <StatusBadge state={s as ScheduleTask['state']} /> },
                  { title: '备注', dataIndex: 'error', ellipsis: true },
                ]}
              />
            );
          },
          showExpandColumn: false,
        }}
        onRow={(record) => ({
          onClick: (e) => {
            const target = e.target as HTMLElement;
            if (target.tagName === 'TD') {
              toggleExpand(record.id);
            }
          },
          style: { cursor: 'pointer', transition: 'background-color 0.2s' },
        })}
      />
    </Space>
  );
}
