// 任务类型配置页与只读详情共用的小工具：问题定位、样例数据、选项文字。
import { Alert } from 'antd';
import type { ActionKey, FieldType, Issue, TaskTypeBody } from '../../api/types';

export const steps = [
  { key: 'source', title: '数据源', description: '任务从哪里来' },
  { key: 'fields', title: '字段', description: '任务带哪些数据' },
  { key: 'nodes', title: '节点', description: '谁处理、能做什么' },
  { key: 'card', title: '卡片', description: '处理人看到什么' },
] as const;

/** 问题路径 → 步骤下标。 */
export function stepOf(path: string): number {
  if (path.startsWith('fields')) return 1;
  if (path.startsWith('nodes')) return 2;
  if (path.startsWith('card')) return 3;
  return 0; // source、key、name 等基本信息
}

/** 某个路径前缀下的问题。 */
export function issuesAt(issues: Issue[], prefix: string): Issue[] {
  return issues.filter((i) => i.path === prefix || i.path.startsWith(prefix + '.'));
}

export function IssueList({ issues }: { issues: Issue[] }) {
  if (issues.length === 0) return null;
  return (
    <Alert
      type="error"
      showIcon
      style={{ marginBottom: 12 }}
      message={issues.length === 1 ? issues[0].message : `${issues.length} 个问题`}
      description={
        issues.length > 1 ? (
          <ul style={{ margin: 0, paddingInlineStart: 18 }}>
            {issues.map((i, n) => (
              <li key={n}>{i.message}</li>
            ))}
          </ul>
        ) : undefined
      }
    />
  );
}

export const fieldTypeLabel: Record<FieldType, string> = { text: '文本', number: '数字', money: '金额', datetime: '日期时间', enum: '枚举' };
export const piiLabel: Record<string, string> = { mask_middle: '中间打码', hash: '哈希', drop: '不显示' };
export const actionLabel: Record<ActionKey, string> = { close: '已处理', no_response: '无需处理', snooze: '推迟', link: '业务链接', escalate: '升级', transfer: '转交' };
export const actionHelp: Record<ActionKey, string> = {
  close: '任务完结，结果「已处理」',
  no_response: '任务完结，结果「无需处理」',
  snooze: '推迟，到期后回到待处理并重新通知',
  link: '打开业务系统链接，不改变任务状态',
  escalate: '交给下一个节点处理',
  transfer: '留在当前节点，交给另一个人（认领节点还可以放回待认领）',
};
export const allActions: ActionKey[] = ['close', 'no_response', 'snooze', 'link', 'escalate', 'transfer'];

/** 一行样例数据：优先用试跑 SQL 的第一行，否则按字段类型生成。 */
export function sampleValues(body: TaskTypeBody, row?: Record<string, unknown>): { key: string; values: Record<string, unknown> } {
  if (row) {
    const { key, ...values } = row;
    return { key: String(key ?? ''), values };
  }
  const values: Record<string, unknown> = {};
  for (const f of body.fields) {
    switch (f.type) {
      case 'number':
        values[f.name] = '3';
        break;
      case 'money':
        values[f.name] = '12345.60';
        break;
      case 'datetime':
        values[f.name] = String(Date.now() - 3_600_000);
        break;
      case 'enum':
        values[f.name] = f.options?.[0]?.value ?? '';
        break;
      default:
        values[f.name] = f.label ? `示例${f.label}` : 'sample';
    }
  }
  return { key: 'KEY-001', values };
}

/** 分配器的一行说明（画布与只读视图）。 */
export function assigneeText(body: TaskTypeBody, a: TaskTypeBody['nodes'][number]['assignee']): string {
  if (a.kind === 'relation') {
    const f = body.fields.find((x) => x.name === a.field);
    return `业务关系 ${a.relation || '?'}（${f?.label || a.field || '?'}）→ 兜底 ${a.fallback || '?'}`;
  }
  if (a.kind === 'claim') {
    return `群 ${a.chat || '?'} 认领${a.remind_after ? `，${a.remind_after} 无人认领再提醒` : ''}`;
  }
  const n = a.users?.length ?? 0;
  return n === 0 ? '随机：未填候选人' : n === 1 ? a.users![0] : `${n} 人中随机：${a.users!.join('、')}`;
}
