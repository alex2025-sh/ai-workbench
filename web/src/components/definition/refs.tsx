// 定义页内的交叉引用：卡片模板、节点可点击跳转；分配策略写成「候选人 → 过滤 → 选择 → 兜底」的步骤。
import { Space, Tag, Tooltip, Typography } from 'antd';
import { createContext, useContext } from 'react';
import { enumLabels } from '../schemaform/labels';
import type { AssignmentBinding, Resolver, ViewBinding } from './model';
import { Expr, Token } from './text';

/** 定义页内跳转：滚动到卡片模板、在画布中选中节点。 */
export const DefinitionNav = createContext<{ view: (name: string) => void; node: (id: string) => void; nodeName: (id: string) => string }>({
  view: () => undefined,
  node: () => undefined,
  nodeName: (id) => id,
});

const linkStyle = { cursor: 'pointer', marginInlineEnd: 0 } as const;

/** 卡片模板名：本任务类型的模板可点击跳到预览；共享组件显示版本。 */
export function ViewName({ name }: { name: string }) {
  const nav = useContext(DefinitionNav);
  if (name.includes('@')) return <Tag color="purple" style={{ marginInlineEnd: 0 }}>共享组件 {name}</Tag>;
  return (
    <Tag color="blue" style={linkStyle} onClick={() => nav.view(name)}>
      {name}
    </Tag>
  );
}

export function ViewRef({ binding }: { binding: ViewBinding | null | undefined }) {
  if (!binding) return <Typography.Text type="secondary">—</Typography.Text>;
  if (typeof binding === 'string') return <ViewName name={binding} />;
  return (
    <Space direction="vertical" size={4}>
      <span>
        默认 <ViewName name={binding.default} />
      </span>
      {(binding.variants ?? []).map((v, i) => (
        <span key={i}>
          当 <Expr>{v.when}</Expr> 时 <ViewName name={v.view} />
        </span>
      ))}
    </Space>
  );
}

/** 节点 id：可点击，在流程画布中选中。 */
export function NodeRef({ id }: { id: string }) {
  const nav = useContext(DefinitionNav);
  const name = nav.nodeName(id);
  return (
    <Tooltip title={name !== id ? id : undefined}>
      <Tag style={linkStyle} onClick={() => nav.node(id)}>
        {name}
      </Tag>
    </Tooltip>
  );
}

/** 配置的来源：本节点、任务类型默认，或沿用上游人工节点。 */
export function BindingFrom({ from }: { from?: string }) {
  if (!from || from === 'self') return <Typography.Text type="secondary">本节点配置</Typography.Text>;
  if (from === 'task_type') return <Typography.Text type="secondary">任务类型默认</Typography.Text>;
  return (
    <Typography.Text type="secondary">
      沿用 <NodeRef id={from} />
    </Typography.Text>
  );
}

function resolverText(r: Resolver) {
  const label = enumLabels.type?.[r.type] ?? r.type;
  switch (r.type) {
    case 'relation':
      return (
        <>
          {label} <Typography.Text code>{String(r.relation)}</Typography.Text>，主体 <Token expr={String(r.subject)} />
        </>
      );
    case 'group':
      return (
        <>
          {label} <Typography.Text code>{String(r.group)}</Typography.Text>
        </>
      );
    case 'user':
      return (
        <>
          {label} {((r.users as string[]) ?? []).join('、')}
        </>
      );
    case 'agreement':
      return (
        <>
          {label} <Tag color="purple">{String(r.agreement)}</Tag>
        </>
      );
    case 'previous_assignee':
      return r.node ? (
        <>
          <NodeRef id={String(r.node)} /> 的处理人
        </>
      ) : (
        <>{label}</>
      );
    case 'expr':
      return (
        <>
          {label} <Expr>{String(r.expr)}</Expr>
        </>
      );
    default:
      return <>{label}</>;
  }
}

const rowLabel = { color: 'rgba(0, 0, 0, 0.55)', width: 40, flex: 'none' } as const;

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{ display: 'flex', gap: 8, alignItems: 'baseline' }}>
      <span style={rowLabel}>{label}</span>
      <div style={{ minWidth: 0 }}>{children}</div>
    </div>
  );
}

/** 分配策略：按执行顺序列出候选人来源、过滤、选择策略与兜底。 */
export function AssignmentView({ binding }: { binding: AssignmentBinding | null | undefined }) {
  if (!binding) return <Typography.Text type="secondary">—</Typography.Text>;
  if (typeof binding === 'string') return <Tag color="purple">共享组件 {binding}</Tag>;
  const a = binding;
  return (
    <Space direction="vertical" size={2} style={{ width: '100%' }}>
      <Row label="候选">
        <Space direction="vertical" size={2}>
          {a.resolvers.map((r, i) => (
            <span key={i}>
              {a.resolvers.length > 1 && <Typography.Text type="secondary">{i + 1}. </Typography.Text>}
              {resolverText(r)}
            </span>
          ))}
        </Space>
      </Row>
      {(a.filters ?? []).length > 0 && <Row label="过滤">{a.filters!.map((f) => enumLabels.filters?.[f] ?? f).join('、')}</Row>}
      {(a.exclude ?? []).length > 0 && (
        <Row label="排除">
          <Space size={4} wrap>
            {a.exclude!.map((e, i) => (
              <Expr key={i}>{e}</Expr>
            ))}
          </Space>
        </Row>
      )}
      {a.strategy && <Row label="选择">{enumLabels.strategy?.[a.strategy] ?? a.strategy}</Row>}
      {a.fallback && (
        <Row label="兜底">
          用户组 <Typography.Text code>{a.fallback.group}</Typography.Text>，{enumLabels.strategy?.[a.fallback.strategy ?? 'claim'] ?? a.fallback.strategy}
        </Row>
      )}
    </Space>
  );
}
