// 流程画布右侧的面板：没有选中节点时列出全部节点（按 workflow 顺序，点击选中）；
// 选中时显示节点的类型说明、前后连线、实际使用的卡片模板与处理人（含继承来源），以及其余配置。
import { ArrowLeftOutlined } from '@ant-design/icons';
import { Button, Descriptions, List, Space, Tag, Typography } from 'antd';
import type { NodeTypeInfo } from '../../api/types';
import { summaryOf, type WfNode, type Workflow } from '../canvas/model';
import type { Binding } from './model';
import { ReadonlyObject, ReadonlyValue } from './ReadonlyValue';
import { AssignmentView, BindingFrom, NodeRef, ViewRef } from './refs';

/** 由 bindings 展示、不再在配置里重复的字段。 */
const boundKeys = new Set(['view', 'assignment', 'to']);

export function NodeList(props: { workflow: Workflow; info: Record<string, NodeTypeInfo>; onSelect: (id: string) => void }) {
  return (
    <List
      size="small"
      dataSource={props.workflow.nodes}
      renderItem={(n) => (
        <List.Item style={{ cursor: 'pointer', paddingInline: 8 }} onClick={() => props.onSelect(n.id)}>
          <List.Item.Meta
            title={
              <Space size={6}>
                <span>{n.name || n.id}</span>
                <Typography.Text type="secondary" style={{ fontSize: 12, fontWeight: 'normal' }}>
                  {props.info[n.type]?.display_name ?? n.type}
                </Typography.Text>
              </Space>
            }
            description={summaryOf(n) || undefined}
          />
        </List.Item>
      )}
    />
  );
}

export function NodeDetail(props: {
  node: WfNode;
  workflow: Workflow;
  info?: NodeTypeInfo;
  binding?: Binding;
  onBack: () => void;
}) {
  const { node: n, workflow: wf, binding: b } = props;
  const incoming = wf.edges.filter((e) => e.to === n.id);
  const outgoing = wf.edges.filter((e) => e.from === n.id);
  const config = Object.fromEntries(Object.entries(n.config ?? {}).filter(([k]) => !(b && boundKeys.has(k))));
  const toLabel = n.type === 'lark_card' ? '接收人' : '处理人';

  const items = [
    { key: 'type', label: '类型', children: <Space size={6}>{props.info?.display_name ?? n.type}<Typography.Text type="secondary" code>{n.type}</Typography.Text></Space> },
    { key: 'id', label: '节点 id', children: <Typography.Text copyable code>{n.id}</Typography.Text> },
    incoming.length > 0 && {
      key: 'in',
      label: '上一步',
      children: (
        <Space size={[4, 4]} wrap>
          {incoming.map((e) => (
            <span key={e.id}>
              <NodeRef id={e.from} />
              {wf.edges.filter((x) => x.from === e.from).length > 1 && <Typography.Text type="secondary" style={{ fontSize: 12 }}>（{e.port}）</Typography.Text>}
            </span>
          ))}
        </Space>
      ),
    },
    outgoing.length > 0 && {
      key: 'out',
      label: '下一步',
      children: (
        <Space direction="vertical" size={4}>
          {outgoing.map((e) => (
            <span key={e.id}>
              {(outgoing.length > 1 || e.port !== 'next') && (
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  {e.port} →{' '}
                </Typography.Text>
              )}
              <NodeRef id={e.to} />
              {e.kind === 'rework' && <Tag color="orange">退回</Tag>}
            </span>
          ))}
        </Space>
      ),
    },
    b && {
      key: 'view',
      label: '卡片模板',
      children: (
        <Space direction="vertical" size={2}>
          <ViewRef binding={b.view} />
          <BindingFrom from={b.view_from} />
        </Space>
      ),
    },
    b && {
      key: 'assignment',
      label: toLabel,
      children: (
        <Space direction="vertical" size={2} style={{ width: '100%' }}>
          <AssignmentView binding={b.assignment} />
          <BindingFrom from={b.assignment_from} />
        </Space>
      ),
    },
  ].filter(Boolean) as { key: string; label: string; children: React.ReactNode }[];

  return (
    <div>
      <Space style={{ marginBottom: 8 }}>
        <Button size="small" type="text" icon={<ArrowLeftOutlined />} onClick={props.onBack} aria-label="返回节点列表" />
        <Typography.Text strong>{n.name || n.id}</Typography.Text>
      </Space>
      {props.info?.description && (
        <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
          {props.info.description}
        </Typography.Paragraph>
      )}
      <Descriptions bordered size="small" column={1} items={items} labelStyle={{ width: 1, whiteSpace: 'nowrap', color: 'rgba(0, 0, 0, 0.55)', verticalAlign: 'top' }} />
      {Object.keys(config).length > 0 && (
        <>
          <Typography.Title level={5} style={{ marginTop: 16, fontSize: 14 }}>
            配置
          </Typography.Title>
          <ReadonlyObject value={config} />
        </>
      )}
      {n.retry !== undefined && (
        <>
          <Typography.Title level={5} style={{ marginTop: 16, fontSize: 14 }}>
            重试
          </Typography.Title>
          <ReadonlyValue value={n.retry} name="retry" />
        </>
      )}
      {Array.isArray(n.side_effects) && n.side_effects.length > 0 && (
        <>
          <Typography.Title level={5} style={{ marginTop: 16, fontSize: 14 }}>
            旁路动作
          </Typography.Title>
          <ReadonlyValue value={n.side_effects} name="side_effects" />
        </>
      )}
    </div>
  );
}
