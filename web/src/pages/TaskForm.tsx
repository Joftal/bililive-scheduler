import { useEffect, useState } from 'react';
import { Form, Input, InputNumber, Button, Card, Space, message, Typography } from 'antd';
import { SwapOutlined } from '@ant-design/icons';
import { useNavigate, useParams } from 'react-router-dom';
import { api } from '../services/api';
import CronInput from '../components/CronInput';
import ScheduleEditor from '../components/ScheduleEditor';
import RoomSelector from '../components/RoomSelector';
import type { CreateTaskRequest, RoomInfo, ScheduleEntry } from '../types/task';

export default function TaskForm() {
  const { id } = useParams<{ id: string }>();
  const isEdit = !!id;
  const navigate = useNavigate();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [selectedRoom, setSelectedRoom] = useState<RoomInfo | null>(null);
  const [taskData, setTaskData] = useState<{ room_url?: string; room_id?: string } | null>(null);
  const [useScheduleEditor, setUseScheduleEditor] = useState(true);
  const [schedules, setSchedules] = useState<ScheduleEntry[]>([]);

  useEffect(() => {
    if (isEdit) {
      setLoading(true);
      api.getTask(Number(id))
        .then((task) => {
          setTaskData({ room_url: task.room_url, room_id: task.room_id });
          const hasSchedules = task.schedules && task.schedules.length > 0;
          setUseScheduleEditor(hasSchedules);
          if (hasSchedules) {
            setSchedules(task.schedules);
          } else {
            form.setFieldsValue({
              cron_expr: task.cron_expr,
              duration_min: task.duration_min,
            });
          }
          form.setFieldsValue({
            name: task.name,
            room_id: task.room_id,
            max_retries: task.max_retries,
          });
        })
        .catch((e) => {
          message.error('加载任务失败: ' + e.message);
          navigate('/tasks');
        })
        .finally(() => setLoading(false));
    }
  }, [id, isEdit, form, navigate]);

  const handleSubmit = async (values: CreateTaskRequest) => {
    setSaving(true);
    try {
      if (isEdit) {
        const payload: Record<string, unknown> = {
          name: values.name,
          max_retries: values.max_retries,
        };
        if (useScheduleEditor) {
          payload.schedules = schedules;
        } else {
          payload.cron_expr = values.cron_expr;
          payload.duration_min = values.duration_min;
        }
        await api.updateTask(Number(id), payload);
        message.success('任务已更新');
      } else {
        const payload: CreateTaskRequest = {
          name: values.name,
          room_id: values.room_id,
          room_url: selectedRoom?.url || '',
          max_retries: values.max_retries,
        };
        if (useScheduleEditor) {
          payload.schedules = schedules;
        } else {
          payload.cron_expr = values.cron_expr;
          payload.duration_min = values.duration_min;
        }
        await api.createTask(payload);
        message.success('任务已创建');
      }
      navigate('/tasks');
    } catch (e: unknown) {
      message.error('保存失败: ' + (e instanceof Error ? e.message : String(e)));
    } finally {
      setSaving(false);
    }
  };

  const toggleMode = () => {
    setUseScheduleEditor(!useScheduleEditor);
  };

  return (
    <Card
      title={isEdit ? '编辑任务' : '新建任务'}
      loading={loading}
      style={{ maxWidth: 700, margin: '0 auto' }}
    >
      <Form
        form={form}
        layout="vertical"
        onFinish={handleSubmit}
        initialValues={{ max_retries: 3, duration_min: 0 }}
      >
        <Form.Item label="任务名称" name="name">
          <Input placeholder="可选，便于识别的名称" />
        </Form.Item>

        {isEdit && taskData ? (
          <Form.Item label="直播间">
            <Typography.Text>{taskData.room_url || taskData.room_id}</Typography.Text>
            <Typography.Text type="secondary" style={{ marginLeft: 8 }}>(创建后不可更改)</Typography.Text>
          </Form.Item>
        ) : (
          <Form.Item
            label="直播间"
            name="room_id"
            rules={[{ required: true, message: '请选择直播间' }]}
          >
            <RoomSelector
              onChange={(val, room) => {
                setSelectedRoom(room);
                form.setFieldsValue({ room_id: val });
              }}
            />
          </Form.Item>
        )}

        {useScheduleEditor ? (
          <Form.Item
            label="录制计划"
            required
            rules={[
              {
                validator: () => {
                  if (schedules.length === 0) {
                    return Promise.reject('请至少添加一个时间段');
                  }
                  for (let i = 0; i < schedules.length; i++) {
                    if (schedules[i].days.length === 0) {
                      return Promise.reject(`时间段 ${i + 1}: 请至少选择一天`);
                    }
                    if (!schedules[i].start_time) {
                      return Promise.reject(`时间段 ${i + 1}: 请设置开始时间`);
                    }
                  }
                  return Promise.resolve();
                },
              },
            ]}
          >
            <ScheduleEditor value={schedules} onChange={setSchedules} />
          </Form.Item>
        ) : (
          <>
            <Form.Item
              label="Cron 表达式"
              name="cron_expr"
              rules={[{ required: true, message: '请输入 cron 表达式' }]}
            >
              <CronInput />
            </Form.Item>

            <Form.Item
              label="录制时长（分钟）"
              name="duration_min"
              tooltip="设置为 0 表示录制直到直播结束，设置为正数表示录制指定分钟后自动停止"
            >
              <InputNumber
                min={0}
                max={1440}
                addonAfter="分钟"
                style={{ width: '100%' }}
                placeholder="0 = 直到直播结束"
              />
            </Form.Item>
          </>
        )}

        <Form.Item>
          <Button
            type="link"
            icon={<SwapOutlined />}
            onClick={toggleMode}
            style={{ padding: 0, marginBottom: 8 }}
          >
            {useScheduleEditor ? '切换到 Cron 表达式模式' : '切换到可视化计划模式'}
          </Button>
        </Form.Item>

        <Form.Item
          label="最大重试次数"
          name="max_retries"
          tooltip="任务失败后的自动重试次数（指数退避）"
        >
          <InputNumber min={0} max={10} style={{ width: '100%' }} />
        </Form.Item>

        <Form.Item>
          <Space>
            <Button type="primary" htmlType="submit" loading={saving}>
              {isEdit ? '保存修改' : '创建任务'}
            </Button>
            <Button onClick={() => navigate('/tasks')}>取消</Button>
          </Space>
        </Form.Item>
      </Form>
    </Card>
  );
}
