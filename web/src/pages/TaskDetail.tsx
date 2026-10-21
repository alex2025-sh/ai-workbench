// 任务详情 /tasks/:id（Lark 卡片「在工作台处理」的深链）。
//
// 左侧：卡片（与 Lark 一致）、全部业务字段、节点链（当前节点高亮，走过的节点为绿色）。
// 右侧：状态与操作（认领 / 分配 / 开始处理 / 节点动作 / 转派 / 调整优先级）、处理历史。
// 经手人（如升级前的 RM）与团队成员可以查看整个流程，但只有当前处理人能处理。
import { ArrowLeftOutlined, EyeOutlined } from '@ant-design/icons';
import { Alert, App as AntApp, Card, Col, Descriptions, Result, Row, Select, Space, Spin, Tag, Timeline, Typography } from 'antd';
import { useCallback, useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ApiError, decodeJson } from '../api/client';
import { taskApi } from '../api/services';
import type { HistoryItem, TaskDetail as Detail, TaskPriority } from '../api/types';
import { ChainCanvas } from '../components/ChainCanvas';
import { TaskActions } from '../components/TaskActions';
import { TaskCard } from '../components/TaskCard';
import { fmtTime, historyLabel, priorityColor, priorityLabel, resolutionLabel, taskStatusColor, taskStatusLabel } from '../format';

const priorities: TaskPriority[] = ['TASK_PRIORITY_P0', 'TASK_PRIORITY_P1', 'TASK_PRIORITY_P2', 'TASK_PRIORITY_P3'];

// 进入节点时处理人是怎么选出来的（assign.Target.By）
const assignedBy: Record<string, string> = { relation: '（业务关系）', fallback: '（业务关系查不到，兜底人）', random: '（随机）' };

function historyText(h: HistoryItem): string {
  const d = decodeJson<Record<string, unknown>>(h.detail_json) ?? {};
  const parts = [historyLabel[h.kind] ?? h.kind];
  if (h.kind === 'snooze' && d.snooze_until) parts.push(`至 ${fmtTime(String(d.snooze_until))}`);
  if (h.kind === 'escalate') parts.push(`离开「${h.node_name}」`);
  if (h.kind === 'transferred') parts.push(d.to ? `${d.from || '待认领'} → ${d.to}` : `${d.from}，放回待认领`);
  if (h.kind === 'priority') parts.push(`P${d.from} → P${d.to}`);
  if (h.kind === 'created') parts.push(`进入「${h.node_name}」`);
  if (d.assignee) parts[parts.length - 1] += `，交给 ${d.assignee}${assignedBy[String(d.assigned_by)] ?? ''}`;
  else if (d.assigned_by === 'claim' && h.kind !== 'transferred') parts[parts.length - 1] += '，通知到群等待认领';
  if (d.note) parts[parts.length - 1] += `：${d.note}`;
  return parts.join(' ');
}

