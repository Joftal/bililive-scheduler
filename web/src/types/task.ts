export type TaskState = 'pending' | 'waiting' | 'recording' | 'completed' | 'error';

export interface ScheduleEntry {
  days: number[];        // 1=Mon, 2=Tue, ..., 6=Sat, 7=Sun (ISO 8601)
  start_time: string;    // "HH:MM"
  duration_min: number;  // 0 = until stream ends
  cron_expr: string;     // auto-generated, read-only
}

export interface ScheduleTask {
  id: number;
  name: string;
  room_id: string;
  room_url: string;
  enabled: boolean;
  state: TaskState;
  next_fire_at: string | null;
  current_live_start: string | null;
  last_error: string;
  retry_count: number;
  max_retries: number;
  created_at: string;
  updated_at: string;
  schedules: ScheduleEntry[];
  current_schedule_idx: number;
  next_fire_schedule_idx: number;
}

export interface CreateTaskRequest {
  name: string;
  room_id: string;
  room_url: string;
  max_retries: number;
  schedules: ScheduleEntry[];
}

export interface UpdateTaskRequest {
  name?: string;
  max_retries?: number;
  schedules?: ScheduleEntry[];
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
