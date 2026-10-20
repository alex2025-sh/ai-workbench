// 任务类型编辑 /config/task-types/:key/edit：四步表单（数据源 → 字段 → 节点 → 卡片），全程不需要写 JSON。
//
// - 草稿自动保存：修改后 800ms 保存，同一时刻只有一个保存请求，期间的修改合并到下一次；保存返回的问题
//   标在对应步骤与表单项上。revision 冲突时保留本地修改，让用户选择以服务端为准重新加载。
// - 校验：完整校验（含团队、数据源连接、业务关系是否存在）；发布：有问题时不发布，问题列在页面上。
import { ArrowLeftOutlined, CheckCircleOutlined, CodeOutlined, DeleteOutlined, SendOutlined } from '@ant-design/icons';
import { Alert, App as AntApp, Button, Card, Drawer, Form, Input, Modal, Result, Space, Spin, Steps, Tag, Tooltip, Typography } from 'antd';
import { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { ApiError, decodeJson, encodeJson } from '../api/client';
import { taskTypeApi } from '../api/services';
import type { DraftResp, Issue, TaskTypeBody } from '../api/types';
import { JsonEditor } from '../components/JsonEditor';
import { CardStep } from '../components/tasktype/CardStep';
import { FieldsStep } from '../components/tasktype/FieldsStep';
import { NodesStep } from '../components/tasktype/NodesStep';
import { stepOf, steps } from '../components/tasktype/shared';
import { SourceStep, type Trial } from '../components/tasktype/SourceStep';
import { useIdentity } from '../identity';

type SaveState = 'saved' | 'dirty' | 'saving' | 'error' | 'conflict';

export default function TaskTypeEditor() {
  const { key = '' } = useParams();
  const navigate = useNavigate();
  const { message, modal } = AntApp.useApp();
  const { can } = useIdentity();
  const [draft, setDraft] = useState<DraftResp>();
  const [body, setBody] = useState<TaskTypeBody>();
  const [issues, setIssues] = useState<Issue[]>([]);
  const [save, setSave] = useState<SaveState>('saved');
  const [step, setStep] = useState(0);
  const [node, setNode] = useState<string>();
  const [trial, setTrial] = useState<Trial>();
  const [jsonOpen, setJsonOpen] = useState(false);
  const [busy, setBusy] = useState<string>();
  const [error, setError] = useState<Error>();

  // 串行保存：revision 与待保存的内容放在 ref 里，避免闭包拿到旧值。
  const revision = useRef('0');
  const pending = useRef<TaskTypeBody>();
  const inflight = useRef<Promise<void>>();
  const timer = useRef<number>();

  const load = useCallback(() => {
    taskTypeApi.getDraft(key).then(
      (d) => {
        setDraft(d);
        revision.current = d.revision;
        setBody(decodeJson<TaskTypeBody>(d.body_json) ?? undefined);
        setIssues(d.issues);
        setSave('saved');
      },
      (e) => setError(e as Error),
    );
  }, [key]);
  useEffect(load, [load]);

  const flush = useCallback(async (): Promise<void> => {
    window.clearTimeout(timer.current);
    if (inflight.current) await inflight.current;
    const b = pending.current;
    if (!b) return;
    pending.current = undefined;
    setSave('saving');
    inflight.current = taskTypeApi
      .saveDraft(key, revision.current, encodeJson(b))
      .then(
        (r) => {
          revision.current = r.revision;
          setIssues(r.issues);
          setSave(pending.current ? 'dirty' : 'saved');
        },
        (e) => {
          if (e instanceof ApiError && e.code === 'Aborted') {
            setSave('conflict');
            return;
          }
          pending.current = pending.current ?? b; // 失败时保留，下次修改或手动保存时重试
          setSave('error');
          message.error(`保存失败：${(e as Error).message}`);
        },
      )
      .finally(() => {
        inflight.current = undefined;
      });
    await inflight.current;
    if (pending.current) await flush();
  }, [key, message]);

  const change = (b: TaskTypeBody) => {
    setBody(b);
    pending.current = b;
    setSave('dirty');
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => void flush(), 800);
  };
  useEffect(() => () => window.clearTimeout(timer.current), []);

  const validate = async () => {
    setBusy('validate');
    try {
      await flush();
      const r = await taskTypeApi.validateDraft(key);
      setIssues(r.issues);
      if (r.issues.length === 0) message.success('校验通过');
      else message.warning(`有 ${r.issues.length} 个问题`);
    } finally {
      setBusy(undefined);
    }
  };

  const publish = () =>
    modal.confirm({
      title: '发布为新版本？',
      content: '发布后立即生效：数据源按新版本执行；进行中的任务按创建时的版本走完节点链。',
      okText: '发布',
      cancelText: '取消',
      onOk: async () => {
        await flush();
        try {
          const r = await taskTypeApi.publish(key);
          if (r.issues.length > 0 || !r.version) {
            setIssues(r.issues);
            setStep(stepOf(r.issues[0]?.path ?? ''));
            message.error('有问题未解决，没有发布');
            return;
          }
          message.success(`已发布 v${r.version.version}`);
          navigate(`/config/task-types/${key}`);
        } catch (e) {
          message.error(`发布失败：${(e as Error).message}`);
        }
      },
    });

  const discard = () =>
    modal.confirm({
      title: '丢弃草稿？',
      content: draft?.base_version ? `回到已发布的 v${draft.base_version}。` : '这个任务类型还没有发布过，会被删除。',
      okText: '丢弃',
      okButtonProps: { danger: true },
      cancelText: '取消',
      onOk: async () => {
        window.clearTimeout(timer.current);
        pending.current = undefined;
        await taskTypeApi.discardDraft(key);
        navigate(draft?.base_version ? `/config/task-types/${key}` : '/config/task-types');
      },
    });

  if (error) {
    const nf = error instanceof ApiError && error.code === 'NotFound';
    return <Result status={nf ? '404' : 'error'} title={nf ? '任务类型不存在' : '加载失败'} subTitle={error.message} />;
  }
  if (!draft || !body) return <Spin style={{ display: 'block', marginTop: 80 }} />;

  const stepIssues = (i: number) => issues.filter((x) => stepOf(x.path) === i);
  const saveText: Record<SaveState, React.ReactNode> = {
    saved: <Typography.Text type="secondary">已保存</Typography.Text>,
    dirty: <Typography.Text type="secondary">未保存</Typography.Text>,
    saving: <Typography.Text type="secondary">保存中…</Typography.Text>,
    error: <Typography.Text type="danger">保存失败</Typography.Text>,
    conflict: <Typography.Text type="danger">草稿已被他人修改</Typography.Text>,
  };
  const content = [
    <SourceStep key="s" body={body} onChange={change} issues={issues} trial={trial} onTrial={setTrial} />,
    <FieldsStep key="f" body={body} onChange={change} issues={issues} />,
    <NodesStep key="n" body={body} onChange={change} issues={issues} selected={node} onSelect={setNode} />,
    <CardStep key="c" body={body} onChange={change} issues={issues} sampleRow={trial?.rows[0]} />,
  ][step];

  return (
    <>
      <Space style={{ marginBottom: 12 }}>
        <Link to={draft.base_version ? `/config/task-types/${key}` : '/config/task-types'}>
          <ArrowLeftOutlined /> {draft.base_version ? '返回已发布版本' : '任务类型'}
        </Link>
      </Space>
      <Card size="small" style={{ marginBottom: 16 }}>
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 12, justifyContent: 'space-between' }}>
          <Space wrap>
            <Typography.Title level={4} style={{ margin: 0 }}>
              {body.name || key}
            </Typography.Title>
            <Typography.Text type="secondary" code>
              {key}
            </Typography.Text>
            <Tag color="gold">{draft.base_version ? `草稿 · 基于 v${draft.base_version}` : '草稿 · 未发布'}</Tag>
            {saveText[save]}
          </Space>
          <Space wrap>
            <Button icon={<CodeOutlined />} onClick={() => setJsonOpen(true)}>
              JSON
            </Button>
            <Button icon={<CheckCircleOutlined />} loading={busy === 'validate'} onClick={() => void validate()}>
              校验
            </Button>
            <Button danger icon={<DeleteOutlined />} onClick={discard}>
              丢弃草稿
            </Button>
            <Tooltip title={can('task_type.publish') ? '' : '需要 task_type.publish 权限'}>
              <Button type="primary" icon={<SendOutlined />} disabled={!can('task_type.publish')} onClick={publish}>
                发布
              </Button>
            </Tooltip>
          </Space>
        </div>
      </Card>
      {save === 'conflict' && (
        <Alert
          type="error"
          showIcon
          style={{ marginBottom: 16 }}
          message="草稿已被他人修改，你的修改没有保存"
          action={
            <Space>
              <Button size="small" onClick={() => setJsonOpen(true)}>
                查看我的 JSON
              </Button>
              <Button size="small" danger onClick={load}>
                以服务端为准重新加载
              </Button>
            </Space>
          }
        />
      )}
      <Steps
        current={step}
        onChange={setStep}
        style={{ marginBottom: 16 }}
        items={steps.map((s, i) => ({
          title: s.title,
          description: stepIssues(i).length ? `${stepIssues(i).length} 个问题` : s.description,
          status: stepIssues(i).length ? 'error' : i === step ? 'process' : i < step ? 'finish' : 'wait',
        }))}
      />
      {content}
      <Space style={{ marginTop: 16, width: '100%', justifyContent: 'space-between' }}>
        <Button disabled={step === 0} onClick={() => setStep(step - 1)}>
          上一步
        </Button>
        {step < steps.length - 1 ? (
          <Button type="primary" onClick={() => setStep(step + 1)}>
            下一步：{steps[step + 1].title}
          </Button>
        ) : (
          <Typography.Text type="secondary">{issues.length ? `还有 ${issues.length} 个问题` : '没有问题，可以发布'}</Typography.Text>
        )}
      </Space>
      <Drawer title="任务类型 JSON（只读）" width={720} open={jsonOpen} onClose={() => setJsonOpen(false)}>
        <JsonEditor value={body} onChange={() => undefined} readOnly height="calc(100vh - 140px)" />
      </Drawer>
    </>
  );
}

