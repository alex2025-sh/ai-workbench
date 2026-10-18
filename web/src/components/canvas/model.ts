// workflow 图（任务类型 JSON 中的 workflow）与画布之间的转换，以及端口、默认配置、自动排版。
// 端口规则与后端 internal/flow/dsl 的 Ports 一致；坐标存在任务类型 JSON 的 ui.canvas.positions（编译器忽略）。
import dagre from '@dagrejs/dagre';
import type { NodeTypeInfo } from '../../api/types';

export interface WfNode {
  id: string;
  type: string;
  name?: string;
  config?: Record<string, unknown>;
  retry?: unknown;
  side_effects?: unknown;
}

export interface WfEdge {
  id: string;
  from: string;
  port: string;
  to: string;
  kind?: string; // rework：退回边（受控环），画布只显示、不能新建
  [extra: string]: unknown;
}

export interface Workflow {
  nodes: WfNode[];
  edges: WfEdge[];
}

export type Positions = Record<string, { x: number; y: number }>;

export const NODE_WIDTH = 200;
export const NODE_HEIGHT = 72;

/** 节点的输出端口，规则同 dsl.Ports。 */
export function portsOf(n: WfNode, info?: NodeTypeInfo): string[] {
  const cfg = (n.config ?? {}) as Record<string, any>;
  switch (info?.port_rule) {
    case 'from_branches':
      return [...((cfg.branches ?? []) as { port?: string }[]).map((b) => b.port ?? '').filter(Boolean), 'default'];
    case 'from_options': {
      const ports = ((cfg.options ?? []) as { key?: string; snooze?: string }[])
        .filter((o) => !o.snooze && o.key)
        .map((o) => o.key as string);
      return cfg.timeout ? [...ports, 'timeout'] : ports;
    }
    default:
      return info?.fixed_ports ?? [];
  }
}

/** 新拖入节点的默认配置：满足 schema 必填项、能直接通过校验的最小内容。 */
export function defaultConfig(type: string): Record<string, unknown> | undefined {
  switch (type) {
    case 'condition':
      return { branches: [{ port: 'yes', when: 'true' }] };
    case 'timer':
      return { after: '1h' };
    case 'human_choice':
      return { options: [{ key: 'done', label: '已处理', style: 'primary' }] };
    case 'skill_call':
      return { skill: 'appeal.get_status', input: {} };
    case 'llm':
      return { prompt: 'summary_prompt@1' };
    case 'send_email':
      return { to: ['{{ task.assignee }}'], subject: '通知' };
    case 'emit_event':
      return { event_type: 'workbench.event' };
    case 'merge':
      return { mode: 'all' };
    default:
      return undefined;
  }
}

/** 生成不重复的 id：前缀 + 序号。 */
export function uniqueId(prefix: string, taken: Iterable<string>): string {
  const used = new Set(taken);
  for (let i = 1; ; i++) {
    const id = `${prefix}_${i}`;
    if (!used.has(id)) return id;
  }
}

/** 用 dagre 自上而下排版，返回每个节点的左上角坐标。 */
export function autoLayout(wf: Workflow): Positions {
  const g = new dagre.graphlib.Graph();
  g.setGraph({ rankdir: 'TB', nodesep: 40, ranksep: 70 });
  g.setDefaultEdgeLabel(() => ({}));
  wf.nodes.forEach((n) => g.setNode(n.id, { width: NODE_WIDTH, height: NODE_HEIGHT }));
  wf.edges.filter((e) => e.kind !== 'rework').forEach((e) => g.setEdge(e.from, e.to));
  dagre.layout(g);
  const out: Positions = {};
  wf.nodes.forEach((n) => {
    const p = g.node(n.id);
    if (p) out[n.id] = { x: Math.round(p.x - NODE_WIDTH / 2), y: Math.round(p.y - NODE_HEIGHT / 2) };
  });
  return out;
}

/** 节点卡片上的一行摘要。 */
export function summaryOf(n: WfNode): string {
  const c = (n.config ?? {}) as Record<string, any>;
  switch (n.type) {
    case 'timer':
      return c.anchor ? `${c.after ?? ''}（从 ${c.anchor} 起）` : `${c.after ?? ''}`;
    case 'human_choice':
      return ((c.options ?? []) as { label?: string }[]).map((o) => o.label).join(' / ') + (c.timeout ? `，超时 ${c.timeout}` : '');
    case 'condition':
      return ((c.branches ?? []) as { port?: string }[]).map((b) => b.port).join(' / ');
    case 'skill_call':
      return c.skill ?? '';
    case 'lark_card':
    case 'send_email':
      return typeof c.view === 'string' ? `模板 ${c.view}` : '';
    case 'end':
      return c.resolution ? `结论 ${c.resolution}` : '';
    case 'merge':
      return c.mode === 'any' ? '任一到达' : '全部到达';
    default:
      return '';
  }
}
