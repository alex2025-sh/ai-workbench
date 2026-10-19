// 任务类型定义（只读）：把任务类型 JSON 按读者关心的问题分区展示，可切到 JSON 原文。
//
// 分区：概览（是什么、默认值）→ 字段（任务带哪些数据）→ 来源（任务从哪来）→ 流程（怎么推进）→
// 处理人与触达（每一步通知谁、发什么卡片）→ 卡片模板（处理人看到什么）。
// 分区之间可以互相跳转：点卡片模板名滚到模板预览，点节点名在画布中选中它。
// 节点实际使用的卡片模板与处理人取编译结果（bindings），继承来的配置标明来源，不必自己推算继承规则。
import { CopyOutlined, DownloadOutlined, QuestionCircleOutlined } from '@ant-design/icons';
import { Anchor, App as AntApp, Button, Card, Col, Descriptions, Empty, Grid, Row, Segmented, Space, Spin, Table, Tag, Tooltip, Typography } from 'antd';
import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { catalogApi } from '../../api/services';
import type { NodeTypeInfo } from '../../api/types';
import { DefinitionCanvas } from '../canvas/DefinitionCanvas';
import type { WfNode } from '../canvas/model';
import { JsonEditor } from '../JsonEditor';
import { durationText } from './text';
import type { Binding, Definition, SchemaProp, Source } from './model';
import { viewNames } from './model';
import { NodeDetail, NodeList } from './NodeDetail';
import { ReadonlyObject } from './ReadonlyValue';
import { AssignmentView, BindingFrom, DefinitionNav, NodeRef, ViewRef } from './refs';
import { cronText, Expr, FieldName, FieldTitles, Tmpl } from './text';
import { ViewPreview } from './ViewPreview';

const typeLabel: Record<string, string> = { string: '文本', integer: '整数', number: '数字', boolean: '是 / 否', object: '对象', array: '列表' };
const formatLabel: Record<string, string> = { money: '金额', percent: '百分比', duration: '时长', datetime: '日期时间' };
const piiLabel: Record<string, string> = { mask_middle: '中间打码', hash: '哈希', drop: '不外发' };
const sourceKind: Record<string, string> = { cron: '定时扫描', kafka: 'Kafka 事件', api: 'API 推送' };

const sections = [
  { key: 'overview', title: '概览' },
  { key: 'fields', title: '字段' },
  { key: 'sources', title: '来源' },
  { key: 'workflow', title: '流程' },
  { key: 'delivery', title: '处理人与触达' },
  { key: 'views', title: '卡片模板' },
];

const labelStyle = { width: 128, color: 'rgba(0, 0, 0, 0.55)', verticalAlign: 'top' } as const;

function Help({ text }: { text: string }) {
  return (
    <Tooltip title={text}>
      <QuestionCircleOutlined style={{ marginInlineStart: 4, color: 'rgba(0,0,0,0.45)' }} />
    </Tooltip>
  );
}

function Section(props: { id: string; title: string; extra?: ReactNode; children: ReactNode }) {
  return (
    <Card id={`def-${props.id}`} title={props.title} extra={props.extra} size="small" style={{ marginBottom: 16, scrollMarginTop: 16 }}>
      {props.children}
    </Card>
  );
}

