// 定义画布（只读）：已发布版本的 workflow，节点显示类型、名称与一行摘要；点击节点选中，
// 选中节点的进出连线高亮。坐标取设计时保存的 ui.canvas.positions，缺节点时整体自动排版。
import { Background, Controls, MarkerType, ReactFlow, type Edge, type Node, type ReactFlowInstance } from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { useEffect, useMemo, useRef, useState } from 'react';
import type { NodeTypeInfo } from '../../api/types';
import { FlowNode, type FlowNodeData } from './FlowNode';
import { autoLayout, NODE_HEIGHT, NODE_WIDTH, portsOf, summaryOf, type Positions, type Workflow } from './model';

const nodeTypes = { wf: FlowNode };

export function DefinitionCanvas(props: {
  workflow: Workflow;
  positions?: Positions;
  catalog: NodeTypeInfo[];
  selected?: string;
  /** 从页面其他位置跳到某个节点时设置（at 每次不同），画布平移到该节点。 */
  focus?: { id: string; at: number };
  onSelect: (id?: string) => void;
}) {
  const { workflow: wf } = props;
  const info = useMemo(() => Object.fromEntries(props.catalog.map((c) => [c.type, c])), [props.catalog]);
  const positions = useMemo(() => {
    const p = props.positions;
    return p && wf.nodes.every((n) => p[n.id]) ? p : autoLayout(wf);
  }, [wf, props.positions]);
  // 受控模式下节点尺寸要回填，否则连线不显示。
  const [measured, setMeasured] = useState<Record<string, { width: number; height: number }>>({});
  const rf = useRef<ReactFlowInstance>();
  useEffect(() => {
    const p = props.focus && positions[props.focus.id];
    if (p) void rf.current?.setCenter(p.x + NODE_WIDTH / 2, p.y + NODE_HEIGHT / 2, { zoom: 1, duration: 300 });
  }, [props.focus]); // 只在跳转请求变化时平移
  const portsByNode = useMemo(() => Object.fromEntries(wf.nodes.map((n) => [n.id, portsOf(n, info[n.type])])), [wf, info]);

  const nodes: Node[] = wf.nodes.map((n) => {
    const t = info[n.type];
    const data: FlowNodeData = {
      id: n.id,
      typeLabel: t?.display_name ?? n.type,
      name: n.name,
      summary: summaryOf(n),
      ports: portsByNode[n.id] ?? [],
      category: t?.category ?? 'native',
      hasIssue: false,
      isEntry: n.type === 'trigger',
    };
    return { id: n.id, type: 'wf', position: positions[n.id] ?? { x: 0, y: 0 }, measured: measured[n.id], data, selected: props.selected === n.id };
  });

  const edges: Edge[] = wf.edges.map((e) => {
    const on = !!props.selected && (e.from === props.selected || e.to === props.selected);
    const color = on ? '#1677ff' : e.kind === 'rework' ? '#fa8c16' : '#bfbfbf';
    return {
      id: e.id,
      source: e.from,
      sourceHandle: portsByNode[e.from]?.includes(e.port) ? e.port : null,
      target: e.to,
      targetHandle: 'in',
      markerEnd: { type: MarkerType.ArrowClosed, color },
      style: { stroke: color, strokeWidth: on ? 2 : 1.2, strokeDasharray: e.kind === 'rework' ? '6 4' : undefined },
      zIndex: on ? 1 : 0,
    };
  });

  return (
    <ReactFlow
      nodes={nodes}
      edges={edges}
      nodeTypes={nodeTypes}
      onNodesChange={(changes) => {
        const sizes: Record<string, { width: number; height: number }> = {};
        for (const c of changes) if (c.type === 'dimensions' && c.dimensions) sizes[c.id] = c.dimensions;
        if (Object.keys(sizes).length) setMeasured((m) => ({ ...m, ...sizes }));
      }}
      onNodeClick={(_, n) => props.onSelect(n.id)}
      onPaneClick={() => props.onSelect(undefined)}
      onInit={(inst) => {
        rf.current = inst;
        // 整图能放下时全图显示；长流程缩到看不清时，以可读比例从入口开始显示，其余拖动查看。
        void inst.fitView({ padding: 0.1 }).then(() => {
          if (inst.getZoom() >= 0.7) return;
          const entry = wf.nodes.find((n) => n.type === 'trigger') ?? wf.nodes[0];
          const p = entry && positions[entry.id];
          const xs = Object.values(positions).map((q) => q.x);
          if (p) void inst.setCenter((Math.min(...xs) + Math.max(...xs) + NODE_WIDTH) / 2, p.y + 220, { zoom: 0.8 });
        });
      }}
      // 画布嵌在可滚动的页面里：滚轮滚动页面，拖动平移画布，缩放用控件或双指。
      zoomOnScroll={false}
      preventScrolling={false}
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
