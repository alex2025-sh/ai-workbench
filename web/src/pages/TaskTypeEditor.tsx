// 任务类型编辑页：草稿自动保存（防抖、串行、乐观锁），基本信息表单、流程画布、其余部分用 JSON 编辑；
// 校验结果点击定位；试运行针对草稿；开发环境可直接发布。
import { ArrowLeftOutlined, CheckCircleOutlined, CloudUploadOutlined, DeleteOutlined, ExperimentOutlined, LoadingOutlined, WarningOutlined } from '@ant-design/icons';
import {
  Alert,
  App as AntApp,
  Badge,
  Button,
  Card,
  Drawer,
  Empty,
  Form,
  Input,
  List,
  Modal,
  Result,
  Select,
  Space,
  Spin,
  Tabs,
  Tag,
  Tooltip,
  Typography,
} from 'antd';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { ApiError, decodeJson, encodeText } from '../api/client';
import { catalogApi, taskTypeApi } from '../api/services';
import type { Issue, NodeTypeInfo, TaskTypeBody } from '../api/types';
import { WorkflowCanvas } from '../components/canvas/WorkflowCanvas';
import { NodeConfigPanel } from '../components/canvas/NodeConfigPanel';
import { autoLayout, type Positions, type WfNode, type Workflow } from '../components/canvas/model';
import { DryRunPanel } from '../components/DryRunPanel';
import { JsonEditor } from '../components/JsonEditor';
import { useIdentity } from '../identity';

// body 是任意 JSON 对象，按需取字段。
type Body = Record<string, any>;
type SaveState = 'saved' | 'dirty' | 'saving' | 'conflict' | 'error';

const SAVE_DELAY = 800;

/** JSON 编辑的分区：页签 → body 中的字段。 */
const sections: { key: string; label: string; fields: string[]; help: string }[] = [
  { key: 'schema', label: '字段', fields: ['schema'], help: 'payload 的 JSON Schema：字段、类型、x-pii 脱敏、x-format 格式。' },
  { key: 'sources', label: '来源', fields: ['sources', 'dedup_key'], help: '任务从哪里来（cron 扫描 / Kafka / API）与去重键。' },
  { key: 'views', label: '卡片模板', fields: ['views', 'view'], help: '自有卡片模板（views）与任务类型默认模板（view）。' },
  { key: 'assignment', label: '处理人', fields: ['assignment'], help: '任务类型默认的分配策略；节点上未配置时使用。' },
];

