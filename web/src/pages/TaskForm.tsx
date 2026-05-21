import { useEffect, useState } from 'react';
import { Form, Input, InputNumber, Button, Card, Space, message, Divider } from 'antd';
import { useNavigate, useParams } from 'react-router-dom';
import { api } from '../services/api';
import CronInput from '../components/CronInput';
import RoomSelector from '../components/RoomSelector';
import type { CreateTaskRequest, RoomInfo } from '../types/task';

export default function TaskForm() {
  const { id } = useParams<{ id: string }>();
  const isEdit = !!id;
  const navigate = useNavigate();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [selectedRoom, setSelectedRoom] = useState<RoomInfo | null>(null);

  useEffect(() => {
    if (isEdit) {
      setLoading(true);
      api.getTask(Number(id))
        .then((task) => {
          form.setFieldsValue({
            name: task.name,
            room_id: task.room_id,
            cron_expr: task.cron_expr,
            duration_min: task.duration_min,
            max_retries: task.max_retries,
          });
        })
        .catch((e) => message.error('加载任务失败: ' + e.message))
        .finally(() => setLoading(false));
    }
  }, [id, isEdit, form]);

  const handleSubmit = async (values: CreateTaskRequest) => {
    setSaving(true);
    try {
      if (isEdit) {
        await api.updateTask(Number(id), {
          name: values.name,
          cron_expr: values.cron_expr,
          duration_min: values.duration_min,
          max_retries: values.max_retries,
        });
        message.success('任务已更新');
      } else {
        await api.createTask({
          ...values,
          room_url: selectedRoom?.url || '',
        });
        message.success('任务已创建');
      }
      navigate('/tasks');
    } catch (e: unknown) {
      message.error('保存失败: ' + (e instanceof Error ? e.message : String(e)));
    } finally {
      setSaving(false);
    }
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
        initialValues={{ duration_min: 0, max_retries: 3 }}
      >
        <Form.Item label="任务名称" name="name">
          <Input placeholder="可选，便于识别的名称" />
        </Form.Item>

        {!isEdit && (
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

        <Form.Item
          label="Cron 表达式"
          name="cron_expr"
          rules={[{ required: true, message: '请输入 cron 表达式' }]}
        >
          <CronInput />
        </Form.Item>

        <Divider />

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
