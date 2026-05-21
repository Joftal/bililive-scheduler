export type TaskState = 'pending' | 'waiting' | 'recording' | 'completed' | 'error';

export interface ScheduleTask {
  id: number;
  name: string;
  room_id: string;
  room_url: string;
  cron_expr: string;
  duration_min: number;
  enabled: boolean;
  state: TaskState;
  next_fire_at: string | null;
  current_live_start: string | null;
  last_error: string;
  retry_count: number;
  max_retries: number;
  created_at: string;
  updated_at: string;
}

export interface CreateTaskRequest {
  name: string;
  room_id: string;
  room_url: string;
  cron_expr: string;
  duration_min: number;
  max_retries: number;
}

export interface UpdateTaskRequest {
  name?: string;
  cron_expr?: string;
  duration_min?: number;
  max_retries?: number;
}

export interface SchedulerStatus {
  running: boolean;
  total_tasks: number;
  active_recordings: number;
  waiting_tasks: number;
  error_tasks: number;
  bili_api_reachable: boolean;
  uptime: string;
}

export interface RoomInfo {
  id: string;
  host_name: string;
  room_name: string;
  url: string;
  is_live: boolean;
}

export interface TaskExecution {
  id: number;
  task_id: number;
  start_time: string;
  end_time: string | null;
  state: TaskState;
  error: string;
}
