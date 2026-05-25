import { useEffect, useState } from 'react';
import { Tag, Button, Spin, message, InputNumber, Space } from 'antd';
import {
  PlayCircleOutlined,
  ClockCircleOutlined,
  ExclamationCircleOutlined,
  CheckCircleOutlined,
  ApiOutlined,
  ReloadOutlined,
  CalendarOutlined,
  QuestionCircleOutlined,
  SettingOutlined,
} from '@ant-design/icons';
import { api } from '../services/api';
import type { SchedulerStatus } from '../types/task';
import './Dashboard.css';

export default function Dashboard() {
  const [status, setStatus] = useState<SchedulerStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [tickInterval, setTickInterval] = useState(15);
  const [savingConfig, setSavingConfig] = useState(false);

  const load = () => {
    setLoading(true);
    Promise.all([
      api.getStatus(),
      api.getConfig().catch(() => ({ tick_interval: 15 })),
    ])
      .then(([statusData, configData]) => {
        setStatus(statusData);
        setTickInterval(configData.tick_interval);
      })
      .catch((e) => message.error('获取状态失败: ' + e.message))
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(); }, []);

  const saveConfig = async () => {
    setSavingConfig(true);
    try {
      const result = await api.updateConfig({ tick_interval: tickInterval });
      setTickInterval(result.tick_interval);
      message.success('配置已保存');
    } catch (e: unknown) {
      message.error('保存失败: ' + (e instanceof Error ? e.message : String(e)));
    } finally {
      setSavingConfig(false);
    }
  };

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
        <div className="bottom-grid bottom-grid-3">
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
              <h5><SettingOutlined style={{ marginRight: 6, fontSize: 14 }} />引擎设置</h5>
            </div>
            <div className="panel-body">
              <div className="config-row">
                <span className="config-label">检查间隔</span>
                <Space>
                  <InputNumber
                    min={5}
                    max={300}
                    value={tickInterval}
                    onChange={(v) => setTickInterval(v ?? 15)}
                    addonAfter="秒"
                    style={{ width: 120 }}
                    size="small"
                  />
                  <Button size="small" type="primary" onClick={saveConfig} loading={savingConfig}>保存</Button>
                </Space>
              </div>
              <div className="config-hint">引擎每隔指定秒数检查一次任务触发（5-300秒）</div>
            </div>
          </div>

          <div className="panel">
            <div className="panel-header">
              <h5><QuestionCircleOutlined style={{ marginRight: 6, fontSize: 14 }} />快速说明</h5>
            </div>
            <div className="panel-body">
              <div className="help-section">
                <div className="help-title">参数说明</div>
                <ul className="help-list">
                  <li><strong>录制计划</strong>: 选择星期和开始时间，支持同一天多段录制，同一房间同一天的相同时段会自动检测冲突</li>
                  <li><strong>录制时长</strong>: 0 = 录制直到主播下播，正数 = 录制指定分钟后自动停止（每次录制独立计时）</li>
                  <li><strong>监控时长</strong>: 两个作用——① 触发时若房间未开播，在此时间内持续检测等待开播；② 录制中断流后，在监控窗口内自动等待主播重新开播并恢复录制</li>
                  <li><strong>自动重试</strong>: 任务失败后按指数退避重试（1分→2分→4分…最长15分钟），超过最大重试次数后进入错误状态，需手动重试</li>
                </ul>
              </div>
              <div className="help-section">
                <div className="help-title">执行示例</div>
                <ul className="help-list">
                  <li>配置「周一 20:00，监控 30 分钟」→ 周一 20:00 触发，若房间未开播则每 15 秒检测一次，直到 20:30 前开播即开始录制</li>
                  <li>配置「录制 120 分钟，监控 30 分钟」→ 20:00 开始录制，20:25 主播断流，20:28 重新开播，自动恢复录制，20:50 再次断流（已过 20:30 监控窗口）→ 录制结束</li>
                  <li>不配置监控时长 → 触发时若未开播直接跳过，录制中断流立即停止，等待下次计划时间</li>
                </ul>
              </div>
            </div>
          </div>
        </div>
      </div>
    </Spin>
  );
}
