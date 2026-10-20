// 节点链画布：start → 人工节点 → end，从左到右。连线不能手画，由节点启用的动作生成：
// escalate 连到下一个节点（上方），close / no_response 连到 end（下方汇成一条线）。
// 配置页用它选择节点、添加节点；任务详情用它显示当前节点与走过的节点。
import { PlusOutlined } from '@ant-design/icons';
import { Handle, MarkerType, Position, ReactFlow, type Edge, type Node, type NodeProps } from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { Button, Tag, Tooltip, Typography } from 'antd';
import { useMemo, useState } from 'react';

export interface ChainNode {
  id: string;
  name: string;
  assignee: string; // 分配器的一行说明
  actions: string[];
  lark?: boolean;
  issue?: boolean; // 配置有问题：红框
  state?: 'current' | 'visited' | 'future'; // 任务详情：当前、走过、未到
}

const W = 210;
const H = 96;
const GAP = 70;
const TERMINAL = 64;

const actionText: Record<string, string> = { close: '已处理', no_response: '无需处理', snooze: '推迟', link: '链接', escalate: '升级' };

interface Data extends Record<string, unknown> {
  node?: ChainNode;
  terminal?: 'start' | 'end';
  selected?: boolean;
  hasNext?: boolean;
}

function ChainBox({ data }: NodeProps) {
  const d = data as Data;
  if (d.terminal) {
    return (
      <div
        style={{
          width: TERMINAL,
          height: 40,
          borderRadius: 20,
          border: '1px solid #bfbfbf',
          background: '#fafafa',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          color: 'rgba(0,0,0,0.65)',
        }}
      >
        {d.terminal === 'start' ? (
          <Handle type="source" position={Position.Right} id="next" />
        ) : (
          <>
            <Handle type="target" position={Position.Left} id="in" />
            <Handle type="target" position={Position.Bottom} id="done" />
          </>
        )}
        {d.terminal}
      </div>
    );
  }
  const n = d.node!;
  const color = n.issue ? '#ff4d4f' : d.selected || n.state === 'current' ? '#1677ff' : n.state === 'visited' ? '#52c41a' : '#d9d9d9';
  return (
    <div
      style={{
        width: W,
        minHeight: H,
        padding: '8px 12px',
        borderRadius: 8,
        background: '#fff',
        border: `${d.selected || n.state === 'current' || n.issue ? 2 : 1}px solid ${color}`,
        boxShadow: d.selected ? '0 2px 8px rgba(22,119,255,0.25)' : undefined,
        opacity: n.state === 'future' ? 0.55 : 1,
        cursor: 'pointer',
      }}
    >
      <Handle type="target" position={Position.Left} id="in" />
      <Handle type="source" position={Position.Right} id="next" />
      <Handle type="source" position={Position.Bottom} id="done" />
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
        <Typography.Text strong ellipsis style={{ maxWidth: 140 }}>
          {n.name || n.id}
        </Typography.Text>
        <span style={{ fontSize: 12, color: n.state === 'current' ? '#1677ff' : 'rgba(0,0,0,0.45)' }}>
          {n.state === 'current' ? '当前' : n.state === 'visited' ? '已经过' : n.lark ? 'Lark 通知' : ''}
        </span>
      </div>
      <Tooltip title={n.assignee}>
        <Typography.Text type="secondary" ellipsis style={{ fontSize: 12, display: 'block' }}>
          {n.assignee || '未配置处理人'}
        </Typography.Text>
      </Tooltip>
      <div style={{ marginTop: 6, display: 'flex', flexWrap: 'wrap', gap: 4 }}>
        {n.actions.map((a) => (
          <Tag key={a} style={{ marginInlineEnd: 0, fontSize: 11, lineHeight: '18px' }}>
            {actionText[a] ?? a}
          </Tag>
        ))}
      </div>
    </div>
  );
}

const nodeTypes = { chain: ChainBox };

export function ChainCanvas(props: {
  nodes: ChainNode[];
  selected?: string;
  onSelect?: (id: string) => void;
  onAdd?: () => void; // 配置页：在末尾添加节点
  height?: number;
}) {
  const [measured, setMeasured] = useState<Record<string, { width: number; height: number }>>({});
  const list = props.nodes;
  const y = 40;
  const xOf = (i: number) => TERMINAL + GAP + i * (W + GAP);
  const endX = xOf(list.length);

  const nodes: Node[] = useMemo(() => {
    const out: Node[] = [
      { id: '__start', type: 'chain', position: { x: 0, y: y + H / 2 - 20 }, data: { terminal: 'start' }, draggable: false, selectable: false },
      ...list.map((n, i) => ({
        id: n.id,
        type: 'chain',
        position: { x: xOf(i), y },
        data: { node: n, selected: props.selected === n.id },
        draggable: false,
      })),
      { id: '__end', type: 'chain', position: { x: endX, y: y + H / 2 - 20 }, data: { terminal: 'end' }, draggable: false, selectable: false },
    ];
    return out.map((n) => ({ ...n, measured: measured[n.id] }));
  }, [list, props.selected, measured]);

  const edges: Edge[] = useMemo(() => {
    const out: Edge[] = [];
    const arrow = (color: string) => ({ type: MarkerType.ArrowClosed, color });
    if (list.length > 0) out.push({ id: 'e-start', source: '__start', sourceHandle: 'next', target: list[0].id, targetHandle: 'in', markerEnd: arrow('#8c8c8c'), style: { stroke: '#8c8c8c' } });
    list.forEach((n, i) => {
      const next = list[i + 1];
      if (next && n.actions.includes('escalate')) {
        out.push({ id: `e-${n.id}-esc`, source: n.id, sourceHandle: 'next', target: next.id, targetHandle: 'in', label: '升级',
          markerEnd: arrow('#fa8c16'), style: { stroke: '#fa8c16' }, labelStyle: { fill: '#d46b08', fontSize: 12 } });
      }
      const ends = n.actions.filter((a) => a === 'close' || a === 'no_response').map((a) => actionText[a]);
      if (ends.length > 0) {
        out.push({ id: `e-${n.id}-end`, source: n.id, sourceHandle: 'done', target: '__end', targetHandle: 'done', type: 'smoothstep',
          label: ends.join(' / '), markerEnd: arrow('#52c41a'), style: { stroke: '#52c41a' }, labelStyle: { fill: '#389e0d', fontSize: 12 },
          pathOptions: { offset: 24 + i * 6 } } as Edge);
      }
    });
    return out;
  }, [list]);

  return (
    <div style={{ height: props.height ?? 260, border: '1px solid #f0f0f0', borderRadius: 8, position: 'relative', background: '#fcfcfc' }}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        onNodesChange={(changes) => {
          const sizes: Record<string, { width: number; height: number }> = {};
          for (const c of changes) if (c.type === 'dimensions' && c.dimensions) sizes[c.id] = c.dimensions;
          if (Object.keys(sizes).length) setMeasured((m) => ({ ...m, ...sizes }));
        }}
        onNodeClick={(_, n) => !n.id.startsWith('__') && props.onSelect?.(n.id)}
        fitView
        fitViewOptions={{ padding: 0.15, maxZoom: 1 }}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable={false}
        zoomOnScroll={false}
        preventScrolling={false}
        panOnDrag
        proOptions={{ hideAttribution: true }}
      />
      {props.onAdd && (
        <Button size="small" icon={<PlusOutlined />} onClick={props.onAdd} style={{ position: 'absolute', right: 12, top: 12, zIndex: 5 }}>
          添加节点
        </Button>
      )}
    </div>
  );
}
