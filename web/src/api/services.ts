// 各 service 的调用封装，路径与 workbench.proto 行尾注释一致。
import { call } from './client';
import type {
  Draft,
  DryRunResp,
  GetTaskTypeResp,
  Issue,
  ListRunsResp,
  ListTasksResp,
  NodeTypeInfo,
  PageReq,
  RunEventsResp,
  RunView,
  SignalResp,
  Skill,
  TaskDetail,
  TaskEventsResp,
  TaskPriority,
  TaskStatus,
  TaskTypeSummary,
  TaskView,
  VersionInfo,
} from './types';

export const taskApi = {
  listMine: (req: { statuses?: TaskStatus[]; priorities?: TaskPriority[]; task_type_key?: string; page?: PageReq }) =>
    call<ListTasksResp>('task/my/list', req),
  listClaimable: (req: { priorities?: TaskPriority[]; task_type_key?: string; page?: PageReq }) =>
    call<ListTasksResp>('task/claimable/list', req),
  get: (id: string) => call<TaskDetail>('task/get', { id }),
  events: (id: string, page?: PageReq) => call<TaskEventsResp>('task/events', { id, page }),
  claim: (id: string) => call<TaskView>('task/claim', { id }),
  startProcessing: (id: string) => call<TaskView>('task/start', { id }),
  updatePriority: (id: string, priority: TaskPriority, reason: string) =>
    call<TaskView>('task/priority/update', { id, priority, reason }),
  reassign: (id: string, assignee_id: string, reason: string) => call<TaskView>('task/reassign', { id, assignee_id, reason }),
};

export const runApi = {
  signal: (req: { run_id: string; node_id: string; iteration: number; option: string; form: Record<string, string> }) =>
    call<SignalResp>('run/signal', req),
  list: (req: { task_id?: string; biz_key?: string; status?: string; task_type_key?: string; page?: PageReq }) =>
    call<ListRunsResp>('run/list', req),
  get: (run_id: string) => call<RunView>('run/get', { run_id }),
  events: (run_id: string, page?: PageReq) => call<RunEventsResp>('run/events', { run_id, page }),
  cancel: (run_id: string, reason: string) => call<Record<string, never>>('run/cancel', { run_id, reason }),
  retryNode: (run_id: string, node_id: string, reason: string) =>
    call<Record<string, never>>('run/node/retry', { run_id, node_id, reason }),
  skipNode: (run_id: string, node_id: string, port: string, reason: string) =>
    call<Record<string, never>>('run/node/skip', { run_id, node_id, port, reason }),
};

export const taskTypeApi = {
  list: (req: { namespace?: string; keyword?: string } = {}) =>
    call<{ items: TaskTypeSummary[]; next_cursor: string }>('task_type/list', req),
  get: (key: string, version = 0) => call<GetTaskTypeResp>('task_type/get', { key, version }),
  versions: (key: string) => call<{ items: VersionInfo[] }>('task_type/versions', { key }),
  dryRun: (req: { key: string; sample_json: string[]; choices: Record<string, string> }) =>
    call<DryRunResp>('task_type/draft/dry_run', req),
  create: (req: { key: string; name: string; namespace: string; description?: string }) =>
    call<{ key: string; draft: Draft }>('task_type/create', req),
  getDraft: (key: string) => call<{ draft: Draft }>('task_type/draft/get', { key }),
  saveDraft: (key: string, revision: string, body_json: string) =>
    call<{ revision: string; issues: Issue[] }>('task_type/draft/save', { key, revision, body_json }),
  validateDraft: (key: string) => call<{ ok: boolean; issues: Issue[] }>('task_type/draft/validate', { key }),
  discardDraft: (key: string, revision: string) => call<Record<string, never>>('task_type/draft/discard', { key, revision }),
  publishDraft: (key: string, revision: string) =>
    call<{ version: number; issues: Issue[] }>('task_type/draft/publish', { key, revision }),
};

export const catalogApi = {
  nodeTypes: () => call<{ items: NodeTypeInfo[] }>('catalog/node_types', {}),
  skills: (keyword = '') => call<{ items: Skill[]; next_cursor: string }>('catalog/skills', { keyword }),
};
