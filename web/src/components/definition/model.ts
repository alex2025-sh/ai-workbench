// 定义页用到的任务类型 JSON 结构（只读，宽松类型；完整契约见 api/schema/task_type.schema.json）。
import type { Workflow } from '../canvas/model';

export interface SchemaProp {
  type?: string;
  title?: string;
  description?: string;
  enum?: unknown[];
  'x-pii'?: string;
  'x-format'?: string;
  'x-enum-labels'?: Record<string, string>;
  'x-currency-field'?: string;
  [k: string]: unknown;
}

export interface Source {
  id: string;
  kind: 'cron' | 'kafka' | 'api' | string;
  schedule?: string;
  timezone?: string;
  skill?: string;
  input?: Record<string, unknown>;
  event_type?: string;
  filter?: string;
  close_when?: string;
  mapping?: Record<string, string>;
}

export interface ViewTemplate {
  title: string;
  color?: string;
  summary?: string;
  fields: { label?: string; value: string; short?: boolean }[];
  links?: { label: string; url: string }[];
  ai_summary?: { enabled: boolean; [k: string]: unknown };
}

/** 卡片模板引用：views 中的名称、共享组件 `组件@版本`，或带受众变体的对象。 */
export type ViewBinding = string | { default: string; variants?: { when: string; view: string }[] };

export interface Resolver {
  type: string;
  [k: string]: unknown;
}

export interface Assignment {
  resolvers: Resolver[];
  filters?: string[];
  strategy?: string;
  exclude?: string[];
  fallback?: { group: string; strategy?: string };
}

/** 分配策略引用：内嵌策略或共享组件 `组件@版本`。 */
export type AssignmentBinding = string | Assignment;

export interface Definition {
  key: string;
  namespace?: string;
  name: string;
  description?: string;
  default_priority?: string;
  schema?: { required?: string[]; properties?: Record<string, SchemaProp> };
  sources?: Source[];
  dedup_key?: string;
  views?: Record<string, ViewTemplate | string>;
  view?: ViewBinding;
  assignment?: AssignmentBinding;
  workflow?: Workflow;
  ui?: { canvas?: { positions?: Record<string, { x: number; y: number }> } };
}

/** 编译后节点实际使用的配置；from 为 self、task_type 或提供配置的上游人工节点 id。 */
export interface Binding {
  view?: ViewBinding | null;
  view_from?: string;
  assignment?: AssignmentBinding | null;
  assignment_from?: string;
}

/** 卡片模板引用中出现的模板名（默认与各受众变体）。 */
export function viewNames(b: ViewBinding | null | undefined): string[] {
  if (!b) return [];
  if (typeof b === 'string') return [b];
  return [b.default, ...(b.variants ?? []).map((v) => v.view)];
}
