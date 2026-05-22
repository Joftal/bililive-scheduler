import { useEffect, useState } from 'react';
import { Tag, Button, Spin, message } from 'antd';
import {
  PlayCircleOutlined,
  ClockCircleOutlined,
  ExclamationCircleOutlined,
  CheckCircleOutlined,
  ApiOutlined,
  ReloadOutlined,
  CalendarOutlined,
  QuestionCircleOutlined,
} from '@ant-design/icons';
import { api } from '../services/api';
import type { SchedulerStatus } from '../types/task';
import './Dashboard.css';

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

  const s = status;

  return (
    <Spin spinning={loading && !s}>
      <div className="dashboard">
        {/* Header */}
        <div className="dashboard-header">
          <div className="dashboard-header-info">
            <h4><CalendarOutlined style={{ marginRight: 8, color: '#1890ff' }} />调度器仪表盘</h4>
            <p>管理定时录制任务，查看调度器运行状态</p>
          </div>
          <Button icon={<ReloadOutlined />} onClick={load} loading={loading}>刷新</Button>
        </div>

        {/* Stat cards */}
        <div className="stat-grid">
          <div className="stat-card">
            <div className={`stat-icon ${s?.running ? 'green' : 'red'}`}>
              {s?.running ? <CheckCircleOutlined /> : <ExclamationCircleOutlined />}
            </div>
            <div className="stat-content">
              <div className="stat-label">调度器状态</div>
              <div className={`stat-value ${s?.running ? 'green' : 'red'}`}>
                {s?.running ? '运行中' : '已停止'}
                {s?.uptime && <span className="stat-extra">运行时间: {s.uptime}</span>}
              </div>
            </div>
          </div>

          <div className="stat-card">
            <div className="stat-icon blue">
              <ClockCircleOutlined />
            </div>
            <div className="stat-content">
              <div className="stat-label">总任务数</div>
              <div className="stat-value">
                {s?.total_tasks ?? '-'}
                {s?.waiting_tasks != null && <span className="stat-extra">等待中: {s.waiting_tasks}</span>}
              </div>
            </div>
          </div>

          <div className="stat-card">
            <div className="stat-icon green">
              <PlayCircleOutlined />
            </div>
            <div className="stat-content">
              <div className="stat-label">正在录制</div>
              <div className="stat-value green">{s?.active_recordings ?? '-'}</div>
            </div>
          </div>

          <div className="stat-card">
            <div className={`stat-icon ${s?.error_tasks ? 'red' : 'blue'}`}>
              <ExclamationCircleOutlined />
            </div>
            <div className="stat-content">
              <div className="stat-label">错误任务</div>
              <div className={`stat-value ${s?.error_tasks ? 'red' : ''}`}>
                {s?.error_tasks ?? '-'}
              </div>
            </div>
          </div>
        </div>

        {/* Bottom panels */}
        <div className="bottom-grid">
          <div className="panel">
            <div className="panel-header">
              <h5>连接状态</h5>
            </div>
            <div className="panel-body">
              <div className="connection-row">
                <ApiOutlined className="conn-icon" />
                <span className="conn-label">bililive-go API</span>
                <span className="conn-status">
                  {s?.bili_api_reachable
                    ? <Tag color="green">可达</Tag>
                    : <Tag color="red">不可达</Tag>
                  }
                </span>
              </div>
            </div>
          </div>

          <div className="panel">
            <div className="panel-header">
              <h5><QuestionCircleOutlined style={{ marginRight: 6, fontSize: 14 }} />快速说明</h5>
            </div>
            <div className="panel-body">
              <ul className="help-list">
                <li><strong>录制计划</strong>: 可视化设置录制时间段，选择星期、开始时间和时长，支持同一天多段录制</li>
                <li><strong>录制时长</strong>: 每个时间段可独立设置，0 表示录制直到直播结束，正数表示指定分钟后自动停止</li>
                <li><strong>Cron 表达式</strong>: 高级模式，标准 5 段格式 <code>分 时 日 月 周</code>，例如 <code>0 20 * * 1-5</code></li>
                <li><strong>自动重试</strong>: 任务失败后自动重试（指数退避），超过最大重试次数后进入错误状态</li>
              </ul>
            </div>
          </div>
        </div>
      </div>
    </Spin>
  );
}
