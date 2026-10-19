// 任务工作台的卡片：一眼看清「是什么、多急、到哪一步」，常用操作不用进详情就能完成。
//
// - 标题行：优先级、状态、标题（当前显示的卡片的 title，与详情、Lark 一致）；逾期时右上角红色角标。
// - 正文：任务类型 · 截止或推迟至，加上卡片摘要（两行截断）。
// - 操作：查看；开始处理或认领（主按钮）；等待中人工节点的选项。选项要填表单时打开确认框，
//   推迟直接提交，其余选项二次确认后提交。按钮是否可点按当前身份判断，权限以服务端校验为准。
import { LinkOutlined, MoreOutlined } from '@ant-design/icons';
import { App as AntApp, Button, Card, Dropdown, Popconfirm, Tag, Tooltip, Typography } from 'antd';
import { useState, type ReactNode } from 'react';
import { useNavigate } from 'react-router-dom';
import { ApiError } from '../api/client';
import { runApi, taskApi } from '../api/services';
import type { ActionOption, TaskSummary } from '../api/types';
import { fmtDuration, fmtTime, priorityColor, taskStatusColor, taskStatusLabel } from '../format';
import { SignalModal } from './SignalModal';

/** 卡片上直接显示的流程选项个数，其余收进「更多」。 */
const INLINE_OPTIONS = 2;

export function isOverdue(t: TaskSummary, now = Date.now()): boolean {
  const due = Number(t.due_at);
  return due > 0 && due < now && t.status !== 'TASK_STATUS_SOLVED';
}

function optionLabel(o: ActionOption): string {
  return Number(o.snooze_ms) > 0 ? `${o.label}（${fmtDuration(o.snooze_ms)}）` : o.label;
}

