import { useEffect, useState } from 'react';
import { Select } from 'antd';
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
    label: `${r.host_name || r.url}${r.room_name ? ' - ' + r.room_name : ''}${r.is_live ? ' [直播中]' : ''}`,
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
      filterOption={(input, option) => {
        if (!option?.label) return false;
        return String(option.label).toLowerCase().includes(input.toLowerCase());
      }}
      style={{ width: '100%' }}
      notFoundContent={loading ? '加载中...' : '无可用房间'}
    />
  );
}
