// 任务上的操作按钮，任务卡片与详情页共用。按钮来自服务端返回的 TaskSummary：
// 认领 / 分配 / 开始处理由 can_* 决定；节点动作由 actions 决定（服务端已按调用人过滤）。权限以服务端校验为准。
//
// - 已处理、无需处理：二次确认；
// - 推迟：选择时长（节点配置的选项）；
// - 升级：弹窗填写备注，显示升级到哪个节点；
// - 分配：弹窗从团队成员中选择执行者；
// - 业务链接：新窗口打开，不改变任务状态。
import { DownOutlined, ExportOutlined, MoreOutlined } from '@ant-design/icons';
import { App as AntApp, Button, Dropdown, Input, Modal, Select, Space, Tooltip } from 'antd';
import type { MenuProps } from 'antd';
import { useState, type ReactNode } from 'react';
import { ApiError } from '../api/client';
import { taskApi, teamApi } from '../api/services';
import type { ActionOption, TaskSummary } from '../api/types';
import { fmtDuration } from '../format';

type Change = 'claimed' | 'assigned' | 'started' | 'acted';

interface Op {
  key: string;
  label: string;
  primary?: boolean;
  danger?: boolean;
  icon?: ReactNode;
  run?: () => void; // 直接执行
  menu?: MenuProps['items']; // 二级选择（推迟时长）
  onMenu?: (key: string) => void;
  tooltip?: string;
}

