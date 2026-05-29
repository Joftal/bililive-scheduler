import { useState, useEffect } from 'react';
import { Button, Checkbox, InputNumber, TimePicker, Card, Empty, Alert } from 'antd';
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons';
import dayjs from 'dayjs';
import type { ScheduleEntry } from '../types/task';
import './ScheduleEditor.css';

const DAY_OPTIONS = [
  { label: '一', value: 1 },
  { label: '二', value: 2 },
  { label: '三', value: 3 },
  { label: '四', value: 4 },
  { label: '五', value: 5 },
  { label: '六', value: 6 },
  { label: '日', value: 7 },
];

interface Props {
  value?: ScheduleEntry[];
  onChange?: (entries: ScheduleEntry[]) => void;
}

function generateCron(days: number[], startTime: string): string {
  if (!startTime || days.length === 0) return '';
  const parts = startTime.split(':');
  if (parts.length !== 2) return '';
  const [hour, minute] = parts;
  const sortedDays = [...days].sort((a, b) => a - b);
  const cronDays = sortedDays.map(d => d % 7); // ISO 8601 (7=Sun) → cron (0=Sun)
  return `${minute} ${hour} * * ${cronDays.join(',')}`;
}

function timeToMinutes(t: string): number {
  const [h, m] = t.split(':').map(Number);
  return h * 60 + m;
}

function findDuplicateConflicts(entries: ScheduleEntry[]): string[] {
  const warnings: string[] = [];
  for (let i = 0; i < entries.length; i++) {
    for (let j = i + 1; j < entries.length; j++) {
      if (!entries[i].start_time || !entries[j].start_time) continue;
      const startI = timeToMinutes(entries[i].start_time);
      const startJ = timeToMinutes(entries[j].start_time);
      const endI = entries[i].duration_min === 0 ? 24 * 60 : startI + entries[i].duration_min;
      const endJ = entries[j].duration_min === 0 ? 24 * 60 : startJ + entries[j].duration_min;
      if (startI >= endJ || startJ >= endI) continue;
      const overlap = entries[i].days.filter((d) => entries[j].days.includes(d));
      if (overlap.length > 0) {
        const dayNames = overlap.map((d) => DAY_OPTIONS.find(o => o.value === d)?.label ?? String(d)).join('、');
        const fmtEnd = (s: number, d: number) => d === 0 ? '持续' : `${String(Math.floor((s + d) / 60)).padStart(2, '0')}:${String((s + d) % 60).padStart(2, '0')}`;
        warnings.push(`时间段 ${i + 1} (${entries[i].start_time}~${fmtEnd(startI, entries[i].duration_min)}) 和 ${j + 1} (${entries[j].start_time}~${fmtEnd(startJ, entries[j].duration_min)}) 在 ${dayNames} 时间重叠`);
      }
    }
  }
  return warnings;
}

export default function ScheduleEditor({ value = [], onChange }: Props) {
  const [entries, setEntries] = useState<ScheduleEntry[]>(value);

  useEffect(() => {
    setEntries(value);
  }, [value]);

  const updateEntries = (newEntries: ScheduleEntry[]) => {
    setEntries(newEntries);
    onChange?.(newEntries);
  };

  const addEntry = () => {
    updateEntries([
      ...entries,
      { days: [1, 2, 3, 4, 5], start_time: '20:00', duration_min: 0, monitor_min: 0, cron_expr: '' },
    ]);
  };

  const removeEntry = (index: number) => {
    updateEntries(entries.filter((_, i) => i !== index));
  };

  const updateEntry = (index: number, patch: Partial<ScheduleEntry>) => {
    const newEntries = entries.map((entry, i) => {
      if (i !== index) return entry;
      const updated = { ...entry, ...patch };
      updated.cron_expr = generateCron(updated.days, updated.start_time);
      return updated;
    });
    updateEntries(newEntries);
  };

  return (
    <div className="schedule-editor">
      {entries.length === 0 && (
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description="暂无录制计划，请添加时间段"
          style={{ margin: '16px 0' }}
        />
      )}

      {(() => {
        const conflicts = findDuplicateConflicts(entries);
        if (conflicts.length > 0) {
          return (
            <Alert
              type="warning"
              showIcon
              message="存在重复时间段"
              description={conflicts.join('；')}
              style={{ marginBottom: 12 }}
            />
          );
        }
        return null;
      })()}

      {entries.map((entry, index) => (
        <Card
          key={index}
          size="small"
          className="schedule-entry-card"
          title={
            <span className="entry-title">
              时间段 {index + 1}
              {entry.cron_expr && (
                <span className="cron-preview">{entry.cron_expr}</span>
              )}
            </span>
          }
          extra={
            <Button
              type="text"
              danger
              icon={<DeleteOutlined />}
              size="small"
              onClick={() => removeEntry(index)}
            />
          }
        >
          <div className="entry-row">
            <div className="entry-field">
              <div className="field-label">星期</div>
              <Checkbox.Group
                options={DAY_OPTIONS}
                value={entry.days}
                onChange={(days) => updateEntry(index, { days: days as number[] })}
              />
            </div>
          </div>
          <div className="entry-row entry-row-inline">
            <div className="entry-field">
              <div className="field-label">开始时间</div>
              <TimePicker
                format="HH:mm"
                value={entry.start_time ? dayjs(entry.start_time, 'HH:mm') : null}
                onChange={(time) =>
                  updateEntry(index, { start_time: time ? time.format('HH:mm') : '' })
                }
                allowClear={false}
                changeOnBlur
              />
            </div>
            <div className="entry-field">
              <div className="field-label">录制时长</div>
              <InputNumber
                min={0}
                max={1440}
                value={entry.duration_min}
                onChange={(v) => updateEntry(index, { duration_min: v ?? 0 })}
                addonAfter="分钟"
                style={{ width: 140 }}
              />
              <span className="duration-hint">
                {entry.duration_min === 0 ? '直到直播结束' : ''}
              </span>
            </div>
            <div className="entry-field">
              <div className="field-label">监控时长</div>
              <InputNumber
                min={0}
                max={1440}
                value={entry.monitor_min}
                onChange={(v) => updateEntry(index, { monitor_min: v ?? 0 })}
                addonAfter="分钟"
                style={{ width: 140 }}
              />
              <span className="duration-hint">
                {entry.monitor_min === 0 ? '不监控' : `${entry.monitor_min}分钟内`}
              </span>
            </div>
          </div>
        </Card>
      ))}

      <Button
        type="dashed"
        block
        icon={<PlusOutlined />}
        onClick={addEntry}
        style={{ marginTop: 8 }}
      >
        添加时间段
      </Button>
    </div>
  );
}
