// 运行态画布（只读）：按该 run 锁定版本的 workflow 绘制，节点按状态着色，走过的连线高亮（端口名标在节点下方）。
import { Background, Controls, MarkerType, ReactFlow, type Edge, type Node } from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { useMemo, useState } from 'react';
import type { NodeTypeInfo, NodeView } from '../../api/types';
import { nodeStatusLabel } from '../../format';
import { FlowNode, type FlowNodeData } from './FlowNode';
import { autoLayout, NODE_HEIGHT, NODE_WIDTH, portsOf, type Positions, type Workflow } from './model';

const nodeTypes = { wf: FlowNode };

export function RunCanvas(props: {
  workflow: Workflow;
  positions?: Positions;
  nodes: NodeView[];
  catalog: NodeTypeInfo[];
  selected?: string;
  focus?: string; // 初始视野居中的节点（卡住或正在等待的节点）；为空时显示全图
  onSelect: (id?: string) => void;
}) {
  const { workflow: wf, catalog } = props;
  const info = useMemo(() => Object.fromEntries(catalog.map((c) => [c.type, c])), [catalog]);
  const state = useMemo(() => Object.fromEntries(props.nodes.map((n) => [n.node_id, n])), [props.nodes]);
  // 设计时保存的坐标缺节点（如旧版本没有坐标）时整体自动排版。
  const positions = useMemo(() => {
    const p = props.positions;
    return p && wf.nodes.every((n) => p[n.id]) ? p : autoLayout(wf);
  }, [wf, props.positions]);
  // 受控模式下节点尺寸要回填，否则连线不显示。
  const [measured, setMeasured] = useState<Record<string, { width: number; height: number }>>({});

  const portsByNode = Object.fromEntries(wf.nodes.map((n) => [n.id, portsOf(n, info[n.type])]));

  const nodes: Node[] = wf.nodes.map((n) => {
    const t = info[n.type];
    const row = state[n.id];
    const s = row?.status === 'pending' ? undefined : row; // pending = 还没走到，与没有记录一样淡化
    const detail = [
      s?.port && portsByNode[n.id]?.length > 1 ? `出口 ${s.port}` : '',
      s && s.attempt > 1 ? `执行 ${s.attempt} 次` : '',
    ]
      .filter(Boolean)
      .join(' · ');
    const data: FlowNodeData = {
      id: n.id,
      typeLabel: t?.display_name ?? n.type,
      name: n.name,
      summary: '',
      ports: portsByNode[n.id] ?? [],
      category: t?.category ?? 'native',
      hasIssue: false,
      isEntry: n.type === 'trigger',
      run: { status: s?.status, label: s ? (nodeStatusLabel[s.status] ?? s.status) : '未到达', detail: detail || undefined },
    };
    return { id: n.id, type: 'wf', position: positions[n.id] ?? { x: 0, y: 0 }, measured: measured[n.id], data, selected: props.selected === n.id };
  });

  const edges: Edge[] = wf.edges.map((e) => {
    const from = state[e.from];
    const taken = !!from && (from.status === 'succeeded' || from.status === 'skipped') && from.port === e.port;
    return {
      id: e.id,
      source: e.from,
      sourceHandle: portsByNode[e.from]?.includes(e.port) ? e.port : null,
      target: e.to,
      targetHandle: 'in',
      markerEnd: { type: MarkerType.ArrowClosed, color: taken ? '#1677ff' : '#d9d9d9' },
      style: { stroke: taken ? '#1677ff' : '#d9d9d9', strokeWidth: taken ? 2 : 1.2, strokeDasharray: taken ? undefined : '4 4' },
    };
  });

  return (
    <ReactFlow
      nodes={nodes}
      edges={edges}
      nodeTypes={nodeTypes}
      onNodesChange={(changes) => {
        const sizes: Record<string, { width: number; height: number }> = {};
        for (const c of changes) {
          if (c.type === 'dimensions' && c.dimensions) sizes[c.id] = c.dimensions;
          if (c.type === 'select' && c.selected) props.onSelect(c.id);
        }
        if (Object.keys(sizes).length) setMeasured((m) => ({ ...m, ...sizes }));
      }}
      onPaneClick={() => props.onSelect(undefined)}
      onInit={(rf) => {
        // 长流程整图缩小后看不清：有焦点节点时以可读的比例居中到它，滚轮平移查看其余部分。
        const p = props.focus ? positions[props.focus] : undefined;
        if (p) void rf.setCenter(p.x + NODE_WIDTH / 2, p.y + NODE_HEIGHT / 2, { zoom: 0.9 });
        else void rf.fitView({ padding: 0.15 });
      }}
      panOnScroll
      nodesDraggable={false}
      nodesConnectable={false}
      edgesFocusable={false}
      deleteKeyCode={null}
      minZoom={0.2}
      proOptions={{ hideAttribution: true }}
    >
      <Background gap={16} />
      <Controls showInteractive={false} />
    </ReactFlow>
  );
}
