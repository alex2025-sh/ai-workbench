// 运行详情：运行态画布（节点按状态着色、走过的连线高亮）、选中节点的详情与干预、可读的事件时间线。
// 运行干预（取消运行、重试节点、跳过节点）需要 run.admin，原因必填，写入 run 事件。
import { ArrowLeftOutlined, ReloadOutlined } from '@ant-design/icons';
import { Alert, App as AntApp, Button, Card, Descriptions, Empty, Form, Input, Modal, Result, Select, Space, Spin, Table, Tag, Typography } from 'antd';
import dayjs from 'dayjs';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ApiError, decodeJson } from '../api/client';
import { catalogApi, runApi } from '../api/services';
import type { NodeTypeInfo, NodeView, RunEvent, RunView } from '../api/types';
import { RunCanvas } from '../components/canvas/RunCanvas';
import type { Positions, Workflow } from '../components/canvas/model';
import { fmtTime, nodeStatusLabel, runStatusColor, runStatusLabel } from '../format';
import { useIdentity } from '../identity';

const nodeColor: Record<string, string> = {
  succeeded: 'success',
  waiting: 'processing',
  running: 'processing',
  failed: 'error',
  skipped: 'default',
  cancelled: 'default',
};

/** 干预对话框：重试一个节点，或以某个出口跳过它。 */
type Intervention = { kind: 'retry' | 'skip'; node: string };