export default function TaskTypeEditor() {
  const { key = '' } = useParams();
  const navigate = useNavigate();
  const { message, modal } = AntApp.useApp();
  const { can } = useIdentity();

  const [body, setBodyState] = useState<Body>();
  const [base, setBase] = useState(0);
  const [loadError, setLoadError] = useState<Error>();
  const [catalog, setCatalog] = useState<NodeTypeInfo[]>([]);
  const [skills, setSkills] = useState<string[]>([]);
  const [state, setState] = useState<SaveState>('saved');
  const [issues, setIssues] = useState<Issue[]>([]);
  const [issuesOpen, setIssuesOpen] = useState(false);
  const [tab, setTab] = useState('workflow');
  const [selected, setSelected] = useState<string>();
  const [dryRunOpen, setDryRunOpen] = useState(false);
  const [busy, setBusy] = useState<string>();

  // 保存用的最新值放在 ref 里：防抖、串行保存都读它。
  const latest = useRef<Body>();
  const revision = useRef('0');
  const timer = useRef<number>();
  const inflight = useRef<Promise<void>>();
  const pending = useRef(false);

  const load = useCallback(async () => {
    try {
      const [d, nt, sk] = await Promise.all([taskTypeApi.getDraft(key), catalogApi.nodeTypes(), catalogApi.skills()]);
      let b = decodeJson<Body>(d.draft.body_json) ?? {};
      // 没有坐标（如 YAML 发布的任务类型）时首次打开自动排版；坐标只是展示数据，下次保存时一并存下。
      if (!b.ui?.canvas?.positions && b.workflow) b = { ...b, ui: { ...(b.ui ?? {}), canvas: { positions: autoLayout(b.workflow) } } };
      latest.current = b;
      revision.current = d.draft.revision;
      setBodyState(b);
      setBase(d.draft.base_version);
      setCatalog(nt.items);
      setSkills(sk.items.map((s) => s.name));
      setState('saved');
      setLoadError(undefined);
    } catch (e) {
      setLoadError(e as Error);
    }
  }, [key]);

  useEffect(() => {
    void load();
    return () => window.clearTimeout(timer.current);
  }, [load]);

  // 串行保存：同一时刻只有一个请求在途，期间的修改合并成最后一份，上一个返回后再发。
  const save = useCallback(async (): Promise<void> => {
    if (inflight.current) {
      pending.current = true;
      return inflight.current;
    }
    const run = async () => {
      setState('saving');
      try {
        const res = await taskTypeApi.saveDraft(key, revision.current, encodeText(JSON.stringify(latest.current)));
        revision.current = res.revision;
        setIssues(res.issues);
        setState(pending.current ? 'dirty' : 'saved');
      } catch (e) {
        if (e instanceof ApiError && e.code === 'Aborted') {
          setState('conflict');
          pending.current = false;
          return;
        }
        setState('error');
        message.error(e instanceof ApiError ? `保存失败：${e.message}` : String(e));
      }
    };
    inflight.current = run().finally(() => {
      inflight.current = undefined;
    });
    await inflight.current;
    if (pending.current) {
      pending.current = false;
      await save();
    }
  }, [key, message]);

  const setBody = (next: Body) => {
    latest.current = next;
    setBodyState(next);
    if (state === 'conflict') return; // 冲突未处理前不再自动保存
    setState('dirty');
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => void save(), SAVE_DELAY);
  };

  /** 立即保存未保存的修改（校验、试运行、发布前调用）。 */
  const flush = async () => {
    window.clearTimeout(timer.current);
    if (state === 'conflict') throw new Error('草稿有冲突，请先处理');
    if (state === 'dirty' || state === 'error' || inflight.current) await save();
  };

  // ---- 派生数据 ----
  const workflow: Workflow = (body?.workflow as Workflow) ?? { nodes: [], edges: [] };
  const positions: Positions = body?.ui?.canvas?.positions ?? {};
  const views = Object.keys(body?.views ?? {});
  const info = useMemo(() => Object.fromEntries(catalog.map((c) => [c.type, c])), [catalog]);
  const { issueNodes, issueEdges } = useMemo(() => {
    const nodes = new Set<string>();
    const edges = new Set<string>();
    for (const i of issues.filter((x) => x.severity === 'error')) {
      if (i.node_id) nodes.add(i.node_id);
      if (i.edge_id) edges.add(i.edge_id);
      const m = /^workflow\.(nodes|edges)\.([^.]+)/.exec(i.path ?? '');
      if (m) (m[1] === 'nodes' ? nodes : edges).add(m[2]);
    }
    return { issueNodes: nodes, issueEdges: edges };
  }, [issues]);
  const errors = issues.filter((i) => i.severity === 'error').length;

  if (loadError) {
    const notFound = loadError instanceof ApiError && loadError.code === 'NotFound';
    return <Result status={notFound ? '404' : 'error'} title={notFound ? '任务类型不存在' : '加载失败'} subTitle={loadError.message} extra={<Link to="/config/task-types">返回任务类型</Link>} />;
  }
  if (!body) return <Spin style={{ display: 'block', marginTop: 80 }} />;

  const setWorkflow = (wf: Workflow, pos: Positions) =>
    setBody({ ...body, workflow: wf, ui: { ...(body.ui ?? {}), canvas: { ...(body.ui?.canvas ?? {}), positions: pos } } });

  const updateNode = (n: WfNode) => setWorkflow({ ...workflow, nodes: workflow.nodes.map((x) => (x.id === n.id ? n : x)) }, positions);

  const renameNode = (from: string, to: string) => {
    const pos = { ...positions };
    if (pos[from]) {
      pos[to] = pos[from];
      delete pos[from];
    }
    setWorkflow(
      {
        nodes: workflow.nodes.map((n) => (n.id === from ? { ...n, id: to } : n)),
        edges: workflow.edges.map((e) => ({ ...e, from: e.from === from ? to : e.from, to: e.to === from ? to : e.to })),
      },
      pos,
    );
    setSelected(to);
  };

  const deleteNode = (id: string) => {
    const pos = { ...positions };
    delete pos[id];
    setWorkflow({ nodes: workflow.nodes.filter((n) => n.id !== id), edges: workflow.edges.filter((e) => e.from !== id && e.to !== id) }, pos);
    setSelected(undefined);
  };

  // ---- 操作 ----
  const validate = async () => {
    setBusy('validate');
    try {
      await flush();
      const res = await taskTypeApi.validateDraft(key);
      setIssues(res.issues);
      setIssuesOpen(true);
      if (res.ok) message.success(res.issues.length ? '校验通过（有警告）' : '校验通过');
    } catch (e) {
      message.error(e instanceof ApiError ? `校验失败：${e.message}` : String(e));
    } finally {
      setBusy(undefined);
    }
  };

  const publish = async () => {
    setBusy('publish');
    try {
      await flush();
      const res = await taskTypeApi.publishDraft(key, revision.current);
      if (res.version > 0) {
        message.success(`已发布 v${res.version}`);
        navigate(`/config/task-types/${key}`);
      } else {
        setIssues(res.issues);
        setIssuesOpen(true);
        message.error('草稿有错误，未发布');
      }
    } catch (e) {
      message.error(e instanceof ApiError ? `发布失败：${e.message}` : String(e));
    } finally {
      setBusy(undefined);
    }
  };

  const discard = () =>
    modal.confirm({
      title: '丢弃草稿？',
      content: base > 0 ? `将丢弃全部未发布的修改，回到 v${base}。` : '这个任务类型还没有发布过，丢弃后草稿内容全部清空。',
      okButtonProps: { danger: true },
      okText: '丢弃',
      onOk: async () => {
        window.clearTimeout(timer.current);
        await inflight.current;
        await taskTypeApi.discardDraft(key, revision.current);
        message.success('已丢弃草稿');
        navigate(base > 0 ? `/config/task-types/${key}` : '/config/task-types');
      },
    });

  const resolveConflict = () =>
    modal.confirm({
      title: '草稿已被修改',
      content: '有人（或另一个页面）在你之后保存了这份草稿。你可以先导出自己的修改，再以服务端为准重新加载。',
      okText: '重新加载',
      cancelText: '导出我的修改',
      onOk: () => load(),
      onCancel: () => {
        const blob = new Blob([JSON.stringify(latest.current, null, 2)], { type: 'application/json' });
        const a = document.createElement('a');
        a.href = URL.createObjectURL(blob);
        a.download = `${key}.draft.json`;
        a.click();
      },
    });

  const locate = (i: Issue) => {
    const m = /^workflow\.(nodes|edges)\.([^.]+)/.exec(i.path ?? '');
    const node = i.node_id || (m?.[1] === 'nodes' ? m[2] : undefined);
    if (node || i.edge_id || m) {
      setTab('workflow');
      if (node) setSelected(node);
      return;
    }
    const top = (i.path ?? '').split('.')[0];
    const sec = sections.find((s) => s.fields.includes(top));
    setTab(sec ? sec.key : 'basic');
  };

  const selectedNode = workflow.nodes.find((n) => n.id === selected);

  return (
    <>
      <Card size="small" style={{ marginBottom: 12 }} styles={{ body: { padding: '8px 16px' } }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 16, flexWrap: 'wrap' }}>
          <Link to={base > 0 ? `/config/task-types/${key}` : '/config/task-types'}>
            <ArrowLeftOutlined />
          </Link>
          <Typography.Title level={5} style={{ margin: 0 }}>
            {body.name || key} <Typography.Text type="secondary">({key})</Typography.Text>
          </Typography.Title>
          <Tag color="gold">草稿 · {base > 0 ? `基于 v${base}` : '未发布'}</Tag>
          <SaveIndicator state={state} onConflict={resolveConflict} onRetry={() => void save()} />
          <div style={{ flex: 1 }} />
          <Space wrap>
            <Badge count={errors} size="small">
              <Button icon={<CheckCircleOutlined />} loading={busy === 'validate'} onClick={() => void validate()}>
                校验
              </Button>
            </Badge>
            <Button icon={<ExperimentOutlined />} onClick={() => setDryRunOpen(true)}>
              试运行
            </Button>
            <Button icon={<DeleteOutlined />} danger onClick={discard}>
              丢弃草稿
            </Button>
            {can('task_type.publish') && (
              <Tooltip title="开发环境：跳过变更集与审批，直接发布为新版本">
                <Button type="primary" icon={<CloudUploadOutlined />} loading={busy === 'publish'} onClick={() => void publish()}>
                  发布
                </Button>
              </Tooltip>
            )}
          </Space>
        </div>
      </Card>

      <Tabs
        tabPosition="left"
        activeKey={tab}
        onChange={setTab}
        items={[
          { key: 'basic', label: '基本信息', children: <BasicForm body={body} onChange={setBody} /> },
          {
            key: 'workflow',
            label: '流程',
            children: (
              <div style={{ display: 'flex', gap: 12, height: 'calc(100vh - 260px)', minHeight: 520 }}>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <WorkflowCanvas
                    workflow={workflow}
                    positions={positions}
                    catalog={catalog}
                    issueNodes={issueNodes}
                    issueEdges={issueEdges}
                    selected={selected}
                    onSelect={setSelected}
                    onChange={setWorkflow}
                  />
                </div>
                <div style={{ width: 360 }}>
                  {selectedNode ? (
                    <NodeConfigPanel
                      key={selectedNode.id}
                      node={selectedNode}
                      info={info[selectedNode.type]}
                      takenIds={workflow.nodes.map((n) => n.id)}
                      views={views}
                      skills={skills}
                      onChange={updateNode}
                      onRename={(to) => renameNode(selectedNode.id, to)}
                      onDelete={() => deleteNode(selectedNode.id)}
                    />
                  ) : (
                    <Card size="small" style={{ height: '100%' }}>
                      <Empty description="选中节点后在这里配置；从左侧拖入新节点，从节点下方的出口拖到其他节点连线；点中连线后按 Delete 或点中点的 × 删除。" />
                    </Card>
                  )}
                </div>
              </div>
            ),
          },
          ...sections.map((sec) => ({
            key: sec.key,
            label: sec.label,
            children: <JsonSection body={body} section={sec} onChange={setBody} />,
          })),
        ]}
      />

      <Card
        size="small"
        style={{ marginTop: 12 }}
        title={
          <Space>
            校验结果
            {errors > 0 ? <Tag color="error">{errors} 个错误</Tag> : <Tag color="success">没有错误</Tag>}
            {issues.length - errors > 0 && <Tag color="warning">{issues.length - errors} 个警告</Tag>}
          </Space>
        }
        extra={
          <Button size="small" type="link" onClick={() => setIssuesOpen(!issuesOpen)}>
            {issuesOpen ? '收起' : '展开'}
          </Button>
        }
        styles={{ body: { display: issuesOpen ? undefined : 'none', maxHeight: 220, overflow: 'auto' } }}
      >
        <List
          size="small"
          dataSource={issues}
          locale={{ emptyText: '没有问题' }}
          renderItem={(i) => (
            <List.Item style={{ cursor: 'pointer' }} onClick={() => locate(i)}>
              <Space>
                {i.severity === 'error' ? <Tag color="error">错误</Tag> : <Tag color="warning">警告</Tag>}
                <Typography.Text code>{i.path || i.node_id || i.edge_id || '—'}</Typography.Text>
                <Typography.Text>{i.message}</Typography.Text>
              </Space>
            </List.Item>
          )}
        />
      </Card>

      <Drawer title="试运行（草稿）" width={1100} open={dryRunOpen} onClose={() => setDryRunOpen(false)} destroyOnClose>
        <DryRunPanel taskKey={key} body={body as TaskTypeBody} beforeRun={flush} />
      </Drawer>
    </>
  );
}

