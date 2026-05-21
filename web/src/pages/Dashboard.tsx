import { useEffect, useState } from 'react';
import { Card, Col, Row, Statistic, Tag, Typography, Space, Button, message } from 'antd';
import {
  PlayCircleOutlined,
  ClockCircleOutlined,
  ExclamationCircleOutlined,
  CheckCircleOutlined,
  ApiOutlined,
  ReloadOutlined,
} from '@ant-design/icons';
import { api } from '../services/api';
import type { SchedulerStatus } from '../types/task';

export default function Dashboard() {
  const [status, setStatus] = useState<SchedulerStatus | null>(null);
  const [loading, setLoading] = useState(true);

  const load = () => {
    setLoading(true);
    api.getStatus()
      .then(setStatus)
      .catch((e) => message.error('获取状态失败: ' + e.message))
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(); }, []);

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Space style={{ justifyContent: 'space-between', width: '100%' }}>
        <Typography.Title level={4} style={{ margin: 0 }}>调度器仪表盘</Typography.Title>
        <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>刷新</Button>
      </Space>

      <Row gutter={[16, 16]}>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic
              title="调度器状态"
              value={status?.running ? '运行中' : '已停止'}
              prefix={status?.running ? <CheckCircleOutlined style={{ color: '#52c41a' }} /> : <ExclamationCircleOutlined style={{ color: '#ff4d4f' }} />}
            />
            {status?.uptime && <Typography.Text type="secondary">运行时间: {status.uptime}</Typography.Text>}
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic
              title="总任务数"
              value={status?.total_tasks ?? 0}
              prefix={<ClockCircleOutlined />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic
              title="正在录制"
              value={status?.active_recordings ?? 0}
              valueStyle={{ color: '#52c41a' }}
              prefix={<PlayCircleOutlined />}
            />
          </Card>
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <Card>
            <Statistic
              title="错误任务"
              value={status?.error_tasks ?? 0}
              valueStyle={status?.error_tasks ? { color: '#ff4d4f' } : undefined}
              prefix={<ExclamationCircleOutlined />}
            />
          </Card>
        </Col>
      </Row>

      <Card title="连接状态">
        <Space>
          <ApiOutlined />
          <span>bililive-go API:</span>
          {status?.bili_api_reachable
            ? <Tag color="green">可达</Tag>
            : <Tag color="red">不可达</Tag>
          }
        </Space>
      </Card>

      <Card title="快速说明">
        <Typography.Paragraph>
          <ul>
            <li><strong>Cron 表达式</strong>: 标准 5 段格式 <code>分 时 日 月 周</code>，例如 <code>0 20 * * 1-5</code> 表示工作日每天 20:00</li>
            <li><strong>录制时长</strong>: 设置为 0 表示录制直到直播结束，设置为正数表示录制指定分钟后自动停止</li>
            <li><strong>重试</strong>: 任务失败后会自动重试（指数退避），超过最大重试次数后进入错误状态，需手动重试</li>
          </ul>
        </Typography.Paragraph>
      </Card>
    </Space>
  );
}