export default function RunDetail() {
  const { id = '' } = useParams();
  const [run, setRun] = useState<RunView>();
  const [events, setEvents] = useState<RunEvent[]>([]);
  const [catalog, setCatalog] = useState<NodeTypeInfo[]>([]);
  const [error, setError] = useState<Error>();
  const [cancelling, setCancelling] = useState(false);
  const [intervening, setIntervening] = useState<Intervention>();
  const [selected, setSelected] = useState<string>();
  const { can } = useIdentity();

  const workflow = useMemo(() => decodeJson<Workflow>(run?.graph_json) ?? { nodes: [], edges: [] }, [run?.graph_json]);
  const positions = useMemo(() => decodeJson<{ canvas?: { positions?: Positions } }>(run?.ui_json)?.canvas?.positions, [run?.ui_json]);

  // 节点的出口取自 workflow 图中从它连出的边（跳过时选择沿哪个出口继续）。
  const ports = useMemo(() => {
    const out: Record<string, string[]> = {};
    for (const e of workflow.edges) {
      out[e.from] = Array.from(new Set([...(out[e.from] ?? []), e.port]));
    }
    return out;
  }, [workflow]);

  const load = useCallback(async () => {
    try {
      const [r, ev] = await Promise.all([runApi.get(id), runApi.events(id, { page_size: 100 })]);
      setRun(r);
      setEvents(ev.items);
      setError(undefined);
    } catch (e) {
      setError(e as Error);
    }
  }, [id]);

  useEffect(() => {
    void load();
    catalogApi
      .nodeTypes()
      .then((r) => setCatalog(r.items))
      .catch(() => setCatalog([]));
  }, [load]);

  // 首次加载时默认选中卡住的节点。
  useEffect(() => {
    if (run?.blocked_node) setSelected((cur) => cur ?? run.blocked_node);
  }, [run?.blocked_node]);

  // 运行中的 run 每 5 秒刷新一次。
  useEffect(() => {
    if (run?.status !== 'running') return;
    const t = window.setInterval(() => void load(), 5000);
    return () => window.clearInterval(t);
  }, [run?.status, load]);

  if (error) {
    const notFound = error instanceof ApiError && error.code === 'NotFound';
    return <Result status={notFound ? '404' : 'error'} title={notFound ? 'Run 不存在' : '加载失败'} subTitle={error.message} extra={<Link to="/runs">返回运行监控</Link>} />;
  }
  if (!run) return <Spin style={{ display: 'block', marginTop: 80 }} />;

  const admin = can('run.admin') && (run.status === 'running' || run.status === 'needs_intervention');
  // 重试：run 暂停时，失败的节点或卡片发送失败而仍在等待的节点；跳过：进行中或失败、且有出口的节点。
  const actionsFor = (n?: NodeView) => {
    if (!admin || !n) return null;
    const retry = run.status === 'needs_intervention' && (n.status === 'failed' || n.status === 'waiting');
    const skip = ['ready', 'running', 'waiting', 'failed'].includes(n.status) && (ports[n.node_id] ?? []).length > 0;
    if (!retry && !skip) return null;
    return (
      <Space size="small">
        {retry && (
          <Button size="small" type="primary" onClick={() => setIntervening({ kind: 'retry', node: n.node_id })}>
            重试
          </Button>
        )}
        {skip && (
          <Button size="small" danger onClick={() => setIntervening({ kind: 'skip', node: n.node_id })}>
            跳过
          </Button>
        )}
      </Space>
    );
  };
  const nodeById = (nid?: string) => run.nodes.find((n) => n.node_id === nid);
  const blocked = nodeById(run.blocked_node);

  return (
    <>
      <Space style={{ marginBottom: 16 }}>
        <Link to="/runs">
          <ArrowLeftOutlined /> 运行监控
        </Link>
      </Space>
      <Card
        title={
          <Space>
            Run #{run.run_id}
            <Tag color={runStatusColor[run.status]}>{runStatusLabel[run.status] ?? run.status}</Tag>
          </Space>
        }
        extra={
          <Space>
            {admin && (
              <Button danger onClick={() => setCancelling(true)}>
                取消运行
              </Button>
            )}
            <Button icon={<ReloadOutlined />} onClick={() => void load()}>
              刷新
            </Button>
          </Space>
        }
        style={{ marginBottom: 16 }}
      >
        <Descriptions size="small">
          <Descriptions.Item label="任务类型">
            <Link to={`/config/task-types/${run.task_type_key}`}>
              {run.task_type_key}@{run.task_type_version}
            </Link>
          </Descriptions.Item>
        </Descriptions>
        {run.status === 'needs_intervention' && (
          <Alert
            type="error"
            showIcon
            style={{ marginTop: 12 }}
            message={
              <Space>
                卡在节点
                <Typography.Link code onClick={() => setSelected(run.blocked_node)}>
                  {run.blocked_node || '—'}
                </Typography.Link>
              </Space>
            }
            description={run.blocked_reason || '原因未知，见下方事件'}
            action={actionsFor(blocked)}
          />
        )}
      </Card>
      {cancelling && (
        <CancelModal
          runId={run.run_id}
          onClose={() => setCancelling(false)}
          onDone={() => {
            setCancelling(false);
            void load();
          }}
        />
      )}
      <div style={{ display: 'flex', gap: 16, marginBottom: 16, height: 520 }}>
        <Card size="small" title="流程" style={{ flex: 1, minWidth: 0 }} styles={{ body: { height: 'calc(100% - 38px)', padding: 0 } }}>
          {catalog.length > 0 && (
            <RunCanvas
              workflow={workflow}
              positions={positions}
              nodes={run.nodes}
              catalog={catalog}
              selected={selected}
              focus={run.blocked_node || run.nodes.find((n) => ['failed', 'waiting', 'running', 'ready'].includes(n.status))?.node_id}
              onSelect={setSelected}
            />
          )}
        </Card>
        <NodePanel node={nodeById(selected)} nodeId={selected} workflow={workflow} actions={actionsFor(nodeById(selected))} />
      </div>
      {intervening && (
        <InterveneModal
          runId={run.run_id}
          action={intervening}
          ports={ports[intervening.node] ?? []}
          onClose={() => setIntervening(undefined)}
          onDone={() => {
            setIntervening(undefined);
            void load();
          }}
        />
      )}
      <Card title="事件" size="small">
        <Table<RunEvent>
          rowKey="id"
          size="small"
          pagination={false}
          dataSource={events}
          expandable={{
            rowExpandable: (e) => !!e.payload_json,
            expandedRowRender: (e) => <pre style={{ margin: 0, fontSize: 12 }}>{JSON.stringify(decodeJson(e.payload_json), null, 2)}</pre>,
          }}
          columns={[
            { title: '时间', dataIndex: 'created_at', width: 150, render: (v: string) => fmtTime(v, 'MM-DD HH:mm:ss.SSS') },
            {
              title: '节点',
              dataIndex: 'node_id',
              width: 140,
              render: (v: string) => (v ? <Typography.Link onClick={() => setSelected(v)}>{v}</Typography.Link> : '—'),
            },
            {
              title: '事件',
              render: (_, e) => {
                const { text, bad } = describeEvent(e);
                return <Typography.Text type={bad ? 'danger' : undefined}>{text}</Typography.Text>;
              },
            },
            { title: '操作人', dataIndex: 'actor', width: 160, render: (v: string) => v || 'system' },
          ]}
        />
      </Card>
    </>
  );
}

