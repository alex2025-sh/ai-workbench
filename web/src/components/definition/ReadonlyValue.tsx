// 只读展示任意配置值（节点配置、skill 参数、重试、旁路动作）：
// 对象 → KV 表格（中文标签取 schemaform/labels.ts，与编辑页的表单一致）；
// 由扁平对象组成的数组 → 表格；基本类型数组 → 标签；
// 字符串按字段名区分：表达式等宽显示、模板中的引用显示为字段标记、时长转成中文、枚举取中文名。
import { Descriptions, Space, Table, Tag, Typography } from 'antd';
import type { ReactNode } from 'react';
import { enumLabels, labelOf } from '../schemaform/labels';
import { durationText, Expr, Tmpl } from './text';

/** 值为表达式的字段名（不是模板）。 */
const exprKeys = new Set(['when', 'filter', 'close_when', 'subject', 'expr', 'anchor']);
/** 值为时长的字段名。 */
const durationKeys = new Set(['after', 'timeout', 'snooze', 'every', 'initial_interval', 'max_interval']);

function isPrimitive(v: unknown): boolean {
  return v === null || ['string', 'number', 'boolean'].includes(typeof v);
}

function isFlat(v: unknown): v is Record<string, unknown> {
  return (
    !!v &&
    typeof v === 'object' &&
    !Array.isArray(v) &&
    Object.values(v).every((x) => isPrimitive(x) || (Array.isArray(x) && x.every(isPrimitive)))
  );
}

/** 单个值。name 是它所在的字段名，决定展示方式；parent 用于同名字段的标签覆盖。 */
export function ReadonlyValue({ value, name = '', parent }: { value: unknown; name?: string; parent?: string }): ReactNode {
  if (value === undefined || value === null || value === '') return <Typography.Text type="secondary">—</Typography.Text>;
  if (typeof value === 'boolean') return value ? '是' : '否';
  if (typeof value === 'number') return String(value);
  if (typeof value === 'string') {
    if (exprKeys.has(name)) return <Expr>{value}</Expr>;
    if (durationKeys.has(name)) return durationText(value);
    const en = enumLabels[name]?.[value];
    if (en) return en;
    if (value.includes('{{')) return <Tmpl text={value} />;
    return value;
  }
  if (Array.isArray(value)) {
    if (value.length === 0) return <Typography.Text type="secondary">—</Typography.Text>;
    if (value.every(isPrimitive)) {
      if (exprKeys.has(name) || name === 'exclude') {
        return (
          <Space direction="vertical" size={4}>
            {value.map((v, i) => (
              <Expr key={i}>{String(v)}</Expr>
            ))}
          </Space>
        );
      }
      return (
        <Space size={[4, 4]} wrap>
          {value.map((v, i) => (
            <Tag key={i} style={{ marginInlineEnd: 0 }}>
              {enumLabels[name]?.[String(v)] ?? String(v)}
            </Tag>
          ))}
        </Space>
      );
    }
    if (value.every(isFlat)) {
      const keys = [...new Set(value.flatMap((v) => Object.keys(v as object)))];
      return (
        <Table
          size="small"
          bordered
          pagination={false}
          rowKey={(_, i) => String(i)}
          dataSource={value as Record<string, unknown>[]}
          columns={keys.map((k) => ({
            key: k,
            title: labelOf(k, name),
            dataIndex: k,
            render: (v: unknown) => <ReadonlyValue value={v} name={k} parent={name} />,
          }))}
          scroll={{ x: 'max-content' }}
        />
      );
    }
    return (
      <Space direction="vertical" style={{ width: '100%' }}>
        {value.map((v, i) => (
          <ReadonlyValue key={i} value={v} name={name} parent={parent} />
        ))}
      </Space>
    );
  }
  return <ReadonlyObject value={value as Record<string, unknown>} parent={name} />;
}

/** 对象：一行一个字段的 KV 表格。 */
export function ReadonlyObject({ value, parent }: { value: Record<string, unknown>; parent?: string }) {
  const entries = Object.entries(value);
  if (entries.length === 0) return <Typography.Text type="secondary">—</Typography.Text>;
  return (
    <Descriptions
      bordered
      size="small"
      column={1}
      labelStyle={{ width: 1, whiteSpace: 'nowrap', color: 'rgba(0, 0, 0, 0.55)', verticalAlign: 'top' }}
      items={entries.map(([k, v]) => ({ key: k, label: labelOf(k, parent), children: <ReadonlyValue value={v} name={k} parent={parent} /> }))}
    />
  );
}
