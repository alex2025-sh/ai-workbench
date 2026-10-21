// 第 3 步：人工节点链。画布显示 start → 节点 → end，连线由动作生成（escalate 连下一个节点，close / no_response 连 end），
// 不能手画；点节点在下方编辑：名称、处理人（业务关系 + 兜底人，或一批人中随机）、可用的动作、业务链接、推迟时长、是否发 Lark。
// 节点 id 由前端自动生成且不展示：任务行记录所在节点的 id，改名、调整顺序都不影响在途任务。
import { ArrowLeftOutlined, ArrowRightOutlined, DeleteOutlined } from '@ant-design/icons';
import { Button, Card, Checkbox, Col, Descriptions, Empty, Form, Input, Popconfirm, Radio, Row, Select, Space, Switch, Table, Tag, Tooltip, Typography } from 'antd';
import { useEffect, useState } from 'react';
import { catalogApi } from '../../api/services';
import type { ActionKey, Issue, TaskTypeBody, TTAssignee, TTNode } from '../../api/types';
import { ChainCanvas } from '../ChainCanvas';
import { actionHelp, actionLabel, allActions, assigneeText, issuesAt, IssueList } from './shared';

const snoozeChoices = ['30m', '1h', '2h', '4h', '1d', '3d', '7d'];
const snoozeText = (d: string) => d.replace('m', ' 分钟').replace('h', ' 小时').replace('d', ' 天');

/** 节点 id：n_ + 6 位随机 base36，与已有节点不重复。 */
export function newNodeId(taken: string[]): string {
  for (;;) {
    const id = `n_${Math.random().toString(36).slice(2, 8).padEnd(6, '0')}`;
    if (!taken.includes(id)) return id;
  }
}

const emptyAssignee = (kind: TTAssignee['kind'], relations: string[]): TTAssignee =>
  kind === 'relation' ? { kind, relation: relations[0] ?? '', field: '', fallback: '' } : { kind, users: [], strategy: 'random' };

function newNode(taken: string[]): TTNode {
  return { id: newNodeId(taken), name: '新节点', assignee: emptyAssignee('pool', []), actions: ['close'], lark: true };
}

const emailLike = /^[^\s@]+@[^\s@]+$/;

