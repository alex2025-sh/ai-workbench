// 各 service 的调用封装，路径与 workbench.proto 行尾注释一致。
import { call } from './client';
import type {
  Card,
  DraftResp,
  GetTaskTypeResp,
  Issue,
  ListTasksResp,
  PageReq,
  Scope,
  TaskDetail,
  TaskPriority,
  TaskStatus,
  TaskSummary,
  TaskTypeSummary,
  TrialSqlResp,
  VersionInfo,
} from './types';

export const taskApi = {
  list: (req: { scope: Scope; statuses?: TaskStatus[]; priorities?: TaskPriority[]; task_type?: string; page?: PageReq }) =>
    call<ListTasksResp>('task/list', req),
  get: (id: string) => call<TaskDetail>('task/get', { id }),
  start: (id: string) => call<TaskSummary>('task/start', { id }),
  act: (req: { id: string; action: string; snooze_ms?: string; note?: string; expect_node?: string }) => call<TaskSummary>('task/act', req),
  reassign: (id: string, assignee_id: string, reason: string) => call<TaskSummary>('task/reassign', { id, assignee_id, reason }),
  priority: (id: string, priority: TaskPriority) => call<TaskSummary>('task/priority', { id, priority }),
};

export const taskTypeApi = {
  list: (req: { namespace?: string; keyword?: string } = {}) => call<{ items: TaskTypeSummary[] }>('task_type/list', req),
  get: (key: string, version = 0) => call<GetTaskTypeResp>('task_type/get', { key, version }),
  versions: (key: string) => call<{ items: VersionInfo[] }>('task_type/versions', { key }),
  create: (req: { key: string; name: string; namespace: string; description?: string }) => call<DraftResp>('task_type/create', req),
  getDraft: (key: string) => call<DraftResp>('task_type/draft/get', { key }),
  saveDraft: (key: string, revision: string, body_json: string) =>
    call<{ revision: string; issues: Issue[] }>('task_type/draft/save', { key, revision, body_json }),
  discardDraft: (key: string) => call<Record<string, never>>('task_type/draft/discard', { key }),
  validateDraft: (key: string) => call<{ issues: Issue[] }>('task_type/draft/validate', { key }),
  publish: (key: string) => call<{ version: VersionInfo | null; issues: Issue[] }>('task_type/draft/publish', { key }),
  trialSql: (datasource: string, query: string) => call<TrialSqlResp>('task_type/trial_sql', { datasource, query }),
  previewCard: (req: { body_json: string; node_id?: string; key: string; values_json: string }) =>
    call<{ card: Card | null; issues: Issue[] }>('task_type/preview_card', req),
};

export const catalogApi = {
  datasources: () => call<{ names: string[] }>('catalog/datasources', {}),
  relations: () => call<{ names: string[] }>('catalog/relations', {}),
};