/** 卡片底部等宽按钮：文字放不下时省略，悬停看全文。 */
const fill = { flex: '1 1 0', minWidth: 0 } as const;
function Label({ text }: { text: string }) {
  return <span style={{ display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{text}</span>;
}

export function TaskBoardCard(props: {
  task: TaskSummary;
  me: string;
  /** 认领、开始处理或流程操作提交后调用，由列表刷新所在分区（任务表由引擎异步同步，列表稍后才变化）。 */
  onChanged: (id: string, change: 'claimed' | 'started' | 'signaled') => void;
}) {
  const t = props.task;
  const navigate = useNavigate();
  const { message } = AntApp.useApp();
  const [busy, setBusy] = useState<string>();
  const [form, setForm] = useState<ActionOption>();
  const p = t.pending_action;
  const detail = `/tasks/${t.id}`;

  const run = async (key: string, label: string, fn: () => Promise<unknown>, change: 'claimed' | 'started') => {
    setBusy(key);
    try {
      await fn();
      message.success(`${label}成功`);
      props.onChanged(t.id, change);
    } catch (e) {
      message.error(e instanceof ApiError ? `${label}失败：${e.message}` : String(e));
    } finally {
      setBusy(undefined);
    }
  };

  // 不需要填表单的选项直接提交；被拒绝（已被别人处理、卡片过期）时提示并刷新。
  const signal = async (o: ActionOption) => {
    if (!p) return;
    setBusy(o.key);
    try {
      const res = await runApi.signal({ run_id: t.run_id, node_id: p.node_id, iteration: p.iteration, option: o.key, form: {} });
      if (res.accepted) {
        message.success(`已提交：${o.label}`);
        props.onChanged(t.id, 'signaled');
      } else {
        message.warning(res.reason || '该任务已被处理');
        props.onChanged(t.id, 'signaled');
      }
    } catch (e) {
      message.error(e instanceof ApiError ? `提交失败：${e.message}` : String(e));
    } finally {
      setBusy(undefined);
    }
  };

  const optionButton = (o: ActionOption): ReactNode => {
    const loading = busy === o.key;
    const disabled = !!busy && !loading;
    if (o.required_fields.length > 0) {
      return (
        <Tooltip key={o.key} title={`${o.label}：需要填写信息`}>
          <Button style={fill} disabled={disabled} danger={o.style === 'danger'} onClick={() => setForm(o)}>
            <Label text={o.label} />
          </Button>
        </Tooltip>
      );
    }
    if (Number(o.snooze_ms) > 0) {
      return (
        <Tooltip key={o.key} title={`推迟 ${fmtDuration(o.snooze_ms)}：到期后重新提醒，期间仍可直接处理`}>
          <Button style={fill} loading={loading} disabled={disabled} onClick={() => void signal(o)}>
            <Label text={o.label} />
          </Button>
        </Tooltip>
      );
    }
    return (
      <Popconfirm key={o.key} title={`确认「${o.label}」？`} okText="确认" cancelText="取消" onConfirm={() => signal(o)}>
        <Tooltip title={o.label}>
          <Button style={fill} loading={loading} disabled={disabled} danger={o.style === 'danger'}>
            <Label text={o.label} />
          </Button>
        </Tooltip>
      </Popconfirm>
    );
  };

  const primary: ReactNode[] = [];
  if (t.status === 'TASK_STATUS_UNASSIGNED') {
    primary.push(
      <Button
        key="claim"
        type="primary"
        style={fill}
        loading={busy === 'claim'}
        disabled={!!busy && busy !== 'claim'}
        onClick={() => void run('claim', '认领', () => taskApi.claim(t.id), 'claimed')}
      >
        认领
      </Button>,
    );
  } else if (t.status === 'TASK_STATUS_PENDING' && t.assignee_id === props.me) {
    primary.push(
      <Button
        key="start"
        type="primary"
        style={fill}
        loading={busy === 'start'}
        disabled={!!busy && busy !== 'start'}
        onClick={() => void run('start', '开始处理', () => taskApi.startProcessing(t.id), 'started')}
      >
        开始处理
      </Button>,
    );
  }

  const options = p?.can_signal ? p.options : [];
  const room = Math.max(0, INLINE_OPTIONS + (primary.length === 0 ? 1 : 0));
  const inline = options.slice(0, room);
  const more = options.slice(room);

  const meta = [
    t.task_type_name || t.task_type_key,
    t.status === 'TASK_STATUS_SNOOZED' && Number(t.snooze_until) > 0 ? `推迟至 ${fmtTime(t.snooze_until)}` : null,
    Number(t.due_at) > 0 ? `截止 ${fmtTime(t.due_at)}` : null,
    t.status === 'TASK_STATUS_UNASSIGNED' ? '组队列待认领' : null,
  ].filter(Boolean);

  const overdue = isOverdue(t);
  return (
    <Card
      size="small"
      hoverable
      style={{
        position: 'relative',
        height: '100%',
        display: 'flex',
        flexDirection: 'column',
        borderLeft: `3px solid ${priorityColor[t.priority]}`,
      }}
      styles={{
        header: overdue ? { paddingInlineEnd: 72 } : undefined,
        body: { flex: 1, display: 'flex', flexDirection: 'column', gap: 8 },
      }}
      title={
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, minWidth: 0 }}>
          <Tag color={priorityColor[t.priority]} style={{ marginInlineEnd: 0, flex: 'none' }}>
            {t.priority.replace('TASK_PRIORITY_', '')}
          </Tag>
          <Tag color={taskStatusColor[t.status]} style={{ marginInlineEnd: 0, flex: 'none' }}>
            {taskStatusLabel[t.status]}
          </Tag>
          <Typography.Text strong ellipsis={{ tooltip: t.title }} style={{ flex: 1, minWidth: 0 }}>
            {t.title}
          </Typography.Text>
        </div>
      }
      onClick={(e) => {
        // 点卡片空白处进入详情；按钮、弹层里的点击不跳转。
        if ((e.target as HTMLElement).closest('button, a, .ant-popover, .ant-dropdown, .ant-modal-root')) return;
        navigate(detail);
      }}
    >
      {overdue && (
        <span
          role="status"
          style={{
            position: 'absolute',
            top: 0,
            right: 12,
            padding: '0 8px',
            lineHeight: '20px',
            fontSize: 12,
            color: '#fff',
            background: '#f5222d',
            borderRadius: '0 0 4px 4px',
          }}
        >
          已逾期
        </span>
      )}
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        {meta.join(' · ')}
      </Typography.Text>
      <Typography.Paragraph ellipsis={{ rows: 2, tooltip: t.subtitle }} style={{ flex: 1, marginBottom: 0, minHeight: 44 }}>
        {t.subtitle}
      </Typography.Paragraph>
      <div style={{ display: 'flex', gap: 8 }}>
        <Button style={fill} onClick={() => navigate(detail)}>
          查看
        </Button>
        {primary}
        {inline.map(optionButton)}
        {more.length > 0 && (
          <Dropdown
            trigger={['click']}
            menu={{
              items: more.map((o) => ({ key: o.key, label: optionLabel(o), danger: o.style === 'danger' })),
              onClick: ({ key }) => {
                const o = more.find((x) => x.key === key);
                if (!o) return;
                if (o.required_fields.length > 0 || Number(o.snooze_ms) === 0) setForm(o);
                else void signal(o);
              },
            }}
          >
            <Button icon={<MoreOutlined />} aria-label="更多操作" />
          </Dropdown>
        )}
        <Tooltip title="复制链接">
          <Button
            icon={<LinkOutlined />}
            aria-label="复制链接"
            onClick={() => {
              void navigator.clipboard?.writeText(new URL(detail, window.location.origin).toString());
              message.success('链接已复制');
            }}
          />
        </Tooltip>
      </div>
      {form && p && (
        <SignalModal
          runId={t.run_id}
          action={p}
          option={form}
          onClose={() => setForm(undefined)}
          onAccepted={() => {
            setForm(undefined);
            message.success(`已提交：${form.label}`);
            props.onChanged(t.id, 'signaled');
          }}
          onStale={(reason) => {
            setForm(undefined);
            message.warning(reason || '该任务已被处理');
            props.onChanged(t.id, 'signaled');
          }}
        />
      )}
    </Card>
  );
}