export function TaskActions(props: {
  task: TaskSummary;
  /** card：卡片底部，最多直接显示 inline 个，其余收进「更多」；detail：全部平铺。 */
  layout: 'card' | 'detail';
  inline?: number;
  /** 分配模式下可选的执行者（详情页由后端给出；卡片上为空时弹窗内加载团队成员）。 */
  members?: string[];
  onChanged: (change: Change) => void;
  extra?: ReactNode;
}) {
  const t = props.task;
  const { message } = AntApp.useApp();
  const [busy, setBusy] = useState<string>();
  const [escalate, setEscalate] = useState<ActionOption>();
  const [note, setNote] = useState('');
  const [assignOpen, setAssignOpen] = useState(false);
  const [members, setMembers] = useState<string[]>(props.members ?? []);
  const [assignee, setAssignee] = useState<string>();

  const exec = async (key: string, label: string, fn: () => Promise<unknown>, change: Change) => {
    setBusy(key);
    try {
      await fn();
      message.success(`${label}成功`);
      props.onChanged(change);
      return true;
    } catch (e) {
      // 任务已被别人处理或已离开当前节点：提示后刷新
      const stale = e instanceof ApiError && ['Aborted', 'FailedPrecondition'].includes(e.code);
      message[stale ? 'warning' : 'error'](e instanceof ApiError ? `${label}失败：${e.message}` : String(e));
      if (stale) props.onChanged(change);
      return false;
    } finally {
      setBusy(undefined);
    }
  };

  const act = (o: ActionOption, extra: { snooze_ms?: string; note?: string } = {}) =>
    exec(o.key, o.label, () => taskApi.act({ id: t.id, action: o.key, expect_node: t.node_id, ...extra }), 'acted');

  const confirm = (o: ActionOption, content: string) =>
    Modal.confirm({ title: `确认「${o.label}」？`, content, okText: '确认', cancelText: '取消', onOk: () => act(o) });

  const ops: Op[] = [];
  if (t.can_claim && !t.can_assign) {
    ops.push({ key: 'claim', label: '认领', primary: true, run: () => void exec('claim', '认领', () => taskApi.claim(t.id), 'claimed') });
  }
  if (t.can_assign) {
    ops.push({
      key: 'assign',
      label: '分配',
      primary: true,
      run: () => {
        setAssignOpen(true);
        if (!props.members?.length) {
          teamApi.get(t.team_key).then((tm) => setMembers(tm.members.map((m) => m.user_id)), () => undefined);
        }
      },
    });
  }
  if (t.can_start) {
    ops.push({ key: 'start', label: '开始处理', primary: ops.length === 0, run: () => void exec('start', '开始处理', () => taskApi.start(t.id), 'started') });
  }
  for (const o of t.actions ?? []) {
    switch (o.key) {
      case 'close':
        ops.push({ key: o.key, label: o.label, primary: ops.length === 0, run: () => confirm(o, '任务将以「已处理」完结。') });
        break;
      case 'no_response':
        ops.push({ key: o.key, label: o.label, run: () => confirm(o, '任务将以「无需处理」完结。') });
        break;
      case 'snooze':
        ops.push({
          key: o.key,
          label: o.label,
          icon: <DownOutlined />,
          tooltip: '到期后重新提醒，期间仍可直接处理',
          menu: o.snooze_ms.map((ms) => ({ key: ms, label: `推迟 ${fmtDuration(ms)}` })),
          onMenu: (ms) => void act(o, { snooze_ms: ms }),
        });
        break;
      case 'escalate':
        ops.push({ key: o.key, label: o.label, tooltip: `升级到「${o.next_node_name}」`, run: () => setEscalate(o) });
        break;
      case 'link':
        ops.push({ key: o.key, label: o.label, icon: <ExportOutlined />, run: () => window.open(o.url, '_blank', 'noopener') });
        break;
    }
  }

  const fill = props.layout === 'card' ? ({ flex: '1 1 0', minWidth: 0 } as const) : undefined;
  const button = (op: Op) => {
    const b = (
      <Button
        key={op.key}
        style={fill}
        type={op.primary ? 'primary' : 'default'}
        danger={op.danger}
        loading={busy === op.key}
        disabled={!!busy && busy !== op.key}
        onClick={op.run}
      >
        <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{op.label}</span>
        {op.menu && op.icon}
        {!op.menu && op.key === 'link' && op.icon}
      </Button>
    );
    const wrapped = op.menu ? (
      <Dropdown key={op.key} trigger={['click']} menu={{ items: op.menu, onClick: ({ key }) => op.onMenu?.(key) }}>
        {b}
      </Dropdown>
    ) : (
      b
    );
    return op.tooltip ? (
      <Tooltip key={op.key} title={op.tooltip}>
        {wrapped}
      </Tooltip>
    ) : (
      wrapped
    );
  };

  const limit = props.layout === 'card' ? (props.inline ?? 3) : ops.length;
  const shown = ops.slice(0, limit);
  const more = ops.slice(limit);
  const moreItems: MenuProps['items'] = more.flatMap((op) =>
    op.menu
      ? [{ key: op.key, label: op.label, children: op.menu.map((m) => ({ ...m!, key: `${op.key}:${String(m!.key)}` })) }]
      : [{ key: op.key, label: op.label }],
  );

  return (
    <>
      <div style={{ display: 'flex', gap: 8, flexWrap: props.layout === 'card' ? 'nowrap' : 'wrap' }}>
        {props.extra}
        {shown.map(button)}
        {more.length > 0 && (
          <Dropdown
            trigger={['click']}
            menu={{
              items: moreItems,
              onClick: ({ key }) => {
                const [opKey, sub] = String(key).split(':');
                const op = more.find((x) => x.key === opKey);
                if (sub) op?.onMenu?.(sub);
                else op?.run?.();
              },
            }}
          >
            <Button icon={<MoreOutlined />} aria-label="更多操作" />
          </Dropdown>
        )}
      </div>
      <Modal
        title={escalate ? `升级到「${escalate.next_node_name}」` : ''}
        open={!!escalate}
        okText="升级"
        cancelText="取消"
        confirmLoading={busy === 'escalate'}
        onCancel={() => setEscalate(undefined)}
        onOk={async () => {
          if (escalate && (await act(escalate, { note }))) {
            setEscalate(undefined);
            setNote('');
          }
        }}
        destroyOnClose
      >
        <p>任务将交给下一个节点处理，你仍然可以查看，但不能再处理。</p>
        <Input.TextArea rows={3} placeholder="备注（选填），如已尝试的处理" value={note} onChange={(e) => setNote(e.target.value)} />
      </Modal>
      <Modal
        title="分配给团队成员"
        open={assignOpen}
        okText="分配"
        cancelText="取消"
        okButtonProps={{ disabled: !assignee }}
        confirmLoading={busy === 'assign'}
        onCancel={() => setAssignOpen(false)}
        onOk={async () => {
          if (assignee && (await exec('assign', '分配', () => taskApi.assign(t.id, assignee), 'assigned'))) setAssignOpen(false);
        }}
        destroyOnClose
      >
        <Space direction="vertical" style={{ width: '100%' }}>
          <Select
            style={{ width: '100%' }}
            placeholder="选择执行者"
            value={assignee}
            onChange={setAssignee}
            options={members.map((m) => ({ value: m, label: m }))}
            showSearch
          />
        </Space>
      </Modal>
    </>
  );
}
