// 任务详情：卡片（后端渲染，与 Lark 一致）、流程操作（等待中的人工节点）、简化进度与事件时间线。
//
// 流程操作走 RunService.Signal（服务端按任务表鉴权）。任务表由引擎异步同步，提交成功后短轮询，
// 直到等待中的节点 (node_id, iteration) 或任务的 updated_at 变化；被拒绝时按 code 决定刷新还是留在表单。
import { ArrowLeftOutlined, ReloadOutlined } from '@ant-design/icons';
import {
  Alert,
  App as AntApp,
  AutoComplete,
  Button,
  Card,
  Col,
  Collapse,
  Descriptions,
  Form,
  Input,
  Modal,
  Result,
  Row,
  Select,
  Space,
  Spin,
  Steps,
  Tag,
  Timeline,
  Tooltip,
  Typography,
} from 'antd';
import { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ApiError, decodeJson } from '../api/client';
import { runApi, taskApi } from '../api/services';
import type { ActionOption, Card as CardData, PendingAction, TaskDetail as Detail, TaskEvent, TaskPriority } from '../api/types';
import { TaskCard } from '../components/TaskCard';
import { useIdentity } from '../identity';
import { fmtDuration, fmtTime, nodeStatusLabel, priorityColor, priorityLabel, taskStatusColor, taskStatusLabel } from '../format';

const POLL_INTERVAL = 2000;
const POLL_TIMES = 10;

const stepStatus: Record<string, 'wait' | 'process' | 'finish' | 'error'> = {
  pending: 'wait',
  running: 'process',
  waiting: 'process',
  succeeded: 'finish',
  skipped: 'wait',
  failed: 'error',
};

