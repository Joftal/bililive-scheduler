import { useEffect, useState } from 'react';
import { Select, Space, Tag, Typography } from 'antd';
import { api } from '../services/api';
import type { RoomInfo } from '../types/task';

interface Props {
  value?: string;
  onChange?: (value: string, room: RoomInfo) => void;
}

export default function RoomSelector({ value, onChange }: Props) {
  const [rooms, setRooms] = useState<RoomInfo[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    setLoading(true);
    api.getRooms()
      .then(setRooms)
      .catch(() => setRooms([]))
      .finally(() => setLoading(false));
  }, []);

  const options = rooms.map((r) => ({
    value: r.id,
    label: (
      <Space>
        <span>{r.host_name || r.url}</span>
        {r.room_name && <Typography.Text type="secondary">- {r.room_name}</Typography.Text>}
        {r.is_live ? <Tag color="green">直播中</Tag> : <Tag>未开播</Tag>}
      </Space>
    ),
    room: r,
  }));

  return (
    <Select
      showSearch
      value={value}
      onChange={(val) => {
        const room = rooms.find((r) => r.id === val);
        if (room && onChange) onChange(val, room);
      }}
      options={options}
      loading={loading}
      placeholder="选择直播间"
      optionFilterProp="label"
      style={{ width: '100%' }}
      notFoundContent={loading ? '加载中...' : '无可用房间'}
    />
  );
}