export default function TaskDetail() {
  const { id = '' } = useParams();
  const { message } = AntApp.useApp();
  const [d, setD] = useState<Detail>();
  const [error, setError] = useState<ApiError | Error>();

  const load = useCallback(() => {
    taskApi.get(id).then(setD, (e) => setError(e as Error));
  }, [id]);
  useEffect(load, [load]);

  if (error) {
    const code = error instanceof ApiError ? error.code : '';
    return (
      <Result
        status={code === 'NotFound' ? '404' : code === 'PermissionDenied' ? '403' : 'error'}
        title={code === 'NotFound' ? '任务不存在' : code === 'PermissionDenied' ? '你没有参与这个任务' : '加载失败'}
        subTitle={error.message}
        extra={<Link to="/tasks">返回任务工作台</Link>}
      />
    );
  }
  if (!d) return <Spin style={{ display: 'block', marginTop: 80 }} />;
  const t = d.task;
  const terminal = t.status === 'TASK_STATUS_SOLVED';

  return (
    <>
      <Space style={{ marginBottom: 16 }}>
        <Link to="/tasks">
          <ArrowLeftOutlined /> 任务工作台
        </Link>
      </Space>
      {d.read_only && !terminal && (
        <Alert
          type="info"
          showIcon
          icon={<EyeOutlined />}
          style={{ marginBottom: 16 }}
          message={`任务现在由 ${t.assignee_id || '—'} 处理，你可以查看整个流程，但不能处理。`}
        />
      )}
      <Row gutter={[16, 16]}>
        <Col xs={24} lg={16}>
          <Space direction="vertical" size={16} style={{ width: '100%' }}>
            <TaskCard card={t.card} />
            <Card size="small" title="业务字段">
              <Descriptions
                bordered
                size="small"
                column={{ xs: 1, md: 2 }}
                items={d.fields.map((f, i) => ({ key: i, label: f.label, children: f.value || <Typography.Text type="secondary">—</Typography.Text> }))}
                labelStyle={{ width: 112, color: 'rgba(0,0,0,0.55)' }}
              />
            </Card>
            <Card size="small" title="流程" extra={<Typography.Text type="secondary">{t.task_type_name} · v{t.task_type_version}</Typography.Text>}>
              <ChainCanvas
                height={220}
                nodes={d.nodes.map((n) => ({
                  id: n.id,
                  name: n.name,
                  assignee: n.assignee,
                  actions: n.actions,
                  state: n.current ? 'current' : n.visited ? 'visited' : 'future',
                }))}
              />
            </Card>
          </Space>
        </Col>
        <Col xs={24} lg={8}>
          <Space direction="vertical" size={16} style={{ width: '100%' }}>
            <Card size="small" title="状态">
              <Descriptions
                size="small"
                column={1}
                items={[
                  { key: 's', label: '状态', children: <Tag color={taskStatusColor[t.status]}>{taskStatusLabel[t.status]}</Tag> },
                  { key: 'p', label: '优先级', children: <Tag color={priorityColor[t.priority]}>{priorityLabel[t.priority]}</Tag> },
                  { key: 'n', label: '当前节点', children: t.card?.node_name ?? t.node_id },
                  {
                    key: 'a',
                    label: '处理人',
                    children: t.assignee_id || (t.status === 'TASK_STATUS_UNASSIGNED' ? '待认领' : '—'),
                  },
                  ...(t.status === 'TASK_STATUS_SNOOZED' ? [{ key: 'z', label: '推迟至', children: fmtTime(t.snooze_until, 'YYYY-MM-DD HH:mm') }] : []),
                  ...(terminal ? [{ key: 'r', label: '结果', children: resolutionLabel[t.resolution] ?? t.resolution }] : []),
                  { key: 'k', label: '数据源 key', children: <Typography.Text code copyable>{t.biz_key}</Typography.Text> },
                  { key: 'c', label: '创建', children: fmtTime(t.created_at, 'YYYY-MM-DD HH:mm') },
                  ...(d.transfer_count > 0 ? [{ key: 't', label: '转交次数', children: d.transfer_count }] : []),
                ]}
              />
              {!terminal && (
                <div style={{ marginTop: 12 }}>
                  <TaskActions task={t} layout="detail" onChanged={load} />
                </div>
              )}
              {d.can_update_priority && (
                <Space wrap style={{ marginTop: 12 }}>
                  <Typography.Text type="secondary">优先级</Typography.Text>
                  {(
                    <Select
                      size="small"
                      value={t.priority}
                      style={{ width: 120 }}
                      options={priorities.map((p) => ({ value: p, label: priorityLabel[p] }))}
                      onChange={async (p: TaskPriority) => {
                        try {
                          await taskApi.priority(t.id, p);
                          message.success('优先级已调整');
                          load();
                        } catch (e) {
                          message.error(String((e as Error).message));
                        }
                      }}
                    />
                  )}
                </Space>
              )}
            </Card>
            <Card size="small" title="处理历史">
              <Timeline
                items={d.history.map((h) => ({
                  color: h.kind === 'gone' || h.kind === 'close' || h.kind === 'no_response' ? 'green' : h.kind === 'escalate' ? 'orange' : 'blue',
                  children: (
                    <div>
                      <div>{historyText(h)}</div>
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        {fmtTime(h.at, 'MM-DD HH:mm')} · {h.actor}
                      </Typography.Text>
                    </div>
                  ),
                }))}
              />
            </Card>
          </Space>
        </Col>
      </Row>
    </>
  );
}

