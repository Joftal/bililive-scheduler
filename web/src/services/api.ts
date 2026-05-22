import type {
  ScheduleTask,
  CreateTaskRequest,
  UpdateTaskRequest,
  SchedulerStatus,
  RoomInfo,
  TaskExecution,
} from '../types/task';

const BASE = '/scheduler/api';

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(BASE + path, {
    headers: { 'Content-Type': 'application/json' },
    ...init,
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error || `HTTP ${res.status}`);
  }
  return res.json();
}

export const api = {
  // Tasks
  listTasks: (state?: string, enabled?: boolean) => {
    const params = new URLSearchParams();
    if (state) params.set('state', state);
    if (enabled !== undefined) params.set('enabled', String(enabled));
    const qs = params.toString();
    return request<ScheduleTask[]>(`/tasks${qs ? '?' + qs : ''}`);
  },

  getTask: (id: number) => request<ScheduleTask>(`/tasks/${id}`),

  createTask: (data: CreateTaskRequest) =>
    request<ScheduleTask>('/tasks', { method: 'POST', body: JSON.stringify(data) }),

  updateTask: (id: number, data: UpdateTaskRequest) =>
    request<ScheduleTask>(`/tasks/${id}`, { method: 'PUT', body: JSON.stringify(data) }),

  deleteTask: (id: number) =>
    request<{ ok: boolean }>(`/tasks/${id}`, { method: 'DELETE' }),

  enableTask: (id: number) =>
    request<ScheduleTask>(`/tasks/${id}/enable`, { method: 'POST' }),

  disableTask: (id: number) =>
    request<ScheduleTask>(`/tasks/${id}/disable`, { method: 'POST' }),

  retryTask: (id: number) =>
    request<ScheduleTask>(`/tasks/${id}/retry`, { method: 'POST' }),

  getTaskHistory: (id: number) =>
    request<TaskExecution[]>(`/tasks/${id}/history`),

  // Status
  getStatus: () => request<SchedulerStatus>('/status'),

  // Rooms
  getRooms: () => request<RoomInfo[]>('/rooms'),

  // Config
  getConfig: () => request<{ tick_interval: number }>('/config'),
  updateConfig: (data: { tick_interval: number }) =>
    request<{ tick_interval: number }>('/config', { method: 'PUT', body: JSON.stringify(data) }),
};
