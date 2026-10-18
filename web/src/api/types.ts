// app/rpc/workbench.proto 中前端用到的消息（JSON 形态：snake_case，int64 为字符串，bytes 为 base64）。

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
  code: string;
  severity: string;
  path: string;
  node_id: string;
  edge_id: string;
  message: string;
}

// ---- TaskService ----

export interface TaskSummary {
  id: Int64;
  task_type_key: string;
  title: string;
  subtitle: string;
  status: TaskStatus;
  priority: TaskPriority;
  assignee_id: string;
  sla_status: string;
  snooze_until: Int64;
  due_at: Int64;
  updated_at: Int64;
}

export interface ListTasksResp {
  items: TaskSummary[];
  next_cursor: string;
}

export interface TaskView {
  id: Int64;
  task_type_key: string;
  task_type_version: number;
  status: TaskStatus;
  priority: TaskPriority;
  assignee_id: string;
  current_node: string;
  resolution: string;
  sla_status: string;
  snooze_until: Int64;
  due_at: Int64;
  payload_json: Bytes;
  run_id: Int64;
  updated_at: Int64;
  title: string;
  subtitle: string;
}

export interface ActionOption {
  key: string;
  label: string;
  style: string;
  required_fields: string[];
  snooze_ms: Int64;
}

export interface FormField {
  key: string;
  label: string;
  type: string;
  options: string[];
}

export interface PendingAction {
  node_id: string;
  iteration: number;
  can_signal: boolean;
  options: ActionOption[];
  deadline_at: Int64;
  form_fields: FormField[];
}

export interface ProgressStep {
  node_id: string;
  display_name: string;
  status: string;
}

export interface TaskDetail {
  task: TaskView;
  rendered_json: Bytes;
  pending_action: PendingAction | null;
  progress: ProgressStep[];
}

export interface TaskEvent {
  id: Int64;
  type: string;
  from_status: TaskStatus;
  to_status: TaskStatus;
  actor: string;
  reason: string;
  detail_json: Bytes;
  created_at: Int64;
}

export interface TaskEventsResp {
  items: TaskEvent[];
  next_cursor: string;
}

// ---- RunService ----

export interface SignalResp {
  accepted: boolean;
  reason: string;
  /** already_handled / stale / not_waiting / invalid_form / unsupported / forbidden */
  code: string;
}

export interface RunSummary {
  run_id: Int64;
  task_type_key: string;
  task_type_version: number;
  status: string;
  created_at: Int64;
  updated_at: Int64;
}

export interface ListRunsResp {
  items: RunSummary[];
  next_cursor: string;
}

export interface NodeView {
  node_id: string;
  iteration: number;
  status: string;
  port: string;
  attempt: number;
  started_at: Int64;
  finished_at: Int64;
}

export interface RunView {
  run_id: Int64;
  task_type_key: string;
  task_type_version: number;
  status: string;
  nodes: NodeView[];
  graph_json: Bytes;
}

export interface RunEvent {
  id: Int64;
  node_id: string;
  iteration: number;
  type: string;
  payload_json: Bytes;
  actor: string;
  created_at: Int64;
}

export interface RunEventsResp {
  items: RunEvent[];
  next_cursor: string;
}

// ---- TaskTypeService ----

export interface TaskTypeSummary {
  key: string;
  name: string;
  namespace: string;
  latest_version: number;
  has_draft: boolean;
  status: string;
  updated_at: Int64;
}

export interface VersionInfo {
  version: number;
  content_hash: string;
  state: string;
  change_set_id: Int64;
  published_by: string;
  approved_by: string;
  published_at: Int64;
}

export interface GetTaskTypeResp {
  summary: TaskTypeSummary;
  version: VersionInfo;
  body_json: Bytes;
}

export interface DryRunStep {
  node_id: string;
  iteration: number;
  port: string;
  status: string;
}

export interface DryRunSend {
  node_id: string;
  kind: string;
  target: string;
  summary: string;
  rendered_json: Bytes;
}

export interface DryRunTimePoint {
  node_id: string;
  kind: string;
  offset_ms: Int64;
}

export interface DryRunCase {
  sample_index: number;
  path: DryRunStep[];
  assignee_id: string;
  sends: DryRunSend[];
  timeline: DryRunTimePoint[];
  end_status: string;
  issues: Issue[];
}

export interface DryRunResp {
  revision: Int64;
  content_hash: string;
  passed: boolean;
  cases: DryRunCase[];
  issues: Issue[];
}

// ---- 渲染后的卡片（rendered_json 解码后，与 Lark 卡片同一结构，见 internal/action.Card） ----

export interface Card {
  title: string;
  color?: string;
  summary?: string;
  fields: { label: string; value: string; short?: boolean }[] | null;
  links?: { label: string; url: string }[] | null;
}

// ---- 任务类型 JSON 中前端用到的部分 ----

export interface TaskTypeBody {
  key: string;
  name: string;
  description?: string;
  schema?: { properties?: Record<string, { type?: string; title?: string }> };
  workflow?: {
    nodes: { id: string; type: string; name?: string; config?: { options?: { key: string; label: string; snooze?: string }[] } }[];
  };
}
