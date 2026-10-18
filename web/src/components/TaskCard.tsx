// 渲染后端给出的卡片（rendered_json）：与 Lark 卡片同一份内容，字段、标签与脱敏都由后端决定，前端不写死字段。
// 任务详情、试运行的模拟发送都用它。
import { Card as AntCard, Col, Empty, Row, Typography } from 'antd';
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

export function TaskCard({ card, size = 'default' }: { card: Card | null; size?: 'default' | 'small' }) {
  if (!card) return <Empty description="没有可显示的卡片" />;
  const accent = colors[card.color ?? ''] ?? '#1677ff';
  return (
    <AntCard
      size={size}
      title={card.title}
      styles={{ header: { borderTop: `4px solid ${accent}`, borderRadius: 0 } }}
    >
      {card.summary && <Typography.Paragraph>{card.summary}</Typography.Paragraph>}
      <Row gutter={[16, 12]}>
        {(card.fields ?? []).map((f, i) => (
          <Col key={i} span={f.short === false ? 24 : 12}>
            <Typography.Text type="secondary">{f.label}</Typography.Text>
            <div>{f.value || '—'}</div>
          </Col>
        ))}
      </Row>
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