export function TaskTypeDefinition(props: { body: Definition; bindings: Record<string, Binding>; version: number }) {
  const { body: d, bindings } = props;
  const { message } = AntApp.useApp();
  const screens = Grid.useBreakpoint();
  const [mode, setMode] = useState<'view' | 'json'>('view');
  const [catalog, setCatalog] = useState<NodeTypeInfo[]>();
  const [skills, setSkills] = useState<Record<string, string>>({});
  const [selected, setSelected] = useState<string>();
  const [focus, setFocus] = useState<{ id: string; at: number }>();
  const [flash, setFlash] = useState<string>();

  // 目录只用于显示名称，取不到时退回原始类型名。
  useEffect(() => {
    catalogApi.nodeTypes().then((r) => setCatalog(r.items), () => setCatalog([]));
    catalogApi.skills().then((r) => setSkills(Object.fromEntries(r.items.map((s) => [s.name, s.display_name]))), () => undefined);
  }, []);

  const info = useMemo(() => Object.fromEntries((catalog ?? []).map((c) => [c.type, c])), [catalog]);
  const props_ = useMemo(() => d.schema?.properties ?? {}, [d]);
  // 存储的 JSON 不保证对象键的顺序：必填字段按 required 的顺序在前，其余按 key 排序，来源映射同序。
  const fieldOrder = useMemo(() => {
    const req = (d.schema?.required ?? []).filter((k) => k in props_);
    return [...req, ...Object.keys(props_).filter((k) => !req.includes(k)).sort()];
  }, [d, props_]);
  const titles = useMemo(() => Object.fromEntries(Object.entries(props_).map(([k, p]) => [k, p.title ?? k])), [props_]);
  const wf = d.workflow ?? { nodes: [], edges: [] };
  const nodeById = useMemo(() => Object.fromEntries(wf.nodes.map((n) => [n.id, n])), [wf]);
  const bound = wf.nodes.filter((n) => bindings[n.id]);

  const nav = useMemo(
    () => ({
      view: (name: string) => {
        document.getElementById(`view-${name}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' });
        setFlash(name);
        window.setTimeout(() => setFlash((f) => (f === name ? undefined : f)), 1600);
      },
      node: (id: string) => {
        setSelected(id);
        setFocus({ id, at: Date.now() });
        document.getElementById('def-workflow')?.scrollIntoView({ behavior: 'smooth', block: 'start' });
      },
      nodeName: (id: string) => nodeById[id]?.name || id,
    }),
    [nodeById],
  );

  const json = useMemo(() => JSON.stringify(d, null, 2), [d]);
  const toolbar = (
    <Space style={{ marginBottom: 16 }} wrap>
      <Segmented
        value={mode}
        onChange={(v) => setMode(v as 'view' | 'json')}
        options={[
          { value: 'view', label: '结构化' },
          { value: 'json', label: 'JSON' },
        ]}
      />
      {mode === 'json' && (
        <>
          <Button
            icon={<CopyOutlined />}
            onClick={() => {
              void navigator.clipboard?.writeText(json);
              message.success('已复制');
            }}
          >
            复制
          </Button>
          <Button
            icon={<DownloadOutlined />}
            onClick={() => {
              const a = document.createElement('a');
              a.href = URL.createObjectURL(new Blob([json], { type: 'application/json' }));
              a.download = `${d.key}.v${props.version}.json`;
              a.click();
              URL.revokeObjectURL(a.href);
            }}
          >
            下载
          </Button>
        </>
      )}
    </Space>
  );

  if (mode === 'json') {
    return (
      <>
        {toolbar}
        <JsonEditor value={d} onChange={() => undefined} readOnly height="70vh" />
      </>
    );
  }

  const nodeCount = wf.nodes.length;
  const humanCount = wf.nodes.filter((n) => n.type === 'human_choice').length;
  const viewEntries = Object.entries(d.views ?? {});

  const overview = (
    <Descriptions
      bordered
      size="small"
      column={screens.md ? 2 : 1}
      labelStyle={labelStyle}
      items={[
        { key: 'key', label: '标识', children: <Typography.Text copyable code>{d.key}</Typography.Text> },
        { key: 'ns', label: '命名空间', children: d.namespace || '—' },
        { key: 'priority', label: '默认优先级', children: <Tag>{d.default_priority ?? 'P2'}</Tag> },
        {
          key: 'scale',
          label: '规模',
          children: `${Object.keys(props_).length} 个字段 · ${(d.sources ?? []).length} 个来源 · ${nodeCount} 个节点（人工 ${humanCount}）· ${viewEntries.length} 个卡片模板`,
        },
        {
          key: 'dedup',
          label: (
            <>
              去重键
              <Help text="同一去重键在任务完结前只创建一个任务；来源的自动关闭条件也按它找到要关闭的任务。" />
            </>
          ),
          span: screens.md ? 2 : 1,
          children: d.dedup_key ? <Tmpl text={d.dedup_key} /> : <Typography.Text type="secondary">未设置（不去重）</Typography.Text>,
        },
        {
          key: 'view',
          label: (
            <>
              默认卡片模板
              <Help text="通知与人工节点没有配置卡片模板、上游也没有人工节点时使用。" />
            </>
          ),
          children: <ViewRef binding={d.view} />,
        },
        {
          key: 'assignment',
          label: (
            <>
              默认处理人
              <Help text="任务创建时按它分配处理人；节点没有配置处理人、上游也没有人工节点时同样使用它。" />
            </>
          ),
          children: <AssignmentView binding={d.assignment} />,
        },
      ]}
    />
  );

  const required = new Set(d.schema?.required ?? []);
  const fieldRows = fieldOrder.map((key) => ({ key, ...props_[key] }));
  const fields = (
    <Table<SchemaProp & { key: string }>
      size="small"
      pagination={false}
      rowKey="key"
      dataSource={fieldRows}
      scroll={{ x: 'max-content' }}
      columns={[
        { title: '字段', key: 'name', render: (_, f) => <FieldName name={f.key} title={f.title} /> },
        {
          title: '类型',
          key: 'type',
          render: (_, f) => (
            <Space size={4}>
              {typeLabel[f.type ?? ''] ?? f.type ?? '—'}
              {f['x-format'] && <Tag style={{ marginInlineEnd: 0 }}>{formatLabel[f['x-format']] ?? f['x-format']}</Tag>}
            </Space>
          ),
        },
        { title: '必填', key: 'required', align: 'center', render: (_, f) => (required.has(f.key) ? '是' : <Typography.Text type="secondary">—</Typography.Text>) },
        {
          title: (
            <>
              脱敏
              <Help text="发往 Lark、LLM 与日志时按此处理。" />
            </>
          ),
          key: 'pii',
          render: (_, f) => (f['x-pii'] ? <Tag color="orange">{piiLabel[f['x-pii']] ?? f['x-pii']}</Tag> : <Typography.Text type="secondary">—</Typography.Text>),
        },
        {
          title: '取值',
          key: 'enum',
          render: (_, f) =>
            f.enum ? (
              <Space size={[4, 4]} wrap style={{ maxWidth: 420 }}>
                {f.enum.map((v) => (
                  <Tooltip key={String(v)} title={String(v)}>
                    <Tag style={{ marginInlineEnd: 0 }}>{f['x-enum-labels']?.[String(v)] ?? String(v)}</Tag>
                  </Tooltip>
                ))}
              </Space>
            ) : (
              <Typography.Text type="secondary">{f.description ?? '—'}</Typography.Text>
            ),
        },
      ]}
    />
  );

  const source = (s: Source) => {
    const cron = s.schedule ? cronText(s.schedule) : '';
    const items = [
      s.schedule && {
        key: 'schedule',
        label: '调度',
        children: (
          <Space size={6} wrap>
            {cron && <span>{cron}</span>}
            <Typography.Text type="secondary" code>
              {s.schedule}
            </Typography.Text>
            <Typography.Text type="secondary">{s.timezone ?? 'Asia/Shanghai'}</Typography.Text>
          </Space>
        ),
      },
      s.event_type && { key: 'event', label: '事件类型', children: <Typography.Text code>{s.event_type}</Typography.Text> },
      s.skill && {
        key: 'skill',
        label: '扫描 skill',
        children: (
          <Space size={6} wrap>
            {skills[s.skill] && <span>{skills[s.skill]}</span>}
            <Typography.Text type="secondary" code>
              {s.skill}
            </Typography.Text>
          </Space>
        ),
      },
      s.input && Object.keys(s.input).length > 0 && { key: 'input', label: '参数', children: <ReadonlyObject value={s.input} /> },
      {
        key: 'filter',
        label: (
          <>
            创建条件
            <Help text="为真时创建任务（同一去重键已有未完结任务则跳过），为假的记录丢弃。" />
          </>
        ),
        children: s.filter ? <Expr>{s.filter}</Expr> : <Typography.Text type="secondary">全部记录</Typography.Text>,
      },
      s.close_when && {
        key: 'close',
        label: (
          <>
            自动关闭条件
            <Help text="为真时按去重键找到未完结的任务，取消流程，任务以 auto_closed 完结。先于创建条件判断。" />
          </>
        ),
        children: <Expr>{s.close_when}</Expr>,
      },
    ].filter(Boolean) as { key: string; label: ReactNode; children: ReactNode }[];
    const rank = (k: string) => (fieldOrder.indexOf(k) + 1 || fieldOrder.length + 1);
    const mapping = Object.entries(s.mapping ?? {}).sort(([a], [b]) => rank(a) - rank(b) || a.localeCompare(b));
    return (
      <Card
        key={s.id}
        type="inner"
        size="small"
        title={
          <Space>
            <Typography.Text code>{s.id}</Typography.Text>
            <Tag color="cyan">{sourceKind[s.kind] ?? s.kind}</Tag>
          </Space>
        }
        style={{ marginBottom: 12 }}
      >
        <Descriptions bordered size="small" column={1} labelStyle={labelStyle} items={items} />
        {mapping.length > 0 && (
          <>
            <Typography.Text strong style={{ display: 'block', margin: '16px 0 8px' }}>
              字段映射
              <Help text="每条记录（record）按表达式映射成任务字段，结果按字段定义校验。" />
            </Typography.Text>
            <Table
              size="small"
              pagination={false}
              rowKey={([k]) => k}
              dataSource={mapping}
              columns={[
                { title: '任务字段', key: 'field', width: '40%', render: (_, [k]) => <FieldName name={k} title={titles[k]} /> },
                { title: '取值', key: 'expr', render: (_, [, v]) => <Expr>{v}</Expr> },
              ]}
            />
          </>
        )}
      </Card>
    );
  };

  const selectedNode: WfNode | undefined = selected ? nodeById[selected] : undefined;
  const panelHeight = 560;
  const workflow = (
    <Row gutter={[16, 16]}>
      <Col xs={24} lg={15} xl={16}>
        <div style={{ height: panelHeight, border: '1px solid #f0f0f0', borderRadius: 6 }}>
          {/* 等节点目录返回再画：端口取决于目录，React Flow 只在节点尺寸变化时重新测量端口，先画会丢连线。 */}
          {catalog ? (
            <DefinitionCanvas
              workflow={wf}
              positions={d.ui?.canvas?.positions}
              catalog={catalog}
              selected={selected}
              focus={focus}
              onSelect={setSelected}
            />
          ) : (
            <Spin style={{ display: 'block', paddingTop: 120 }} />
          )}
        </div>
      </Col>
      <Col xs={24} lg={9} xl={8}>
        <div style={{ height: screens.lg ? panelHeight : undefined, overflow: 'auto' }}>
          {selectedNode ? (
            <NodeDetail node={selectedNode} workflow={wf} info={info[selectedNode.type]} binding={bindings[selectedNode.id]} onBack={() => setSelected(undefined)} />
          ) : (
            <>
              <Typography.Text type="secondary">点击画布或下方列表中的节点查看配置。</Typography.Text>
              <NodeList workflow={wf} info={info} onSelect={nav.node} />
            </>
          )}
        </div>
      </Col>
    </Row>
  );

  const delivery =
    bound.length === 0 ? (
      <Empty description="流程中没有通知或人工节点" />
    ) : (
      <Table<WfNode>
        size="small"
        pagination={false}
        rowKey="id"
        dataSource={bound}
        scroll={{ x: 'max-content' }}
        columns={[
          {
            title: '节点',
            key: 'node',
            width: 160,
            render: (_, n) => (
              <Space direction="vertical" size={2}>
                <NodeRef id={n.id} />
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  {info[n.type]?.display_name ?? n.type}
                </Typography.Text>
              </Space>
            ),
          },
          {
            title: '处理人 / 接收人',
            key: 'assignment',
            render: (_, n) => (
              <Space direction="vertical" size={2} style={{ minWidth: 260 }}>
                <AssignmentView binding={bindings[n.id].assignment} />
                <BindingFrom from={bindings[n.id].assignment_from} />
              </Space>
            ),
          },
          {
            title: '卡片模板',
            key: 'view',
            render: (_, n) => (
              <Space direction="vertical" size={2}>
                <ViewRef binding={bindings[n.id].view} />
                <BindingFrom from={bindings[n.id].view_from} />
              </Space>
            ),
          },
          {
            title: '处理人可选',
            key: 'options',
            render: (_, n) => {
              if (n.type !== 'human_choice') return <Typography.Text type="secondary">只通知</Typography.Text>;
              const c = (n.config ?? {}) as { options?: { key: string; label: string; style?: string; snooze?: string }[]; timeout?: string };
              return (
                <Space direction="vertical" size={4}>
                  <Space size={[4, 4]} wrap>
                    {(c.options ?? []).map((o) => (
                      <Tag key={o.key} color={o.style === 'primary' ? 'blue' : o.style === 'danger' ? 'red' : undefined} style={{ marginInlineEnd: 0 }}>
                        {o.label}
                        {o.snooze && `（推迟 ${durationText(o.snooze)}）`}
                      </Tag>
                    ))}
                  </Space>
                  {c.timeout && <Typography.Text type="secondary" style={{ fontSize: 12 }}>{durationText(c.timeout)}未选择则超时</Typography.Text>}
                </Space>
              );
            },
          },
        ]}
      />
    );

  const defaultViews = viewNames(d.view);
  const views =
    viewEntries.length === 0 ? (
      <Empty description="没有卡片模板" />
    ) : (
      <Row gutter={[16, 24]}>
        {viewEntries.map(([name, v]) => (
          <Col key={name} xs={24} md={12} xl={8}>
            <ViewPreview
              name={name}
              view={v}
              isDefault={defaultViews.includes(name)}
              usedBy={bound.filter((n) => viewNames(bindings[n.id].view).includes(name)).map((n) => n.id)}
              highlight={flash === name}
            />
          </Col>
        ))}
      </Row>
    );

  return (
    <FieldTitles.Provider value={titles}>
      <DefinitionNav.Provider value={nav}>
        {toolbar}
        <Row gutter={24} wrap={false}>
          {screens.lg && (
            <Col flex="132px">
              <Anchor offsetTop={16} items={sections.map((s) => ({ key: s.key, href: `#def-${s.key}`, title: s.title }))} onClick={(e) => e.preventDefault()} />
            </Col>
          )}
          <Col flex="auto" style={{ minWidth: 0 }}>
            <Section id="overview" title="概览">
              {overview}
            </Section>
            <Section id="fields" title={`字段（${fieldRows.length}）`}>
              {fields}
            </Section>
            <Section id="sources" title="来源">
              {(d.sources ?? []).length === 0 ? <Empty description="没有来源：任务只能由接口创建" /> : d.sources!.map(source)}
            </Section>
            <Section id="workflow" title="流程" extra={<Typography.Text type="secondary">拖动平移，右下角缩放</Typography.Text>}>
              {workflow}
            </Section>
            <Section id="delivery" title="处理人与触达">
              {delivery}
            </Section>
            <Section id="views" title="卡片模板">
              {views}
            </Section>
          </Col>
        </Row>
      </DefinitionNav.Provider>
    </FieldTitles.Provider>
  );
}
