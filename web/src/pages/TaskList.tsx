import { useEffect, useState, useCallback } from 'react';
import { Table, Button, Space, Popconfirm, message, Typography, Switch, Tag, Tooltip, Card } from 'antd';
import {
  PlusOutlined, DeleteOutlined, EditOutlined, ReloadOutlined,
  RedoOutlined, HistoryOutlined,
} from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import { api } from '../services/api';
import StatusBadge from '../components/StatusBadge';
import type { ScheduleTask, TaskExecution } from '../types/task';
import dayjs from 'dayjs';

export default function TaskList() {
  const [tasks, setTasks] = useState<ScheduleTask[]>([]);
  const [loading, setLoading] = useState(true);
  const [history, setHistory] = useState<TaskExecution[] | null>(null);
  const [historyTaskId, setHistoryTaskId] = useState<number | null>(null);
  const navigate = useNavigate();

  const load = useCallback(() => {
    setLoading(true);
    api.listTasks()
      .then(setTasks)
      .catch((e) => message.error('加载失败: ' + e.message))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => { load(); }, [load]);

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

  const showHistory = async (taskId: number) => {
    try {
      const data = await api.getTaskHistory(taskId);
      setHistory(data);
      setHistoryTaskId(taskId);
    } catch (e: unknown) {
      message.error('获取历史失败: ' + (e instanceof Error ? e.message : String(e)));
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
      dataIndex: 'room_url',
      ellipsis: true,
      render: (url: string) => (
        <Tooltip title={url}>
          <Typography.Text style={{ maxWidth: 200 }} ellipsis>{url}</Typography.Text>
        </Tooltip>
      ),
    },
    {
      title: 'Cron',
      dataIndex: 'cron_expr',
      render: (expr: string) => <Tag>{expr}</Tag>,
    },
    {
      title: '时长',
      dataIndex: 'duration_min',
      width: 80,
      render: (m: number) => m > 0 ? `${m}分钟` : '直到结束',
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
      width: 200,
      render: (_: unknown, r: ScheduleTask) => (
        <Space size="small">
          <Button size="small" icon={<EditOutlined />} onClick={() => navigate(`/tasks/${r.id}/edit`)} />
          {r.state === 'error' && (
            <Button size="small" icon={<RedoOutlined />} onClick={() => handleRetry(r.id)} type="primary" danger>
              重试
            </Button>
          )}
          <Button size="small" icon={<HistoryOutlined />} onClick={() => showHistory(r.id)} />
          <Popconfirm title="确认删除此任务？" onConfirm={() => handleDelete(r.id)}>
            <Button size="small" icon={<DeleteOutlined />} danger />
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
        expandable={{
          expandedRowRender: (r) => r.last_error ? (
            <Typography.Text type="danger">错误信息: {r.last_error} (重试 {r.retry_count}/{r.max_retries})</Typography.Text>
          ) : <Typography.Text type="secondary">无错误信息</Typography.Text>,
          rowExpandable: (r) => r.state === 'error' || r.last_error !== '',
        }}
      />

      {history && (
        <Card title={`执行历史 - 任务 #${historyTaskId}`} extra={<Button size="small" onClick={() => setHistory(null)}>关闭</Button>}>
          <Table
            dataSource={history}
            rowKey="id"
            size="small"
            pagination={false}
            columns={[
              { title: '开始时间', dataIndex: 'start_time', render: (t: string) => dayjs(t).format('MM-DD HH:mm:ss') },
              { title: '结束时间', dataIndex: 'end_time', render: (t: string | null) => t ? dayjs(t).format('MM-DD HH:mm:ss') : '-' },
              { title: '状态', dataIndex: 'state', render: (s: string) => <StatusBadge state={s as ScheduleTask['state']} /> },
              { title: '备注', dataIndex: 'error' },
            ]}
          />
        </Card>
      )}
    </Space>
  );
}
