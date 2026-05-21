import { Tag } from 'antd';
import {
  ClockCircleOutlined,
  SyncOutlined,
  PlayCircleOutlined,
  CheckCircleOutlined,
  ExclamationCircleOutlined,
  PauseCircleOutlined,
} from '@ant-design/icons';
import type { TaskState } from '../types/task';

const config: Record<TaskState, { color: string; icon: React.ReactNode; label: string }> = {
  pending:   { color: 'default',  icon: <ClockCircleOutlined />,   label: '待处理' },
  waiting:   { color: 'processing', icon: <SyncOutlined spin />,   label: '等待中' },
  recording: { color: 'success',  icon: <PlayCircleOutlined />,    label: '录制中' },
  completed: { color: 'blue',     icon: <CheckCircleOutlined />,   label: '已完成' },
  error:     { color: 'error',    icon: <ExclamationCircleOutlined />, label: '错误' },
};

export default function StatusBadge({ state, enabled }: { state: TaskState; enabled?: boolean }) {
  if (enabled === false) {
    return <Tag icon={<PauseCircleOutlined />} color="default">已禁用</Tag>;
  }
  const c = config[state] || config.pending;
  return <Tag icon={c.icon} color={c.color}>{c.label}</Tag>;
}
