// workflow 设计态画布：左侧节点面板拖入，端口连线，选中后 Delete 或点删除按钮删除，拖动保存坐标，按校验问题描红。
// 数据完全由父组件持有（workflow 与坐标），这里只把操作翻译成新的 workflow / 坐标。
import {
  Background,
  Controls,
  MarkerType,
  MiniMap,
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
  type Connection,
  type Edge,
  type EdgeChange,
  type Node,
  type NodeChange,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { ApartmentOutlined } from '@ant-design/icons';
import { Button, Card, Input, Space, Tooltip, Typography } from 'antd';
import { useMemo, useRef, useState, type DragEvent } from 'react';
import type { NodeTypeInfo } from '../../api/types';
import { DeletableEdge, type EdgeData } from './DeletableEdge';
import { FlowNode, type FlowNodeData } from './FlowNode';
import { autoLayout, defaultConfig, NODE_HEIGHT, NODE_WIDTH, portsOf, summaryOf, uniqueId, type Positions, type WfEdge, type WfNode, type Workflow } from './model';

const DRAG_TYPE = 'application/wb-node-type';
const nodeTypes = { wf: FlowNode };
const edgeTypes = { wf: DeletableEdge };

export interface CanvasProps {
  workflow: Workflow;
  positions: Positions;
  catalog: NodeTypeInfo[];
  issueNodes: Set<string>;
  issueEdges: Set<string>;
  selected?: string;
  onSelect: (id?: string) => void;
  onChange: (wf: Workflow, positions: Positions) => void;
}

export function WorkflowCanvas(props: CanvasProps) {
  return (
    <ReactFlowProvider>
      <Inner {...props} />
    </ReactFlowProvider>
  );
}

function Inner(props: CanvasProps) {
  const { workflow: wf, positions, catalog } = props;
  const info = useMemo(() => Object.fromEntries(catalog.map((c) => [c.type, c])), [catalog]);
  const { screenToFlowPosition, fitView } = useReactFlow();
  // 受控模式下节点尺寸要回填，否则节点不算初始化完成、连线不显示。
  const [measured, setMeasured] = useState<Record<string, { width: number; height: number }>>({});
  // 连线的选中状态由这里持有：受控模式下不回填选中，连线点了也选不中，Delete 就删不掉。
  const [selectedEdge, setSelectedEdge] = useState<string>();

  const nodes: Node[] = wf.nodes.map((n) => {
    const t = info[n.type];
    const data: FlowNodeData = {
      id: n.id,
      typeLabel: t?.display_name ?? n.type,
      name: n.name,
      summary: summaryOf(n),
      ports: portsOf(n, t),
      category: t?.category ?? 'native',
      hasIssue: props.issueNodes.has(n.id),
      isEntry: n.type === 'trigger',
    };
    return {
      id: n.id,
      type: 'wf',
      position: positions[n.id] ?? { x: 0, y: 0 },
      measured: measured[n.id],
      data,
      selected: props.selected === n.id,
      deletable: n.type !== 'trigger',
    };
  });

  const portsByNode = Object.fromEntries(wf.nodes.map((n) => [n.id, portsOf(n, info[n.type])]));
  const edges: Edge[] = wf.edges.map((e) => {
    // 端口已不存在（如删掉了人工节点的某个选项）的连线标红，等用户处理。
    const broken = !portsByNode[e.from]?.includes(e.port) || !portsByNode[e.to];
    const bad = broken || props.issueEdges.has(e.id);
    const rework = e.kind === 'rework';
    const data: EdgeData = { onRemove: rework ? undefined : removeEdge };
    return {
      id: e.id,
      type: 'wf',
      data,
      selected: selectedEdge === e.id,
      source: e.from,
      sourceHandle: portsByNode[e.from]?.includes(e.port) ? e.port : null,
      target: e.to,
      targetHandle: 'in',
      label: rework ? `退回（${e.port}）` : undefined,
      animated: rework,
      deletable: !rework,
      markerEnd: { type: MarkerType.ArrowClosed },
      style: { stroke: bad ? '#ff4d4f' : rework ? '#fa8c16' : '#8c8c8c', strokeWidth: bad ? 2 : 1.5, strokeDasharray: rework ? '6 4' : undefined },
    };
  });

  const emit = (next: Workflow, pos: Positions = positions) => props.onChange(next, pos);

  function removeEdge(id: string) {
    emit({ ...wf, edges: wf.edges.filter((e) => e.id !== id) });
    setSelectedEdge(undefined);
  }

  const onEdgesChange = (changes: EdgeChange[]) => {
    for (const c of changes) {
      if (c.type !== 'select') continue;
      if (c.selected) {
        setSelectedEdge(c.id);
        props.onSelect(undefined);
      } else {
        setSelectedEdge((cur) => (cur === c.id ? undefined : cur));
      }
    }
  };

  const onNodesChange = (changes: NodeChange[]) => {
    let pos = positions;
    const sizes: Record<string, { width: number; height: number }> = {};
    for (const c of changes) {
      if (c.type === 'position' && c.position) pos = { ...pos, [c.id]: { x: Math.round(c.position.x), y: Math.round(c.position.y) } };
      if (c.type === 'dimensions' && c.dimensions) sizes[c.id] = c.dimensions;
      if (c.type === 'select' && c.selected) {
        props.onSelect(c.id);
        setSelectedEdge(undefined);
      }
    }
    if (Object.keys(sizes).length) setMeasured((m) => ({ ...m, ...sizes }));
    if (pos !== positions) emit(wf, pos);
  };

  const onConnect = (c: Connection) => {
    if (!c.source || !c.target || !c.sourceHandle || c.source === c.target) return;
    if (wf.edges.some((e) => e.from === c.source && e.port === c.sourceHandle && e.to === c.target)) return;
    const edge: WfEdge = { id: uniqueId('e', wf.edges.map((e) => e.id)), from: c.source, port: c.sourceHandle, to: c.target };
    emit({ ...wf, edges: [...wf.edges, edge] });
  };

  const onDelete = ({ nodes: ns, edges: es }: { nodes: Node[]; edges: Edge[] }) => {
    const gone = new Set(ns.map((n) => n.id));
    const goneEdges = new Set(es.map((e) => e.id));
    const pos = Object.fromEntries(Object.entries(positions).filter(([id]) => !gone.has(id)));
    emit(
      {
        nodes: wf.nodes.filter((n) => !gone.has(n.id)),
        edges: wf.edges.filter((e) => !goneEdges.has(e.id) && !gone.has(e.from) && !gone.has(e.to)),
      },
      pos,
    );
    if (props.selected && gone.has(props.selected)) props.onSelect(undefined);
    if (selectedEdge && goneEdges.has(selectedEdge)) setSelectedEdge(undefined);
  };

  const pane = useRef<HTMLDivElement>(null);

  const add = (type: string, at: { x: number; y: number }) => {
    const p = screenToFlowPosition(at);
    const id = uniqueId(type, wf.nodes.map((n) => n.id));
    const node: WfNode = { id, type };
    const cfg = defaultConfig(type);
    if (cfg) node.config = cfg;
    emit({ ...wf, nodes: [...wf.nodes, node] }, { ...positions, [id]: { x: Math.round(p.x), y: Math.round(p.y) } });
    props.onSelect(id);
  };

  const onDrop = (ev: DragEvent) => {
    ev.preventDefault();
    const type = ev.dataTransfer.getData(DRAG_TYPE);
    if (type) add(type, { x: ev.clientX, y: ev.clientY });
  };

  // 点击面板中的节点：加在当前视野中央（与拖入效果相同）。
  const addAtCenter = (type: string) => {
    const r = pane.current?.getBoundingClientRect();
    if (r) add(type, { x: r.left + r.width / 2 - NODE_WIDTH / 2, y: r.top + r.height / 2 - NODE_HEIGHT / 2 });
  };

  const relayout = () => {
    emit(wf, autoLayout(wf));
    window.setTimeout(() => fitView({ padding: 0.2 }), 50);
  };

  return (
    <div style={{ display: 'flex', height: '100%', gap: 12 }}>
      <Palette catalog={catalog} onAdd={addAtCenter} />
      <div ref={pane} style={{ flex: 1, border: '1px solid #f0f0f0', borderRadius: 8, background: '#fafafa' }}>
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          edgeTypes={edgeTypes}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onConnect={onConnect}
          onDelete={onDelete}
          onPaneClick={() => {
            props.onSelect(undefined);
            setSelectedEdge(undefined);
          }}
          onDragOver={(ev) => {
            ev.preventDefault();
            ev.dataTransfer.dropEffect = 'move';
          }}
          onDrop={onDrop}
          deleteKeyCode={['Delete', 'Backspace']}
          fitView
          fitViewOptions={{ padding: 0.2 }}
          minZoom={0.2} // 节点多、纵向长的流程也能整体显示
          proOptions={{ hideAttribution: true }}
        >
          <Background gap={16} />
          <Controls />
          <MiniMap pannable zoomable style={{ width: 140, height: 90 }} />
          <div style={{ position: 'absolute', right: 160, bottom: 12, zIndex: 5 }}>
            <Tooltip title="按连线自上而下重新排版">
              <Button size="small" icon={<ApartmentOutlined />} onClick={relayout}>
                自动布局
              </Button>
            </Tooltip>
          </div>
        </ReactFlow>
      </div>
    </div>
  );
}

function Palette({ catalog, onAdd }: { catalog: NodeTypeInfo[]; onAdd: (type: string) => void }) {
  const [q, setQ] = useState('');
  const groups: [string, string][] = [
    ['native', '流程控制'],
    ['action', '动作'],
  ];
  const visible = catalog.filter((t) => t.type !== 'trigger' && (!q || t.display_name.includes(q) || t.type.includes(q)));
  return (
    <Card size="small" title="节点" style={{ width: 170, overflow: 'auto' }} styles={{ body: { padding: 8 } }}>
      <Input size="small" allowClear placeholder="搜索" value={q} onChange={(e) => setQ(e.target.value)} style={{ marginBottom: 8 }} />
      {groups.map(([cat, label]) => (
        <div key={cat} style={{ marginBottom: 8 }}>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {label}
          </Typography.Text>
          <Space direction="vertical" size={4} style={{ width: '100%', marginTop: 4 }}>
            {visible
              .filter((t) => t.category === cat)
              .map((t) => (
                <Tooltip key={t.type} title={`${t.description}（点击或拖入画布）`} placement="right">
                  <div
                    draggable
                    onClick={() => onAdd(t.type)}
                    onDragStart={(ev) => {
                      ev.dataTransfer.setData(DRAG_TYPE, t.type);
                      ev.dataTransfer.effectAllowed = 'move';
                    }}
                    style={{ padding: '4px 8px', border: '1px solid #d9d9d9', borderRadius: 4, background: '#fff', cursor: 'grab', fontSize: 13 }}
                  >
                    {t.display_name}
                  </div>
                </Tooltip>
              ))}
          </Space>
        </div>
      ))}
    </Card>
  );
}