// 选中节点的详情：状态、出口、执行次数、起止时间、最近一次错误与干预按钮。
function NodePanel(props: { node?: NodeView; nodeId?: string; workflow: Workflow; actions: React.ReactNode }) {
  const { node: n, nodeId } = props;
  const def = props.workflow.nodes.find((x) => x.id === nodeId);
  return (
    <Card size="small" title={def ? def.name || def.id : '节点详情'} style={{ width: 340, overflow: 'auto' }} extra={props.actions}>
      {!def ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="点击流程图中的节点查看详情" />
      ) : (
        <>
          <Descriptions size="small" column={1}>
            <Descriptions.Item label="节点">
              <Typography.Text code>{def.id}</Typography.Text>
              <Typography.Text type="secondary" style={{ marginLeft: 4 }}>
                {def.type}
              </Typography.Text>
            </Descriptions.Item>
            <Descriptions.Item label="状态">{n ? <Tag color={nodeColor[n.status]}>{nodeStatusLabel[n.status] ?? n.status}</Tag> : <Tag>未到达</Tag>}</Descriptions.Item>
            {n?.port && <Descriptions.Item label="出口">{n.port}</Descriptions.Item>}
            {n && n.attempt > 0 && <Descriptions.Item label="执行次数">{n.attempt}</Descriptions.Item>}
            {n && <Descriptions.Item label="开始">{fmtTime(n.started_at, 'MM-DD HH:mm:ss')}</Descriptions.Item>}
            {n && <Descriptions.Item label="结束">{fmtTime(n.finished_at, 'MM-DD HH:mm:ss')}</Descriptions.Item>}
          </Descriptions>
          {n?.last_error && (
            <Alert
              type={n.status === 'failed' ? 'error' : 'warning'}
              message={n.status === 'failed' ? '错误' : '最近一次失败（之后已重试）'}
              description={<Typography.Text style={{ fontSize: 12, wordBreak: 'break-all' }}>{n.last_error}</Typography.Text>}
            />
          )}
        </>
      )}
    </Card>
  );
}

const effectLabel: Record<string, string> = { sync_task: '同步任务状态', lark_card: '发送 Lark 卡片', send_email: '发送邮件', emit_event: '发出事件' };

/** 把事件翻译成一句话；bad 表示失败类事件。原始 payload 可在展开行中查看。 */
function describeEvent(e: RunEvent): { text: string; bad?: boolean } {
  const p = (decodeJson<Record<string, any>>(e.payload_json) ?? {}) as Record<string, any>;
  const at = (v?: string) => (v ? dayjs(v).format('MM-DD HH:mm:ss') : '');
  switch (e.type) {
    case 'run_started':
      return { text: '流程启动' };
    case 'node_started':
      return { text: p.attempt > 1 ? `开始执行（第 ${p.attempt} 次）` : '开始执行' };
    case 'node_completed':
      return { text: p.port && p.port !== 'next' ? `完成，走出口 ${p.port}` : '完成' };
    case 'node_skipped':
      return { text: '未走到的分支，跳过' };
    case 'node_waiting':
      return { text: p.until ? `等待到 ${at(p.until)}` : '等待人工处理' };
    case 'node_retry':
      return { text: `第 ${p.attempt} 次执行失败，${at(p.next_at)} 重试：${p.error}`, bad: true };
    case 'node_failed':
      return { text: `执行失败（共 ${p.attempt} 次）：${p.error}`, bad: true };
    case 'node_lease_expired':
      return { text: `第 ${p.attempt} 次执行超时未返回`, bad: true };
    case 'signal':
      return { text: `做出选择：${p.option}` };
    case 'effect_done':
      return { text: `${effectLabel[p.kind] ?? p.kind}：完成` };
    case 'effect_failed':
      return { text: `${effectLabel[p.kind] ?? p.kind}：失败（第 ${p.attempt} 次）：${p.error}`, bad: true };
    case 'run_needs_intervention':
      return { text: `需要人工介入：${p.reason}`, bad: true };
    case 'job_quarantined':
      return { text: `内部任务多次处理失败，已隔离：${p.reason}`, bad: true };
    case 'node_retried':
      return { text: `人工重试：${p.reason}` };
    case 'node_skipped_manually':
      return { text: `人工跳过，走出口 ${p.port}：${p.reason}` };
    case 'run_resumed':
      return { text: `恢复运行：${p.reason}` };
    case 'run_cancelled':
      return { text: `取消运行：${p.reason}` };
    default:
      if (e.type.startsWith('run_')) {
        const status = e.type.slice(4);
        return { text: `流程结束：${runStatusLabel[status] ?? status}${p.resolution ? `（${p.resolution}）` : ''}`, bad: status === 'failed' };
      }
      return { text: e.type };
  }
}

