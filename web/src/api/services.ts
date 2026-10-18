// 各 service 的调用封装，路径与 workbench.proto 行尾注释一致。
import { call } from './client';
import type {
  DryRunResp,
  GetTaskTypeResp,
  ListRunsResp,
  ListTasksResp,
  PageReq,
  RunEventsResp,
  RunView,
  SignalResp,
  TaskDetail,
  TaskEventsResp,
  TaskPriority,
  TaskStatus,
  TaskTypeSummary,
  VersionInfo,
} from './types';

export const taskApi = {
  listMine: (req: { statuses?: TaskStatus[]; priorities?: TaskPriority[]; task_type_key?: string; page?: PageReq }) =>
    call<ListTasksResp>('task/my/list', req),
  listClaimable: (req: { priorities?: TaskPriority[]; task_type_key?: string; page?: PageReq }) =>
    call<ListTasksResp>('task/claimable/list', req),
  get: (id: string) => call<TaskDetail>('task/get', { id }),
  events: (id: string, page?: PageReq) => call<TaskEventsResp>('task/events', { id, page }),
};

export const runApi = {
  signal: (req: { run_id: string; node_id: string; iteration: number; option: string; form: Record<string, string> }) =>
    call<SignalResp>('run/signal', req),
  list: (req: { task_id?: string; biz_key?: string; status?: string; task_type_key?: string; page?: PageReq }) =>
    call<ListRunsResp>('run/list', req),
  get: (run_id: string) => call<RunView>('run/get', { run_id }),
  events: (run_id: string, page?: PageReq) => call<RunEventsResp>('run/events', { run_id, page }),
};

export const taskTypeApi = {
  list: (req: { namespace?: string; keyword?: string } = {}) =>
    call<{ items: TaskTypeSummary[]; next_cursor: string }>('task_type/list', req),
  get: (key: string, version = 0) => call<GetTaskTypeResp>('task_type/get', { key, version }),
  versions: (key: string) => call<{ items: VersionInfo[] }>('task_type/versions', { key }),
  dryRun: (req: { key: string; sample_json: string[]; choices: Record<string, string> }) =>
    call<DryRunResp>('task_type/draft/dry_run', req),
};