function SaveIndicator(props: { state: SaveState; onConflict: () => void; onRetry: () => void }) {
  switch (props.state) {
    case 'saving':
      return <Typography.Text type="secondary"><LoadingOutlined /> 保存中…</Typography.Text>;
    case 'dirty':
      return <Typography.Text type="secondary">有未保存的修改</Typography.Text>;
    case 'conflict':
      return (
        <Button size="small" danger icon={<WarningOutlined />} onClick={props.onConflict}>
          草稿冲突，点击处理
        </Button>
      );
    case 'error':
      return (
        <Button size="small" danger onClick={props.onRetry}>
          保存失败，重试
        </Button>
      );
    default:
      return <Typography.Text type="success"><CheckCircleOutlined /> 已保存</Typography.Text>;
  }
}

/** 基本信息：key 不可修改，其余字段直接写回 body。 */
function BasicForm(props: { body: Body; onChange: (b: Body) => void }) {
  const { body } = props;
  const set = (k: string, v: unknown) => {
    const next = { ...body };
    if (v === undefined || v === '') delete next[k];
    else next[k] = v;
    props.onChange(next);
  };
  return (
    <Card size="small" style={{ maxWidth: 720 }}>
      <Form layout="vertical">
        <Form.Item label="Key" extra="任务类型的稳定标识，创建后不能修改">
          <Input value={body.key} disabled />
        </Form.Item>
        <Form.Item label="名称" required>
          <Input value={body.name ?? ''} onChange={(e) => set('name', e.target.value)} />
        </Form.Item>
        <Form.Item label="Namespace">
          <Input value={body.namespace ?? ''} onChange={(e) => set('namespace', e.target.value)} />
        </Form.Item>
        <Form.Item label="描述">
          <Input.TextArea rows={3} value={body.description ?? ''} onChange={(e) => set('description', e.target.value)} />
        </Form.Item>
        <Form.Item label="默认优先级">
          <Select
            allowClear
            value={body.default_priority}
            placeholder="默认 P2"
            onChange={(v) => set('default_priority', v)}
            options={['P0', 'P1', 'P2', 'P3'].map((p) => ({ value: p, label: p }))}
            style={{ width: 160 }}
          />
        </Form.Item>
      </Form>
    </Card>
  );
}

