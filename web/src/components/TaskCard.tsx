// 渲染后端给出的卡片（rendered_json）：与 Lark 卡片同一份内容，字段、标签与脱敏都由后端决定，前端不写死字段。
// 任务详情、试运行的模拟发送都用它。
//
// 字段用带边框的 KV 表格展示：标签列浅底、宽度固定、次要色，值列左对齐；空值显示「—」。
// 展示模板里 short=false 的字段（长文本）独占一行，其余字段每行两个（窄屏一个）。
import { Card as AntCard, Descriptions, Empty, Grid, Typography } from 'antd';
import type { DescriptionsProps } from 'antd';
import type { Card } from '../api/types';

const colors: Record<string, string> = {
  blue: '#1677ff',
  orange: '#fa8c16',
  red: '#f5222d',
  green: '#52c41a',
  grey: '#8c8c8c',
  gray: '#8c8c8c',
  purple: '#722ed1',
};

type Field = NonNullable<Card['fields']>[number];

/**
 * 按列数排成 KV 表格的条目：长字段独占一行；一行只剩一个短字段时让它占满，
 * 避免右侧留空格子（Descriptions 要求每行的 span 之和等于列数）。
 */
export function kvItems(fields: Field[], column: number): DescriptionsProps['items'] {
  const items: NonNullable<DescriptionsProps['items']> = [];
  let used = 0; // 当前行已占的列数
  const closeRow = () => {
    if (used > 0 && used < column) {
      const last = items[items.length - 1];
      last.span = Number(last.span ?? 1) + (column - used);
    }
    used = 0;
  };
  fields.forEach((f, i) => {
    const long = f.short === false || column === 1;
    if (long) closeRow();
    items.push({
      key: i,
      label: f.label || '—',
      span: long ? column : 1,
      children: f.value ? (
        <Typography.Text style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{f.value}</Typography.Text>
      ) : (
        <Typography.Text type="secondary">—</Typography.Text>
      ),
    });
    if (long) used = 0;
    else if (++used === column) used = 0;
  });
  closeRow();
  return items;
}

export function TaskCard({ card, size = 'default' }: { card: Card | null; size?: 'default' | 'small' }) {
  const screens = Grid.useBreakpoint();
  if (!card) return <Empty description="没有可显示的卡片" />;
  const accent = colors[card.color ?? ''] ?? '#1677ff';
  const column = screens.md ? 2 : 1;
  const fields = card.fields ?? [];
  return (
    <AntCard size={size} title={card.title} styles={{ header: { borderTop: `4px solid ${accent}`, borderRadius: 0 } }}>
      {card.summary && <Typography.Paragraph style={{ marginBottom: fields.length ? 16 : 0 }}>{card.summary}</Typography.Paragraph>}
      {fields.length > 0 && (
        <Descriptions
          bordered
          size="small"
          column={column}
          items={kvItems(fields, column)}
          labelStyle={{ width: 128, color: 'rgba(0, 0, 0, 0.55)', verticalAlign: 'top' }}
          contentStyle={{ verticalAlign: 'top' }}
        />
      )}
      {(card.links ?? []).length > 0 && (
        <div style={{ marginTop: 12 }}>
          {(card.links ?? []).map((l) => (
            <a key={l.url} href={l.url} target="_blank" rel="noreferrer" style={{ marginRight: 16 }}>
              {l.label}
            </a>
          ))}
        </div>
      )}
    </AntCard>
  );
}
