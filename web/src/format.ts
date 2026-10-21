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

/** 任务完结的结果。 */
export const resolutionLabel: Record<string, string> = {
  handled: '已处理',
  no_response: '无需处理',
  auto_closed: '自动关闭',
};

/** 动作的中文名（历史记录用）。 */
export const historyLabel: Record<string, string> = {
  created: '创建',
  claimed: '认领',
  started: '开始处理',
  close: '已处理',
  no_response: '无需处理',
  snooze: '推迟',
  escalate: '升级',
  woke: '推迟到期',
  reminded: '无人认领，再次提醒群',
  gone: '数据源中消失，自动关闭',
  transferred: '转交',
  priority: '调整优先级',
};

/** 把 Unix 毫秒（int64 字符串）格式化为本地时间；0 或空为「—」。 */
export function fmtTime(ms: string | number | undefined | null, pattern = 'MM-DD HH:mm'): string {
  const n = Number(ms ?? 0);
  return n > 0 ? dayjs(n).format(pattern) : '—';
}

/** 把毫秒时长格式化为「x 天 y 小时」，不足 1 小时显示分钟。 */
export function fmtDuration(ms: string | number): string {
  const n = Number(ms);
  if (n < 3_600_000) return `${Math.round(n / 60_000)} 分钟`;
  const h = Math.round(n / 3_600_000);
  if (h < 24) return `${h} 小时`;
  const d = Math.floor(h / 24);
  return h % 24 ? `${d} 天 ${h % 24} 小时` : `${d} 天`;
}

/** 常见的 5 段 cron 转成中文；不认识的写法返回空，由调用方只显示原文。 */
export function cronText(expr: string | undefined): string {
  const f = (expr ?? '').trim().split(/\s+/);
  if (f.length !== 5) return '';
  const [min, hour, dom, mon, dow] = f;
  const daily = dom === '*' && mon === '*' && dow === '*';
  let m: RegExpMatchArray | null;
  if (daily && hour === '*' && (min === '*' || min === '*/1')) return '每分钟';
  if (daily && hour === '*' && (m = min.match(/^\*\/(\d+)$/))) return `每 ${m[1]} 分钟`;
  if (daily && hour === '*' && /^\d+$/.test(min)) return min === '0' ? '每小时整点' : `每小时第 ${min} 分`;
  const at = /^\d+$/.test(min) && /^\d+$/.test(hour) ? `${hour.padStart(2, '0')}:${min.padStart(2, '0')}` : '';
  if (!at) return '';
  if (daily) return `每天 ${at}`;
  if (dom === '*' && mon === '*' && dow === '1-5') return `工作日 ${at}`;
  return '';
}
