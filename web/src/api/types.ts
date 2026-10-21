// proto 消息的 JSON 形态（devgw / gateway 的 proto3 JSON 映射）：字段名 snake_case，int64 为字符串，bytes 为 base64。

export type Int64 = string;
export type Bytes = string;

export type TaskStatus =
  | 'TASK_STATUS_UNSPECIFIED'
  | 'TASK_STATUS_UNASSIGNED'
  | 'TASK_STATUS_PENDING'
  | 'TASK_STATUS_PROCESSING'
  | 'TASK_STATUS_SNOOZED'
  | 'TASK_STATUS_SOLVED';

export type TaskPriority = 'TASK_PRIORITY_P0' | 'TASK_PRIORITY_P1' | 'TASK_PRIORITY_P2' | 'TASK_PRIORITY_P3';

export interface PageReq {
  cursor?: string;
  page_size?: number;
}

export interface Issue {
  path: string;
  message: string;
}

// ---- 任务

export interface KV {
  label: string;
  value: string;
}

/** 渲染后的卡片：与 Lark 卡片同一份内容，已按字段的 pii 脱敏。 */
export interface Card {
  title: string;
  node_name: string;
  fields: KV[];
  link: KV | null;
}

export interface ActionOption {
  key: 'close' | 'no_response' | 'snooze' | 'escalate' | 'link';
  label: string;
  snooze_ms: Int64[];
  url: string;
  next_node_name: string;
}

export interface TaskSummary {
  id: Int64;
  task_type_key: string;
  task_type_name: string;
  task_type_version: number;
  biz_key: string;
  status: TaskStatus;
  priority: TaskPriority;
  assignee_id: string;
  node_id: string;
  resolution: string;
  snooze_until: Int64;
  created_at: Int64;
  updated_at: Int64;
  card: Card | null;
  actions: ActionOption[]; // 节点动作（不含转交，见 can_transfer）
  can_start: boolean;
  can_claim: boolean; // 待认领，调用人可以认领
  can_transfer: boolean;
  claim_node: boolean; // 当前节点是认领节点：转交时可以放回待认领
}

export interface TaskCount {
  priority: TaskPriority;
  status: TaskStatus;
  count: Int64;
}

export type Scope = 'mine' | 'claimable' | 'involved';

export interface ListTasksResp {
  items: TaskSummary[];
  next_cursor: string;
  counts: TaskCount[];
}

export interface NodeView {
  id: string;
  name: string;
  assignee: string;
  current: boolean;
  visited: boolean;
  actions: string[];
}

export interface HistoryItem {
  kind: string;
  actor: string;
  node_id: string;
  node_name: string;
  from_status: TaskStatus;
  to_status: TaskStatus;
  detail_json: Bytes;
  at: Int64;
}

export interface TaskDetail {
  task: TaskSummary;
  fields: KV[];
  nodes: NodeView[];
  history: HistoryItem[];
  can_update_priority: boolean;
  read_only: boolean;
  transfer_count: number;
}

// ---- 任务类型

export interface TaskTypeSummary {
  key: string;
  namespace: string;
  name: string;
  latest_version: number;
  has_draft: boolean;
  updated_at: Int64;
  source_kind: string;
}

export interface VersionInfo {
  version: number;
  content_hash: string;
  published_by: string;
  published_at: Int64;
}

export interface SourceState {
  next_run_at: Int64;
  last_run_at: Int64;
  last_rows: number;
  last_created: number;
  last_closed: number;
  last_error: string;
}

export interface GetTaskTypeResp {
  summary: TaskTypeSummary;
  version: VersionInfo;
  body_json: Bytes;
  source_state: SourceState | null;
}

export interface DraftResp {
  key: string;
  base_version: number;
  revision: Int64;
  body_json: Bytes;
  updated_by: string;
  issues: Issue[];
}

export interface TrialSqlResp {
  columns: string[];
  rows_json: Bytes;
  has_more: boolean;
}

// ---- 任务类型 JSON（与 api/schema/task_type.schema.json 一致）

export type SourceKind = 'sql' | 'api' | 'kafka';
export type FieldType = 'text' | 'number' | 'money' | 'datetime' | 'enum';
export type ActionKey = 'close' | 'no_response' | 'snooze' | 'link' | 'escalate' | 'transfer';

export interface TTField {
  name: string;
  label?: string;
  type?: FieldType;
  pii?: 'mask_middle' | 'hash' | 'drop';
  options?: { value: string; label: string }[];
}

/**
 * 处理人：relation 按业务关系查人，查不到交给 fallback；pool 从 users 中按 strategy 选一人；
 * claim 通知到 Lark 群 chat，任何人打开链接后认领，remind_after 后仍无人认领再提醒一次。
 */
export interface TTAssignee {
  kind: 'relation' | 'pool' | 'claim';
  relation?: string;
  field?: string;
  fallback?: string;
  users?: string[];
  strategy?: 'random';
  chat?: string;
  remind_after?: string;
}

export interface TTNode {
  id: string;
  name: string;
  assignee: TTAssignee;
  actions: ActionKey[];
  link?: { label: string; url: string };
  snooze?: string[];
  lark?: boolean;
}

export interface TaskTypeBody {
  key: string;
  namespace?: string;
  name: string;
  description?: string;
  priority?: 'P0' | 'P1' | 'P2' | 'P3';
  source: { kind: SourceKind; datasource?: string; schedule?: string; timezone?: string; query?: string };
  fields: TTField[];
  card: { title: string; fields?: string[] };
  nodes: TTNode[];
}
