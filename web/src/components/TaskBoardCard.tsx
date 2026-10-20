// 任务工作台的卡片：一眼看清「是什么、多急、在哪一步」，常用操作不用进详情就能完成。
//
// - 标题行：优先级、状态、卡片标题（与 Lark 卡片一致）。
// - 正文：任务类型 · 当前节点 · 团队队列 / 推迟至；卡片的前 3 个字段。
// - 操作：查看；认领 / 分配 / 开始处理；节点动作（服务端按调用人给出）。经手人的只读卡片只有「查看」。
import { LinkOutlined } from '@ant-design/icons';
import { App as AntApp, Button, Card, Tag, Tooltip, Typography } from 'antd';
import { useNavigate } from 'react-router-dom';
import type { TaskSummary } from '../api/types';
import { fmtTime, priorityColor, resolutionLabel, taskStatusColor, taskStatusLabel } from '../format';
import { TaskActions } from './TaskActions';

export function TaskBoardCard(props: { task: TaskSummary; onChanged: () => void }) {
  const t = props.task;
  const navigate = useNavigate();
  const { message } = AntApp.useApp();
  const detail = `/tasks/${t.id}`;
  const card = t.card;
  const queue = t.status === 'TASK_STATUS_UNASSIGNED' && t.team_key;

  const meta = [
    t.task_type_name || t.task_type_key,
    card?.node_name,
    queue ? `团队 ${t.team_key} · ${t.team_mode === 'assign' ? '待分配' : '待认领'}` : null,
    t.status === 'TASK_STATUS_SNOOZED' ? `推迟至 ${fmtTime(t.snooze_until)}` : null,
    t.status === 'TASK_STATUS_SOLVED' ? resolutionLabel[t.resolution] ?? t.resolution : null,
    t.status !== 'TASK_STATUS_UNASSIGNED' && t.assignee_id ? `处理人 ${t.assignee_id}` : null,
  ].filter(Boolean);

  return (
    <Card
      size="small"
      hoverable
      style={{ height: '100%', display: 'flex', flexDirection: 'column', borderLeft: `3px solid ${priorityColor[t.priority]}` }}
      styles={{ body: { flex: 1, display: 'flex', flexDirection: 'column', gap: 8 } }}
      title={
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, minWidth: 0 }}>
          <Tag color={priorityColor[t.priority]} style={{ marginInlineEnd: 0, flex: 'none' }}>
            {t.priority.replace('TASK_PRIORITY_', '')}
          </Tag>
          <Tag color={taskStatusColor[t.status]} style={{ marginInlineEnd: 0, flex: 'none' }}>
            {queue && t.team_mode === 'assign' ? '待分配' : taskStatusLabel[t.status]}
          </Tag>
          <Typography.Text strong ellipsis={{ tooltip: card?.title }} style={{ flex: 1, minWidth: 0 }}>
            {card?.title || `${t.task_type_key} · ${t.biz_key}`}
          </Typography.Text>
        </div>
      }
      onClick={(e) => {
        // 点卡片空白处进入详情；按钮与弹层里的点击不跳转。
        if ((e.target as HTMLElement).closest('button, a, .ant-dropdown, .ant-modal-root, .ant-select-dropdown')) return;
        navigate(detail);
      }}
    >
      <Typography.Text type="secondary" style={{ fontSize: 12 }} ellipsis={{ tooltip: meta.join(' · ') }}>
        {meta.join(' · ')}
      </Typography.Text>
      <div style={{ flex: 1, minHeight: 66 }}>
        {(card?.fields ?? []).slice(0, 3).map((f) => (
          <div key={f.label} style={{ display: 'flex', gap: 8, fontSize: 13, lineHeight: '22px' }}>
            <span style={{ color: 'rgba(0,0,0,0.45)', flex: 'none', width: 72, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {f.label}
            </span>
            <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{f.value || '—'}</span>
          </div>
        ))}
      </div>
      <TaskActions
        task={t}
        layout="card"
        inline={2}
        onChanged={props.onChanged}
        extra={
          <>
            <Button style={{ flex: '1 1 0', minWidth: 0 }} onClick={() => navigate(detail)}>
              查看
            </Button>
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
          </>
        }
      />
    </Card>
  );
}
