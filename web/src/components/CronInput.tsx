import { useState, useMemo } from 'react';
import { Input, Typography, Space, Tag } from 'antd';
import cronstrue from 'cronstrue';
import 'cronstrue/locales/zh_CN';

interface Props {
  value?: string;
  onChange?: (value: string) => void;
}

function parseCronNext(expr: string): string[] {
  if (!expr || expr.trim() === '') return [];
  try {
    const desc = cronstrue.toString(expr, { locale: 'zh_CN', use24HourTimeFormat: true });
    // We can't compute actual next times in JS without a library,
    // so we just show the human-readable description.
    return [desc];
  } catch {
    return [];
  }
}

export default function CronInput({ value = '', onChange }: Props) {
  const [touched, setTouched] = useState(false);
  const desc = useMemo(() => parseCronNext(value), [value]);
  const isValid = useMemo(() => {
    if (!value || value.trim() === '') return false;
    try {
      cronstrue.toString(value, { locale: 'zh_CN' });
      return true;
    } catch {
      return false;
    }
  }, [value]);

  return (
    <Space direction="vertical" style={{ width: '100%' }}>
      <Input
        value={value}
        onChange={(e) => onChange?.(e.target.value)}
        onBlur={() => setTouched(true)}
        placeholder="分 时 日 月 周  例: 0 20 * * 1-5"
        status={touched && value && !isValid ? 'error' : undefined}
      />
      {touched && value && !isValid && (
        <Typography.Text type="danger">无效的 cron 表达式</Typography.Text>
      )}
      {value && isValid && desc.length > 0 && (
        <Space size={4} wrap>
          <Typography.Text type="secondary">含义：</Typography.Text>
          <Tag color="blue">{desc[0]}</Tag>
        </Space>
      )}
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        标准 5 段格式：分(0-59) 时(0-23) 日(1-31) 月(1-12) 周(0-7)
        &nbsp;&nbsp;常用示例：
        <Typography.Link onClick={() => onChange?.('0 20 * * 1-5')}>工作日20点</Typography.Link>
        &nbsp;|&nbsp;
        <Typography.Link onClick={() => onChange?.('*/30 * * * *')}>每30分钟</Typography.Link>
        &nbsp;|&nbsp;
        <Typography.Link onClick={() => onChange?.('0 */2 * * *')}>每2小时</Typography.Link>
      </Typography.Text>
    </Space>
  );
}
