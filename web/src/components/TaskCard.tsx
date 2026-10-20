// 渲染后端给出的卡片：与 Lark 卡片同一份内容（标题、当前节点、字段 KV），字段与脱敏由任务类型决定。
// 任务详情与配置页的卡片预览都用它。
import { Card as AntCard, Descriptions, Empty, Grid, Tag, Typography } from 'antd';
import type { Card } from '../api/types';

export function TaskCard({ card, extra }: { card: Card | null; extra?: React.ReactNode }) {
  const screens = Grid.useBreakpoint();
  if (!card) return <Empty description="没有可显示的卡片" />;
  const fields = card.fields ?? [];
  const column = screens.md ? 2 : 1;
  return (
    <AntCard
      size="small"
      title={
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
          {card.node_name && <Tag color="blue" style={{ marginInlineEnd: 0, flex: 'none' }}>{card.node_name}</Tag>}
          <Typography.Text strong ellipsis={{ tooltip: card.title }} style={{ flex: 1, minWidth: 0 }}>
            {card.title || '（无标题）'}
          </Typography.Text>
        </div>
      }
      extra={extra}
      styles={{ header: { borderTop: '3px solid #1677ff', borderRadius: 0 } }}
    >
      {fields.length > 0 ? (
        <Descriptions
          bordered
          size="small"
          column={column}
          items={fields.map((f, i) => ({
            key: i,
            label: f.label,
            // 字段数为奇数时最后一格占满整行，避免右侧空格子
            span: column === 2 && i === fields.length - 1 && fields.length % 2 === 1 ? 2 : 1,
            children: f.value ? <span style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{f.value}</span> : <Typography.Text type="secondary">—</Typography.Text>,
          }))}
          labelStyle={{ width: 112, color: 'rgba(0, 0, 0, 0.55)' }}
        />
      ) : (
        <Typography.Text type="secondary">卡片没有配置字段</Typography.Text>
      )}
      {card.link && (
        <div style={{ marginTop: 12 }}>
          <Typography.Link href={card.link.value} target="_blank" rel="noreferrer">
            {card.link.label}
          </Typography.Link>
        </div>
      )}
    </AntCard>
  );
}