/** 用 JSON 编辑 body 中的一组字段。 */
function JsonSection(props: { body: Body; section: (typeof sections)[number]; onChange: (b: Body) => void }) {
  const { body, section } = props;
  const single = section.fields.length === 1;
  const value = single ? body[section.fields[0]] : Object.fromEntries(section.fields.filter((f) => body[f] !== undefined).map((f) => [f, body[f]]));
  const apply = (v: unknown) => {
    const next = { ...body };
    if (single) {
      if (v === undefined) delete next[section.fields[0]];
      else next[section.fields[0]] = v;
    } else {
      const obj = (v && typeof v === 'object' ? v : {}) as Body;
      for (const f of section.fields) {
        if (obj[f] === undefined) delete next[f];
        else next[f] = obj[f];
      }
    }
    props.onChange(next);
  };
  return (
    <Card size="small">
      <Alert type="info" showIcon message={section.help} description={single ? undefined : `编辑对象中的 ${section.fields.join('、')} 字段。`} style={{ marginBottom: 12 }} />
      <JsonEditor value={value} onChange={apply} height="calc(100vh - 380px)" />
    </Card>
  );
}

/** 新建任务类型的对话框：成功后进入编辑页。 */
export function CreateTaskTypeModal(props: { open: boolean; onClose: () => void }) {
  const navigate = useNavigate();
  const { message } = AntApp.useApp();
  const [form] = Form.useForm<{ key: string; name: string; namespace: string; description?: string }>();
  const [saving, setSaving] = useState(false);
  const submit = async () => {
    const v = await form.validateFields();
    setSaving(true);
    try {
      await taskTypeApi.create(v);
      message.success('已创建草稿');
      navigate(`/config/task-types/${v.key}/edit`);
    } catch (e) {
      message.error(e instanceof ApiError ? (e.code === 'AlreadyExists' ? 'key 已被使用' : e.message) : String(e));
    } finally {
      setSaving(false);
    }
  };
  return (
    <Modal open={props.open} title="新建任务类型" okText="创建" onOk={() => void submit()} onCancel={props.onClose} confirmLoading={saving} destroyOnClose>
      <Form form={form} layout="vertical" preserve={false}>
        <Form.Item
          name="key"
          label="Key"
          extra="小写字母开头，只含小写字母、数字、下划线；创建后不能修改"
          rules={[{ required: true, pattern: /^[a-z][a-z0-9_]{1,63}$/, message: '格式不正确' }]}
        >
          <Input placeholder="如 vip_ticket_alert" />
        </Form.Item>
        <Form.Item name="name" label="名称" rules={[{ required: true, whitespace: true }]}>
          <Input />
        </Form.Item>
        <Form.Item name="namespace" label="Namespace" rules={[{ required: true, whitespace: true }]}>
          <Input placeholder="业务线或团队，如 cs_vip" />
        </Form.Item>
        <Form.Item name="description" label="描述">
          <Input.TextArea rows={2} />
        </Form.Item>
      </Form>
    </Modal>
  );
}
