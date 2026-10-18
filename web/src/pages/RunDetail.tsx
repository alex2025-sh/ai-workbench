// 运行详情：节点状态表与事件时间线，运行干预（取消运行；重试、跳过节点待实现）需要 run.admin。三期换成运行态画布。
import { ArrowLeftOutlined, ReloadOutlined } from '@ant-design/icons';
import { App as AntApp, Button, Card, Descriptions, Form, Input, Modal, Result, Space, Spin, Table, Tag, Typography } from 'antd';
import { useCallback, useEffect, useState } from 'react';
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
};

export default function RunDetail() {
  const { id = '' } = useParams();
  const [run, setRun] = useState<RunView>();
  const [events, setEvents] = useState<RunEvent[]>([]);
  const [error, setError] = useState<Error>();
  const [cancelling, setCancelling] = useState(false);
  const { can } = useIdentity();

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
            {
              title: '状态',
              dataIndex: 'status',
              render: (s: string) =>
                // run 已结束（如被取消）时，引擎不改写未完成节点的状态，这里显示为已中止。
                run.status !== 'running' && run.status !== 'needs_intervention' && ['waiting', 'running', 'ready'].includes(s) ? (
                  <Tag>已中止</Tag>
                ) : (
                  <Tag color={nodeColor[s]}>{nodeStatusLabel[s] ?? s}</Tag>
                ),
            },
            { title: '出口', dataIndex: 'port', render: (v: string) => v || '—' },
            { title: '执行次数', dataIndex: 'attempt' },
            { title: '开始', dataIndex: 'started_at', render: (v: string) => fmtTime(v, 'MM-DD HH:mm:ss') },
            { title: '结束', dataIndex: 'finished_at', render: (v: string) => fmtTime(v, 'MM-DD HH:mm:ss') },
          ]}
        />
      </Card>
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
