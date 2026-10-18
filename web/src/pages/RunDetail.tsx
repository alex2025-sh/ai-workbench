// 运行详情：节点状态表与事件时间线。运行干预（取消运行、重试节点、跳过节点）需要 run.admin，原因必填，写入 run 事件。
// 三期换成运行态画布。
import { ArrowLeftOutlined, ReloadOutlined } from '@ant-design/icons';
import { App as AntApp, Button, Card, Descriptions, Form, Input, Modal, Result, Select, Space, Spin, Table, Tag, Typography } from 'antd';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ApiError, decodeJson } from '../api/client';
import { runApi } from '../api/services';
import type { NodeView, RunEvent, RunView } from '../api/types';
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
  const [error, setError] = useState<Error>();
  const [cancelling, setCancelling] = useState(false);
  const [intervening, setIntervening] = useState<Intervention>();
  const { can } = useIdentity();

  // 节点的出口取自 workflow 图中从它连出的边（跳过时选择沿哪个出口继续）。
  const ports = useMemo(() => {
    const graph = decodeJson<{ edges?: { from: string; port: string }[] }>(run?.graph_json);
    const out: Record<string, string[]> = {};
    for (const e of graph?.edges ?? []) {
      out[e.from] = Array.from(new Set([...(out[e.from] ?? []), e.port]));
    }
    return out;
  }, [run?.graph_json]);

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
  }, [load]);

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
            {can('run.admin') && (run.status === 'running' || run.status === 'needs_intervention') && (
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
      <Card title="节点" size="small" style={{ marginBottom: 16 }}>
        <Table<NodeView>
          rowKey={(n) => `${n.node_id}:${n.iteration}`}
          size="small"
          pagination={false}
          dataSource={run.nodes}
          columns={[
            { title: '节点', dataIndex: 'node_id', render: (v: string) => <Typography.Text code>{v}</Typography.Text> },
            { title: '轮次', dataIndex: 'iteration' },
            { title: '状态', dataIndex: 'status', render: (s: string) => <Tag color={nodeColor[s]}>{nodeStatusLabel[s] ?? s}</Tag> },
            { title: '出口', dataIndex: 'port', render: (v: string) => v || '—' },
            { title: '执行次数', dataIndex: 'attempt' },
            { title: '开始', dataIndex: 'started_at', render: (v: string) => fmtTime(v, 'MM-DD HH:mm:ss') },
            { title: '结束', dataIndex: 'finished_at', render: (v: string) => fmtTime(v, 'MM-DD HH:mm:ss') },
            ...(can('run.admin') && (run.status === 'running' || run.status === 'needs_intervention')
              ? [
                  {
                    title: '干预',
                    render: (_: unknown, n: NodeView) => {
                      // 重试：run 暂停时，失败的节点或卡片发送失败而仍在等待的节点；跳过：进行中或失败的节点。
                      const retry = run.status === 'needs_intervention' && (n.status === 'failed' || n.status === 'waiting');
                      const skip = ['ready', 'running', 'waiting', 'failed'].includes(n.status) && (ports[n.node_id] ?? []).length > 0;
                      return (
                        <Space size="small">
                          {retry && (
                            <Button size="small" onClick={() => setIntervening({ kind: 'retry', node: n.node_id })}>
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
                    },
                  },
                ]
              : []),
          ]}
        />
      </Card>
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
            { title: '时间', dataIndex: 'created_at', render: (v: string) => fmtTime(v, 'MM-DD HH:mm:ss.SSS') },
            { title: '节点', dataIndex: 'node_id', render: (v: string) => v || '—' },
            { title: '事件', dataIndex: 'type' },
            { title: '操作人', dataIndex: 'actor', render: (v: string) => v || 'system' },
          ]}
        />
      </Card>
    </>
  );
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
