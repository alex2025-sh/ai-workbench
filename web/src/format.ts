import dayjs from 'dayjs';
import type { TaskPriority, TaskStatus } from './api/types';

export const taskStatusLabel: Record<TaskStatus, string> = {
  TASK_STATUS_UNSPECIFIED: '—',
  TASK_STATUS_UNASSIGNED: '待认领',
  TASK_STATUS_PENDING: '待处理',
  TASK_STATUS_PROCESSING: '处理中',
  TASK_STATUS_SNOOZED: '已推迟',
  TASK_STATUS_SOLVED: '已完成',
};

export const taskStatusColor: Record<TaskStatus, string> = {
  TASK_STATUS_UNSPECIFIED: 'default',
  TASK_STATUS_UNASSIGNED: 'purple',
  TASK_STATUS_PENDING: 'blue',
  TASK_STATUS_PROCESSING: 'cyan',
  TASK_STATUS_SNOOZED: 'gold',
  TASK_STATUS_SOLVED: 'default',
};

export const priorityLabel: Record<TaskPriority, string> = {
  TASK_PRIORITY_P0: 'P0 立即',
  TASK_PRIORITY_P1: 'P1 今日',
  TASK_PRIORITY_P2: 'P2 机会',
  TASK_PRIORITY_P3: 'P3 维护',
};

export const priorityColor: Record<TaskPriority, string> = {
  TASK_PRIORITY_P0: '#cf1322',
  TASK_PRIORITY_P1: '#d46b08',
  TASK_PRIORITY_P2: '#1677ff',
  TASK_PRIORITY_P3: '#8c8c8c',
};

export const runStatusLabel: Record<string, string> = {
  running: '运行中',
  succeeded: '已完成',
  failed: '失败',
  needs_intervention: '需人工介入',
  cancelled: '已取消',
};

export const runStatusColor: Record<string, string> = {
  running: 'processing',
  succeeded: 'success',
  failed: 'error',
  needs_intervention: 'warning',
  cancelled: 'default',
};

export const nodeStatusLabel: Record<string, string> = {
  pending: '未开始',
  ready: '就绪',
  running: '执行中',
  waiting: '等待中',
  succeeded: '已完成',
  skipped: '已跳过',
  failed: '失败',
  cancelled: '已取消',
};

/** 把 Unix 毫秒（int64 字符串）格式化为本地时间；0 或空为「—」。 */
export function fmtTime(ms: string | number | undefined | null, pattern = 'MM-DD HH:mm'): string {
  const n = Number(ms ?? 0);
  return n > 0 ? dayjs(n).format(pattern) : '—';
}

/** 把毫秒时长格式化为「x 天 y 小时」。 */
export function fmtDuration(ms: string | number): string {
  const h = Math.round(Number(ms) / 3_600_000);
  if (h < 24) return `${h} 小时`;
  const d = Math.floor(h / 24);
  return h % 24 ? `${d} 天 ${h % 24} 小时` : `${d} 天`;
}