// 取消运行：撤销尚未执行的节点与定时，任务置为已完成（auto_closed）。原因必填，写入 run 事件。
function CancelModal(props: { runId: string; onClose: () => void; onDone: () => void }) {
  const { message } = AntApp.useApp();
  const [form] = Form.useForm<{ reason: string }>();
  const [saving, setSaving] = useState(false);
  const submit = async () => {
    const { reason } = await form.validateFields();
    setSaving(true);
    try {
      await runApi.cancel(props.runId, reason);
      message.success('已取消运行');
      props.onDone();
    } catch (e) {
      message.error(e instanceof ApiError ? `取消失败：${e.message}` : String(e));
    } finally {
      setSaving(false);
    }
  };
  return (
    <Modal open title={`取消运行 #${props.runId}`} okText="确认取消" okButtonProps={{ danger: true }} onOk={() => void submit()} onCancel={props.onClose} confirmLoading={saving}>
      <Typography.Paragraph type="secondary">尚未执行的节点与定时会被撤销，关联的任务置为已完成，此操作不可撤回。</Typography.Paragraph>
      <Form form={form} layout="vertical">
        <Form.Item name="reason" label="原因" rules={[{ required: true, whitespace: true, message: '请填写原因' }]}>
          <Input.TextArea rows={3} />
        </Form.Item>
      </Form>
    </Modal>
  );
}

// 重试或跳过一个节点。重试：失败的节点重新执行、失败的卡片重新发送，并恢复暂停的 run；
// 跳过：节点以选定的出口完成，流程沿该出口继续。
function InterveneModal(props: { runId: string; action: Intervention; ports: string[]; onClose: () => void; onDone: () => void }) {
  const { message } = AntApp.useApp();
  const [form] = Form.useForm<{ port: string; reason: string }>();
  const [saving, setSaving] = useState(false);
  const { kind, node } = props.action;
  const label = kind === 'retry' ? '重试' : '跳过';
  const submit = async () => {
    const v = await form.validateFields();
    setSaving(true);
    try {
      if (kind === 'retry') await runApi.retryNode(props.runId, node, v.reason);
      else await runApi.skipNode(props.runId, node, v.port, v.reason);
      message.success(`已${label}节点 ${node}`);
      props.onDone();
    } catch (e) {
      message.error(e instanceof ApiError ? `${label}失败：${e.message}` : String(e));
    } finally {
      setSaving(false);
    }
  };
  return (
    <Modal open title={`${label}节点 ${node}`} okText={`确认${label}`} okButtonProps={{ danger: kind === 'skip' }} onOk={() => void submit()} onCancel={props.onClose} confirmLoading={saving}>
      <Typography.Paragraph type="secondary">
        {kind === 'retry'
          ? '失败的节点重新执行（重试次数清零），失败的卡片重新发送；run 恢复运行，暂停期间作废的定时会补回。'
          : '节点以选定的出口完成，尚未执行的定时与卡片撤销，流程沿该出口继续。'}
      </Typography.Paragraph>
      <Form form={form} layout="vertical" initialValues={{ port: props.ports[0] }}>
        {kind === 'skip' && (
          <Form.Item name="port" label="出口" rules={[{ required: true }]}>
            <Select options={props.ports.map((p) => ({ value: p, label: p }))} />
          </Form.Item>
        )}
        <Form.Item name="reason" label="原因" rules={[{ required: true, whitespace: true, message: '请填写原因' }]}>
          <Input.TextArea rows={3} />
        </Form.Item>
      </Form>
    </Modal>
  );
}