export function NodesStep(props: { body: TaskTypeBody; onChange?: (b: TaskTypeBody) => void; issues: Issue[]; selected?: string; onSelect?: (id?: string) => void }) {
  const { body, onChange } = props;
  const ro = !onChange;
  const [relations, setRelations] = useState<string[]>([]);
  const [sel, setSel] = useState<string | undefined>(props.selected ?? (ro ? undefined : body.nodes[0]?.id));
  useEffect(() => setSel((s) => props.selected ?? s), [props.selected]);
  useEffect(() => {
    if (ro) return;
    catalogApi.relations().then((r) => setRelations(r.names), () => undefined);
  }, [ro]);

  const nodes = body.nodes;
  const idx = nodes.findIndex((n) => n.id === sel);
  const node = idx >= 0 ? nodes[idx] : undefined;
  const setNodes = (ns: TTNode[]) => onChange?.({ ...body, nodes: ns });
  const update = (patch: Partial<TTNode>) => setNodes(nodes.map((n, i) => (i === idx ? { ...n, ...patch } : n)));
  const select = (id?: string) => {
    setSel(id);
    props.onSelect?.(id);
  };
  const move = (d: number) => {
    const ns = [...nodes];
    [ns[idx], ns[idx + d]] = [ns[idx + d], ns[idx]];
    // 移到最后的节点不能再升级
    setNodes(ns.map((n, i) => (i === ns.length - 1 ? { ...n, actions: n.actions.filter((a) => a !== 'escalate') } : n)));
  };
  const toggleAction = (a: ActionKey, on: boolean) => {
    const actions = on ? allActions.filter((x) => x === a || node!.actions.includes(x)) : node!.actions.filter((x) => x !== a);
    const patch: Partial<TTNode> = { actions };
    if (a === 'link') patch.link = on ? node!.link ?? { label: '打开业务系统', url: '' } : undefined;
    if (a === 'snooze' && !on) patch.snooze = undefined;
    update(patch);
  };

  const canvas = (
    <ChainCanvas
      height={240}
      selected={sel}
      onSelect={select}
      onAdd={
        ro
          ? undefined
          : () => {
              const n = newNode(nodes.map((x) => x.id));
              // 原来的最后一个节点现在可以升级到新节点
              setNodes([...nodes, n]);
              select(n.id);
            }
      }
      nodes={nodes.map((n, i) => ({
        id: n.id,
        name: n.name,
        assignee: assigneeText(body, n.assignee),
        actions: n.actions,
        lark: n.lark,
        issue: issuesAt(props.issues, `nodes.${i}`).length > 0,
      }))}
    />
  );

  if (ro) {
    return (
      <Card size="small" title={`节点（${nodes.length}）`}>
        {canvas}
        <Table<TTNode>
          style={{ marginTop: 12 }}
          size="small"
          pagination={false}
          rowKey="id"
          dataSource={nodes}
          scroll={{ x: 'max-content' }}
          columns={[
            { title: '节点', render: (_, n) => <b>{n.name}</b> },
            { title: '处理人', render: (_, n) => assigneeText(body, n.assignee) },
            {
              title: '动作',
              render: (_, n) => (
                <Space size={[4, 4]} wrap>
                  {n.actions.map((a) => (
                    <Tooltip key={a} title={a === 'link' ? n.link?.url : a === 'snooze' ? (n.snooze?.length ? n.snooze : ['1h', '4h', '1d']).map(snoozeText).join('、') : actionHelp[a]}>
                      <Tag>{a === 'link' ? n.link?.label : actionLabel[a]}</Tag>
                    </Tooltip>
                  ))}
                </Space>
              ),
            },
            { title: 'Lark 通知', render: (_, n) => (n.lark ? '是' : '否') },
          ]}
        />
      </Card>
    );
  }

  const nodeIssues = idx >= 0 ? issuesAt(props.issues, `nodes.${idx}`) : [];
  const fieldOptions = body.fields.map((f) => ({ value: f.name, label: f.label ? `${f.label}（${f.name}）` : f.name }));
  const a = node?.assignee;
  return (
    <Card size="small" title={`节点（${nodes.length}）`}>
      <IssueList issues={issuesAt(props.issues, 'nodes').filter((i) => !/^nodes\.\d/.test(i.path))} />
      <Typography.Paragraph type="secondary">
        任务创建时进入第一个节点；「升级」交给下一个节点，「已处理 / 无需处理」结束任务。点击节点在下方编辑，右上角添加节点。
      </Typography.Paragraph>
      {canvas}
      {!node ? (
        <Empty style={{ marginTop: 24 }} description="点击画布中的节点进行编辑" />
      ) : (
        <Row gutter={24} style={{ marginTop: 16 }}>
          <Col xs={24} lg={14}>
            <IssueList issues={nodeIssues} />
            <Form layout="vertical">
              <Form.Item label="节点名称" style={{ marginBottom: 12, maxWidth: 320 }}>
                <Input value={node.name} onChange={(e) => update({ name: e.target.value })} placeholder="如：RM 跟进" />
              </Form.Item>
              <Form.Item label="处理人" style={{ marginBottom: 12 }}>
                <Radio.Group
                  value={a!.kind}
                  optionType="button"
                  onChange={(e) => update({ assignee: emptyAssignee(e.target.value, relations) })}
                  options={[
                    { value: 'relation', label: '按业务关系' },
                    { value: 'pool', label: '一批人中随机' },
                  ]}
                />
              </Form.Item>
              {a!.kind === 'relation' ? (
                <Space wrap align="start">
                  <Form.Item label="业务关系" tooltip="如 vip_rm：VIP 用户 → 专属 RM" style={{ marginBottom: 12, width: 160 }}>
                    <Select value={a!.relation || undefined} onChange={(v) => update({ assignee: { ...a!, relation: v } })} options={relations.map((r) => ({ value: r, label: r }))} />
                  </Form.Item>
                  <Form.Item label="按哪个字段查" style={{ marginBottom: 12, width: 200 }}>
                    <Select value={a!.field || undefined} onChange={(v) => update({ assignee: { ...a!, field: v } })} options={fieldOptions} />
                  </Form.Item>
                  <Form.Item
                    label="查不到时交给"
                    tooltip="兜底人：业务关系里查不到处理人时，任务交给这个人"
                    validateStatus={a!.fallback && !emailLike.test(a!.fallback) ? 'warning' : undefined}
                    help={a!.fallback && !emailLike.test(a!.fallback) ? '请填写邮箱' : undefined}
                    style={{ marginBottom: 12, width: 260 }}
                  >
                    <Input value={a!.fallback} onChange={(e) => update({ assignee: { ...a!, fallback: e.target.value.trim() } })} placeholder="lead@example.com" />
                  </Form.Item>
                </Space>
              ) : (
                <Form.Item
                  label="候选人"
                  tooltip="进入节点时从中随机选一人处理；输入邮箱后回车，可填多人"
                  style={{ marginBottom: 12, maxWidth: 520 }}
                >
                  <Select
                    mode="tags"
                    open={false}
                    tokenSeparators={[',', '，', ' ', ';']}
                    value={a!.users ?? []}
                    onChange={(v: string[]) => update({ assignee: { ...a!, users: [...new Set(v.map((x) => x.trim()).filter(Boolean))], strategy: 'random' } })}
                    placeholder="reviewer.dave@example.com, reviewer.erin@example.com"
                    tagRender={({ label, closable, onClose, value }) => (
                      <Tag closable={closable} onClose={onClose} color={emailLike.test(String(value)) ? undefined : 'warning'} style={{ marginInlineEnd: 4 }}>
                        {label}
                      </Tag>
                    )}
                  />
                </Form.Item>
              )}
              <Form.Item label="处理人可以" style={{ marginBottom: 12 }}>
                <Space direction="vertical" size={4}>
                  {allActions.map((key) => {
                    const disabled = key === 'escalate' && idx === nodes.length - 1;
                    return (
                      <Checkbox key={key} checked={node.actions.includes(key)} disabled={disabled} onChange={(e) => toggleAction(key, e.target.checked)}>
                        {actionLabel[key]}
                        <Typography.Text type="secondary" style={{ marginInlineStart: 8, fontSize: 12 }}>
                          {disabled ? '最后一个节点不能升级' : key === 'escalate' ? `交给「${nodes[idx + 1]?.name}」` : actionHelp[key]}
                        </Typography.Text>
                      </Checkbox>
                    );
                  })}
                </Space>
              </Form.Item>
              {node.actions.includes('link') && (
                <Space wrap align="start">
                  <Form.Item label="链接按钮文字" style={{ marginBottom: 12, width: 160 }}>
                    <Input value={node.link?.label} onChange={(e) => update({ link: { url: node.link?.url ?? '', label: e.target.value } })} />
                  </Form.Item>
                  <Form.Item label="链接地址" tooltip="可用字段占位，如 {appeal_id}" style={{ marginBottom: 12, width: 380 }}>
                    <Input value={node.link?.url} onChange={(e) => update({ link: { label: node.link?.label ?? '', url: e.target.value } })} placeholder="https://.../{appeal_id}" />
                  </Form.Item>
                </Space>
              )}
              {node.actions.includes('snooze') && (
                <Form.Item label="推迟时长" style={{ marginBottom: 12 }}>
                  <Checkbox.Group
                    value={node.snooze?.length ? node.snooze : ['1h', '4h', '1d']}
                    onChange={(v) => update({ snooze: (v as string[]).length ? (snoozeChoices.filter((c) => (v as string[]).includes(c))) : undefined })}
                    options={snoozeChoices.map((c) => ({ value: c, label: snoozeText(c) }))}
                  />
                </Form.Item>
              )}
              <Form.Item label="Lark 通知" tooltip="进入节点、转派、推迟到期时发给处理人" style={{ marginBottom: 12 }}>
                <Switch checked={!!node.lark} onChange={(v) => update({ lark: v })} />
              </Form.Item>
            </Form>
          </Col>
          <Col xs={24} lg={10}>
            <Descriptions size="small" column={1} bordered title="节点" items={[
              { key: 'p', label: '位置', children: `第 ${idx + 1} / ${nodes.length} 个` },
              { key: 'n', label: '下一个', children: nodes[idx + 1]?.name ?? '— (最后一个)' },
            ]} />
            <Space style={{ marginTop: 12 }}>
              <Button icon={<ArrowLeftOutlined />} disabled={idx === 0} onClick={() => move(-1)}>前移</Button>
              <Button icon={<ArrowRightOutlined />} disabled={idx === nodes.length - 1} onClick={() => move(1)}>后移</Button>
              <Popconfirm title="删除这个节点？" okText="删除" cancelText="取消" disabled={nodes.length === 1}
                onConfirm={() => {
                  const ns = nodes.filter((_, i) => i !== idx);
                  setNodes(ns.map((n, i) => (i === ns.length - 1 ? { ...n, actions: n.actions.filter((x) => x !== 'escalate') } : n)));
                  select(ns[Math.max(0, idx - 1)]?.id);
                }}>
                <Button danger icon={<DeleteOutlined />} disabled={nodes.length === 1}>删除</Button>
              </Popconfirm>
            </Space>
          </Col>
        </Row>
      )}
    </Card>
  );
}
