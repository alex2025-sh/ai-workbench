// 画布上的节点卡片：上方一个入口，下方每个输出端口一个出口（端口名标在下面）。
import { Handle, Position, type NodeProps } from '@xyflow/react';
import { Typography } from 'antd';
import { NODE_HEIGHT, NODE_WIDTH } from './model';

export interface FlowNodeData extends Record<string, unknown> {
  id: string;
  typeLabel: string;
  name?: string;
  summary: string;
  ports: string[];
  category: string;
  hasIssue: boolean;
  isEntry: boolean;
}

const accent: Record<string, string> = { native: '#1677ff', action: '#13a8a8' };

export function FlowNode({ data, selected }: NodeProps) {
  const d = data as FlowNodeData;
  const border = d.hasIssue ? '#ff4d4f' : selected ? '#1677ff' : '#d9d9d9';
  const width = selected || d.hasIssue ? 2 : 1;
  return (
    <div
      style={{
        width: NODE_WIDTH,
        minHeight: NODE_HEIGHT,
        background: '#fff',
        // 四条边分别设置：简写与单边属性混用时，React 在重新渲染时会把样式弄乱。
        borderStyle: 'solid',
        borderTopWidth: 4,
        borderRightWidth: width,
        borderBottomWidth: width,
        borderLeftWidth: width,
        borderTopColor: d.hasIssue ? '#ff4d4f' : (accent[d.category] ?? '#8c8c8c'),
        borderRightColor: border,
        borderBottomColor: border,
        borderLeftColor: border,
        borderRadius: 6,
        padding: '6px 10px 14px',
        boxShadow: selected ? '0 2px 8px rgba(22,119,255,0.25)' : undefined,
      }}
    >
      {!d.isEntry && <Handle type="target" position={Position.Top} id="in" />}
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
        <Typography.Text strong ellipsis style={{ maxWidth: 120 }}>
          {d.name || d.id}
        </Typography.Text>
        <Typography.Text type="secondary" style={{ fontSize: 12, whiteSpace: 'nowrap' }}>
          {d.typeLabel}
        </Typography.Text>
      </div>
      {d.name && (
        <Typography.Text type="secondary" code style={{ fontSize: 11 }}>
          {d.id}
        </Typography.Text>
      )}
      {d.summary && (
        <div>
          <Typography.Text type="secondary" ellipsis style={{ fontSize: 12, maxWidth: NODE_WIDTH - 20 }}>
            {d.summary}
          </Typography.Text>
        </div>
      )}
      {d.ports.map((p, i) => {
        const left = `${((i + 1) / (d.ports.length + 1)) * 100}%`;
        return (
          <div key={p}>
            <Handle type="source" position={Position.Bottom} id={p} style={{ left, background: p === 'error' || p === 'timeout' ? '#fa8c16' : '#1677ff' }} />
            <span style={{ position: 'absolute', bottom: -18, left, transform: 'translateX(-50%)', fontSize: 10, color: '#8c8c8c', whiteSpace: 'nowrap' }}>
              {p}
            </span>
          </div>
        );
      })}
    </div>
  );
}