/** 新建任务类型：填 key、名称、命名空间，生成一份最小草稿后进入编辑页。 */
export function CreateTaskTypeModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [form] = Form.useForm<{ key: string; name: string; namespace: string; description?: string }>();
  const navigate = useNavigate();
  const { message } = AntApp.useApp();
  const [saving, setSaving] = useState(false);
  return (
    <Modal
      title="新建任务类型"
      open={open}
      okText="创建"
      cancelText="取消"
      confirmLoading={saving}
      onCancel={onClose}
      onOk={async () => {
        const v = await form.validateFields();
        setSaving(true);
        try {
          await taskTypeApi.create(v);
          navigate(`/config/task-types/${v.key}/edit`);
        } catch (e) {
          message.error((e as Error).message);
        } finally {
          setSaving(false);
        }
      }}
      destroyOnClose
    >
      <Form form={form} layout="vertical" preserve={false}>
        <Form.Item name="key" label="标识（key）" rules={[{ required: true, pattern: /^[a-z][a-z0-9_]{0,63}$/, message: '小写字母开头，只能有小写字母、数字、下划线' }]}>
          <Input placeholder="如 vip_appeal_pending，创建后不可修改" />
        </Form.Item>
        <Form.Item name="name" label="名称" rules={[{ required: true }]}>
          <Input placeholder="如 VIP 工单待审核" />
        </Form.Item>
        <Form.Item name="namespace" label="命名空间" rules={[{ required: true }]}>
          <Input placeholder="团队或业务域，如 cs_vip" />
        </Form.Item>
        <Form.Item name="description" label="描述">
          <Input.TextArea rows={2} />
        </Form.Item>
      </Form>
    </Modal>
  );
}