export default function TaskDetail() {
  const { id = '' } = useParams();
  const { message } = AntApp.useApp();
  const [detail, setDetail] = useState<Detail>();
  const [events, setEvents] = useState<TaskEvent[]>([]);
  const [error, setError] = useState<ApiError | Error>();
  const [syncing, setSyncing] = useState(false);
  const [choosing, setChoosing] = useState<ActionOption>();
  const timer = useRef<number>();

  const load = useCallback(async () => {
    try {
      const [d, ev] = await Promise.all([taskApi.get(id), taskApi.events(id, { page_size: 100 })]);
      setDetail(d);
      setEvents(ev.items);
      setError(undefined);
      return d;
    } catch (e) {
      setError(e as Error);
      return undefined;
    }
  }, [id]);

  useEffect(() => {
    void load();
    return () => window.clearTimeout(timer.current);
  }, [load]);

  // 提交成功后等任务表同步：节点或 updated_at 变化即停止。
  const waitForSync = (before: Detail) => {
    setSyncing(true);
    const key = (d: Detail) => `${d.pending_action?.node_id ?? ''}:${d.pending_action?.iteration ?? ''}:${d.task.updated_at}`;
    let n = 0;
    const tick = async () => {
      const d = await load();
      if (d && key(d) !== key(before)) {
        setSyncing(false);
        return;
      }
      if (++n >= POLL_TIMES) {
        setSyncing(false);
        message.info('状态同步中，请稍后刷新');
        return;
      }
      timer.current = window.setTimeout(tick, POLL_INTERVAL);
    };
    timer.current = window.setTimeout(tick, 300);
  };

  if (error) {
    const notFound = error instanceof ApiError && error.code === 'NotFound';
    return (
      <Result
        status={notFound ? '404' : 'error'}
        title={notFound ? '任务不存在' : '加载失败'}
        subTitle={error.message}
        extra={<Link to="/tasks">返回我的任务</Link>}
      />
    );
  }
  if (!detail) return <Spin style={{ display: 'block', marginTop: 80 }} />;

  const t = detail.task;
  const card = decodeJson<CardData>(detail.rendered_json);
  const payload = decodeJson(t.payload_json);

  return (
    <>
      <Space style={{ marginBottom: 16 }}>
        <Link to="/tasks">
          <ArrowLeftOutlined /> 我的任务
        </Link>
      </Space>
      <Card
        title={
          <Space>
            <span style={{ borderLeft: `4px solid ${priorityColor[t.priority]}`, paddingLeft: 8 }}>{t.title}</span>
            <Tag color={taskStatusColor[t.status]}>{taskStatusLabel[t.status]}</Tag>
          </Space>
        }
        extra={
          <Button icon={<ReloadOutlined />} onClick={() => void load()}>
            刷新
          </Button>
        }
        style={{ marginBottom: 16 }}
      >
        <Descriptions size="small" column={4}>
          <Descriptions.Item label="优先级">{priorityLabel[t.priority]}</Descriptions.Item>
          <Descriptions.Item label="处理人">{t.assignee_id || '—（组队列）'}</Descriptions.Item>
          <Descriptions.Item label="截止">{fmtTime(t.due_at)}</Descriptions.Item>
          <Descriptions.Item label="推迟至">{fmtTime(t.snooze_until)}</Descriptions.Item>
          <Descriptions.Item label="任务类型">
            <Link to={`/config/task-types/${t.task_type_key}`}>
              {t.task_type_key}@{t.task_type_version}
            </Link>
          </Descriptions.Item>
          <Descriptions.Item label="结论">{t.resolution || '—'}</Descriptions.Item>
          <Descriptions.Item label="运行">{t.run_id !== '0' ? <Link to={`/runs/${t.run_id}`}>#{t.run_id}</Link> : '—'}</Descriptions.Item>
          <Descriptions.Item label="更新">{fmtTime(t.updated_at, 'YYYY-MM-DD HH:mm:ss')}</Descriptions.Item>
        </Descriptions>
      </Card>

      <Row gutter={16}>
        <Col span={14}>
          <TaskCard card={card} />
          <Collapse
            style={{ marginTop: 16 }}
            items={[
              {
                key: 'payload',
                label: '任务数据（已按调用人脱敏）',
                children: <pre style={{ margin: 0, fontSize: 12 }}>{JSON.stringify(payload, null, 2)}</pre>,
              },
            ]}
          />
          <Card title="事件时间线" size="small" style={{ marginTop: 16 }}>
            <Timeline
              items={events.map((e) => ({
                children: (
                  <>
                    <Typography.Text strong>{e.type}</Typography.Text>{' '}
                    {e.from_status !== 'TASK_STATUS_UNSPECIFIED' && <>{taskStatusLabel[e.from_status]} → </>}
                    {taskStatusLabel[e.to_status]}
                    <Typography.Text type="secondary">
                      {' '}
                      · {e.actor || 'system'} · {fmtTime(e.created_at, 'MM-DD HH:mm:ss')}
                    </Typography.Text>
                    {e.reason && <div>{e.reason}</div>}
                  </>
                ),
              }))}
            />
          </Card>
        </Col>
        <Col span={10}>
          <ActionPanel detail={detail} syncing={syncing} onChoose={setChoosing} onChanged={() => void load()} />
          <Card title="进度" size="small" style={{ marginTop: 16 }}>
            <Steps
              direction="vertical"
              size="small"
              items={detail.progress.map((s) => ({
                title: s.display_name,
                description: nodeStatusLabel[s.status] ?? s.status,
                status: stepStatus[s.status] ?? 'wait',
              }))}
            />
          </Card>
        </Col>
      </Row>

      {choosing && detail.pending_action && (
        <SignalModal
          runId={t.run_id}
          action={detail.pending_action}
          option={choosing}
          onClose={() => setChoosing(undefined)}
          onAccepted={() => {
            setChoosing(undefined);
            message.success('已提交');
            waitForSync(detail);
          }}
          onStale={(reason) => {
            setChoosing(undefined);
            message.warning(reason || '该节点已被处理');
            void load();
          }}
        />
      )}
    </>
  );
}

// 操作区：任务中心的操作（认领、开始处理、调整优先级、转派，直接生效）与流程操作（等待中的人工节点的选项）。
// 按钮是否显示按当前身份判断，权限以服务端校验为准。
function ActionPanel(props: { detail: Detail; syncing: boolean; onChoose: (o: ActionOption) => void; onChanged: () => void }) {
  const { detail, syncing, onChoose } = props;
  const t = detail.task;
  const p = detail.pending_action;
  const { user, can } = useIdentity();
  const { message } = AntApp.useApp();
  const [busy, setBusy] = useState<string>();
  const [dialog, setDialog] = useState<'priority' | 'reassign'>();

  const run = async (label: string, fn: () => Promise<unknown>) => {
    setBusy(label);
    try {
      await fn();
      message.success(`${label}成功`);
      props.onChanged();
    } catch (e) {
      message.error(e instanceof ApiError ? `${label}失败：${e.message}` : String(e));
    } finally {
      setBusy(undefined);
    }
  };

  const open = t.status !== 'TASK_STATUS_SOLVED';
  const mine = t.assignee_id !== '' && t.assignee_id === user;
  const manage = open && (mine || can('task.admin'));
  const taskActions = [
    t.status === 'TASK_STATUS_UNASSIGNED' && (
      <Button key="claim" type="primary" ghost loading={busy === '认领'} onClick={() => void run('认领', () => taskApi.claim(t.id))}>
        认领
      </Button>
    ),
    t.status === 'TASK_STATUS_PENDING' && mine && (
      <Button key="start" loading={busy === '开始处理'} onClick={() => void run('开始处理', () => taskApi.startProcessing(t.id))}>
        开始处理
      </Button>
    ),
    manage && (
      <Button key="priority" onClick={() => setDialog('priority')}>
        调整优先级
      </Button>
    ),
    manage && (
      <Button key="reassign" onClick={() => setDialog('reassign')}>
        转派
      </Button>
    ),
  ].filter(Boolean);

  return (
    <Card title="操作" size="small">
      {taskActions.length > 0 && <Space wrap style={{ marginBottom: 16 }}>{taskActions}</Space>}
      {dialog === 'priority' && (
        <PriorityModal
          current={t.priority}
          onClose={() => setDialog(undefined)}
          onSubmit={(priority, reason) => run('调整优先级', () => taskApi.updatePriority(t.id, priority, reason)).then(() => setDialog(undefined))}
        />
      )}
      {dialog === 'reassign' && (
        <ReassignModal
          current={t.assignee_id}
          onClose={() => setDialog(undefined)}
          onSubmit={(assignee, reason) => run('转派', () => taskApi.reassign(t.id, assignee, reason)).then(() => setDialog(undefined))}
        />
      )}
      {syncing && <Alert type="info" showIcon message="已提交，正在同步任务状态…" style={{ marginBottom: 12 }} />}
      {!p ? (
        <Typography.Text type="secondary">{t.status === 'TASK_STATUS_SOLVED' ? '任务已完成' : '当前没有等待你决定的流程节点'}</Typography.Text>
      ) : (
        <>
          <Typography.Paragraph>
            当前节点 <Typography.Text code>{p.node_id}</Typography.Text>
            {Number(p.deadline_at) > 0 && <> · 超时 {fmtTime(p.deadline_at)}</>}
          </Typography.Paragraph>
          {!p.can_signal && (
            <Alert
              type="warning"
              showIcon
              message={t.assignee_id ? `处理人是 ${t.assignee_id}，你只能查看` : '你不在该任务的候选组内，只能查看'}
              style={{ marginBottom: 12 }}
            />
          )}
          <Space wrap>
            {p.options.map((o) => {
              const snooze = Number(o.snooze_ms) > 0;
              const button = (
                <Button
                  key={o.key}
                  type={o.style === 'primary' ? 'primary' : 'default'}
                  danger={o.style === 'danger'}
                  disabled={!p.can_signal || snooze || syncing}
                  onClick={() => onChoose(o)}
                >
                  {o.label}
                  {snooze && `（${fmtDuration(o.snooze_ms)}）`}
                </Button>
              );
              return snooze ? (
                <Tooltip key={o.key} title="推迟即将开放">
                  {button}
                </Tooltip>
              ) : (
                button
              );
            })}
          </Space>
        </>
      )}
    </Card>
  );
}

// 选择一个选项后的确认框：表单字段在节点上、所有选项共用，必填项由选项声明。
function SignalModal(props: {
  runId: string;
  action: PendingAction;
  option: ActionOption;
  onClose: () => void;
  onAccepted: () => void;
  onStale: (reason: string) => void;
}) {
  const { action, option } = props;
  const { message } = AntApp.useApp();
  const [form] = Form.useForm<Record<string, string>>();
  const [submitting, setSubmitting] = useState(false);
  const [rejected, setRejected] = useState<string>();

  const submit = async () => {
    const values = await form.validateFields();
    const formValues: Record<string, string> = {};
    Object.entries(values).forEach(([k, v]) => {
      if (v != null && String(v).trim() !== '') formValues[k] = String(v);
    });
    setSubmitting(true);
    setRejected(undefined);
    try {
      const res = await runApi.signal({
        run_id: props.runId,
        node_id: action.node_id,
        iteration: action.iteration,
        option: option.key,
        form: formValues,
      });
      if (res.accepted) return props.onAccepted();
      switch (res.code) {
        case 'invalid_form':
          setRejected(res.reason);
          break;
        case 'unsupported':
          message.info(res.reason);
          break;
        case 'forbidden':
          message.error(res.reason);
          props.onStale(res.reason);
          break;
        default: // already_handled / stale / not_waiting：通常是在 Lark 上先处理了
          props.onStale(res.reason);
      }
    } catch (e) {
      setRejected(e instanceof ApiError ? `${e.code}: ${e.message}` : String(e));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal open title={`处理任务：${option.label}`} okText="确认提交" onOk={() => void submit()} onCancel={props.onClose} confirmLoading={submitting}>
      {rejected && <Alert type="error" showIcon message={rejected} style={{ marginBottom: 16 }} />}
      <Form form={form} layout="vertical">
        {action.form_fields.length === 0 && <Typography.Text type="secondary">确认选择「{option.label}」？</Typography.Text>}
        {action.form_fields.map((f) => (
          <Form.Item
            key={f.key}
            name={f.key}
            label={f.label}
            rules={option.required_fields.includes(f.key) ? [{ required: true, message: `请填写${f.label}` }] : []}
          >
            {f.type === 'select' ? (
              <Select allowClear options={f.options.map((o) => ({ value: o, label: o }))} />
            ) : f.type === 'textarea' ? (
              <Input.TextArea rows={3} />
            ) : (
              <Input />
            )}
          </Form.Item>
        ))}
      </Form>
    </Modal>
  );
}

function PriorityModal(props: { current: TaskPriority; onClose: () => void; onSubmit: (p: TaskPriority, reason: string) => Promise<void> }) {
  const [form] = Form.useForm<{ priority: TaskPriority; reason: string }>();
  const [saving, setSaving] = useState(false);
  const submit = async () => {
    const v = await form.validateFields();
    setSaving(true);
    await props.onSubmit(v.priority, v.reason).finally(() => setSaving(false));
  };
  return (
    <Modal open title="调整优先级" okText="确认" onOk={() => void submit()} onCancel={props.onClose} confirmLoading={saving}>
      <Form form={form} layout="vertical" initialValues={{ priority: props.current }}>
        <Form.Item name="priority" label="优先级" rules={[{ required: true }]}>
          <Select options={Object.entries(priorityLabel).map(([value, label]) => ({ value, label }))} />
        </Form.Item>
        <Form.Item name="reason" label="原因" rules={[{ required: true, whitespace: true, message: '请填写原因' }]}>
          <Input.TextArea rows={3} />
        </Form.Item>
      </Form>
    </Modal>
  );
}

// 转派：人员选择器线上直接调 BSP，demo 用 devgw 的 mock 身份列表，也可以直接输入。
function ReassignModal(props: { current: string; onClose: () => void; onSubmit: (assignee: string, reason: string) => Promise<void> }) {
  const { users } = useIdentity();
  const [form] = Form.useForm<{ assignee: string; reason: string }>();
  const [saving, setSaving] = useState(false);
  const submit = async () => {
    const v = await form.validateFields();
    setSaving(true);
    await props.onSubmit(v.assignee.trim(), v.reason).finally(() => setSaving(false));
  };
  return (
    <Modal open title="转派" okText="确认" onOk={() => void submit()} onCancel={props.onClose} confirmLoading={saving}>
      <Typography.Paragraph type="secondary">当前处理人：{props.current || '—（组队列）'}。转派只改变处理人，任务状态不变。</Typography.Paragraph>
      <Form form={form} layout="vertical">
        <Form.Item name="assignee" label="转派给" rules={[{ required: true, whitespace: true, message: '请选择处理人' }]}>
          <AutoComplete
            options={users.filter((u) => u.id !== props.current).map((u) => ({ value: u.id }))}
            filterOption={(input, o) => (o?.value ?? '').includes(input)}
            placeholder="选择或输入人员 ID"
          />
        </Form.Item>
        <Form.Item name="reason" label="原因" rules={[{ required: true, whitespace: true, message: '请填写原因' }]}>
          <Input.TextArea rows={3} />
        </Form.Item>
      </Form>
    </Modal>
  );
}
