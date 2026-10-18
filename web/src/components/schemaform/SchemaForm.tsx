// 按 JSON Schema 生成节点配置表单。只覆盖 task_type.schema.json 实际用到的写法：
//   - $ref（按定义名选控件：duration、expression、template、view_binding、skill_name、assignment_binding……）；
//   - object / array / enum / string / integer / number / boolean；
//   - allOf + if/then 按某个字段（如处理人解析器的 type）区分的联合类型。
// 改动通过 onChange 返回新值；可选字段清空时从对象中删除，保持 JSON 干净。表单覆盖不到的写法在 JSON 页签编辑。
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons';
import { AutoComplete, Button, Card, Input, InputNumber, Select, Space, Switch, Tooltip, Typography } from 'antd';
import type { ReactNode } from 'react';
import { enumLabels, labelOf } from './labels';

// schema 是任意 JSON，按需取字段。
type Schema = Record<string, any>;

/** 表单上下文：根 schema（用于解析 $ref）与可选值来源。 */
export interface FormContext {
  root: Schema;
  views: string[]; // 当前任务类型的卡片模板名
  skills: string[];
}

interface FieldProps {
  schema: Schema;
  value: unknown;
  onChange: (v: unknown) => void;
  ctx: FormContext;
  name: string;
  parent?: string;
  required?: boolean;
}

/** 顺着 $ref 找到具体的 schema，返回它与最后一个定义名。 */
function resolve(schema: Schema, ctx: FormContext): { s: Schema; ref?: string } {
  let s = schema;
  let ref: string | undefined;
  for (let i = 0; i < 10 && s?.$ref; i++) {
    const name = String(s.$ref).replace('#/$defs/', '');
    ref = name;
    const { $ref: _ignored, ...rest } = s;
    s = { ...ctx.root.$defs?.[name], ...rest };
  }
  return { s, ref };
}

function isEmpty(v: unknown): boolean {
  return v === undefined || v === null || v === '' || (Array.isArray(v) && v.length === 0);
}

function patternError(s: Schema, v: unknown): string | undefined {
  if (typeof v !== 'string' || v === '' || !s.pattern) return undefined;
  try {
    return new RegExp(s.pattern).test(v) ? undefined : '格式不正确';
  } catch {
    return undefined;
  }
}

function Field(props: { label: string; required?: boolean; help?: string; error?: string; children: ReactNode }) {
  return (
    <div style={{ marginBottom: 12 }}>
      <div style={{ marginBottom: 4 }}>
        {props.required && <span style={{ color: '#ff4d4f', marginRight: 4 }}>*</span>}
        <Tooltip title={props.help}>
          <Typography.Text style={{ borderBottom: props.help ? '1px dashed #bbb' : undefined }}>{props.label}</Typography.Text>
        </Tooltip>
      </div>
      {props.children}
      {props.error && <Typography.Text type="danger" style={{ fontSize: 12 }}>{props.error}</Typography.Text>}
    </div>
  );
}

export function SchemaField(props: FieldProps): ReactNode {
  const { ctx, name, parent } = props;
  const { s, ref } = resolve(props.schema, ctx);
  const label = labelOf(name, parent);
  const help = s.description as string | undefined;
  const v = props.value;
  const set = props.onChange;

  // 按定义名选控件。
  switch (ref) {
    case 'duration':
      return (
        <Field label={label} required={props.required} help={help} error={patternError(s, v)}>
          <Input value={(v as string) ?? ''} placeholder="如 30m、48h、7d" onChange={(e) => set(e.target.value)} />
        </Field>
      );
    case 'expression':
      return (
        <Field label={label} required={props.required} help={help}>
          <Input
            value={(v as string) ?? ''}
            style={{ fontFamily: 'monospace' }}
            placeholder="表达式，如 payload.vip_level > 0、nodes.check.output.status"
            onChange={(e) => set(e.target.value)}
          />
        </Field>
      );
    case 'template':
      return (
        <Field label={label} required={props.required} help={help}>
          <Input value={(v as string) ?? ''} placeholder="可用 {{ 表达式 }} 插值" onChange={(e) => set(e.target.value)} />
        </Field>
      );
    case 'view_binding':
    case 'view_name':
      if (v && typeof v === 'object') {
        return (
          <Field label={label} help={help}>
            <Typography.Text type="secondary">配置了受众变体，请在 JSON 页签编辑</Typography.Text>
          </Field>
        );
      }
      return (
        <Field label={label} required={props.required} help={help}>
          <AutoComplete
            value={(v as string) ?? ''}
            allowClear
            placeholder="不填则沿用上一个人工节点或任务类型默认"
            options={ctx.views.map((x) => ({ value: x }))}
            onChange={(x) => set(x || undefined)}
            style={{ width: '100%' }}
          />
        </Field>
      );
    case 'skill_name':
      return (
        <Field label={label} required={props.required} help={help} error={patternError(s, v)}>
          <AutoComplete
            value={(v as string) ?? ''}
            options={ctx.skills.map((x) => ({ value: x }))}
            onChange={(x) => set(x)}
            style={{ width: '100%' }}
            placeholder="skill 名称"
          />
        </Field>
      );
    case 'assignment_binding':
      if (typeof v === 'string') {
        return (
          <Field label={label} help="引用共享分配策略（组件@版本）">
            <Input value={v} onChange={(e) => set(e.target.value || undefined)} />
          </Field>
        );
      }
      return (
        <Group label={label} help={help} optional={!props.required} present={v !== undefined} onAdd={() => set({ resolvers: [{ type: 'user', users: [] }] })} onRemove={() => set(undefined)}>
          {/* 外层已有分组卡片，内层只渲染字段（parent 为空时不再套卡片）。 */}
          <SchemaField {...props} schema={{ $ref: '#/$defs/assignment' }} parent={undefined} />
        </Group>
      );
    case 'template_map':
      return (
        <Field label={label} help={help}>
          <KeyValue value={(v as Record<string, unknown>) ?? {}} onChange={(m) => set(Object.keys(m).length ? m : undefined)} />
        </Field>
      );
  }

  if (s.enum) {
    const labels = enumLabels[name] ?? {};
    return (
      <Field label={label} required={props.required} help={help}>
        <Select
          value={v as string | undefined}
          allowClear={!props.required}
          placeholder={s.default !== undefined ? `不填为「${labels[s.default] ?? s.default}」` : undefined}
          options={(s.enum as string[]).map((x) => ({ value: x, label: labels[x] ?? x }))}
          onChange={(x) => set(x)}
          style={{ width: '100%' }}
        />
      </Field>
    );
  }

  switch (s.type) {
    case 'object':
      return <ObjectField {...props} schema={s} />;
    case 'array':
      return <ArrayField {...props} schema={s} />;
    case 'integer':
    case 'number':
      return (
        <Field label={label} required={props.required} help={help}>
          <InputNumber
            value={v as number | undefined}
            min={s.minimum}
            max={s.maximum}
            precision={s.type === 'integer' ? 0 : undefined}
            onChange={(x) => set(x ?? undefined)}
            style={{ width: '100%' }}
          />
        </Field>
      );
    case 'boolean':
      return (
        <Field label={label} help={help}>
          <Switch checked={Boolean(v)} onChange={(x) => set(x)} />
        </Field>
      );
    default:
      return (
        <Field label={label} required={props.required} help={help} error={patternError(s, v)}>
          <Input value={(v as string) ?? ''} onChange={(e) => set(e.target.value)} />
        </Field>
      );
  }
}

/** 对象：合并 allOf + if/then 中当前生效的分支（如解析器 type = group 时只有 group 字段）。 */
function ObjectField(props: FieldProps) {
  const { schema: s, ctx } = props;
  const value = (props.value && typeof props.value === 'object' ? props.value : {}) as Record<string, unknown>;
  // then 分支里的属性可以是 true（表示沿用基础定义），只渲染对象形式的。
  let properties: Record<string, Schema | boolean> = { ...(s.properties ?? {}) };
  let required: string[] = [...(s.required ?? [])];
  let discriminator: string | undefined;
  for (const branch of (s.allOf ?? []) as Schema[]) {
    const cond = branch.if?.properties as Record<string, Schema> | undefined;
    if (!cond) continue;
    const [k, c] = Object.entries(cond)[0] ?? [];
    if (!k) continue;
    discriminator = k;
    if (value[k] === c.const) {
      properties = { ...properties, ...(branch.then?.properties ?? {}) };
      required = [...required, ...(branch.then?.required ?? [])];
    }
  }
  const update = (k: string, v: unknown) => {
    const next = { ...value };
    if (isEmpty(v) && !required.includes(k)) delete next[k];
    else next[k] = v;
    // 切换联合类型时丢掉不再适用的字段。
    if (k === discriminator) {
      for (const key of Object.keys(next)) {
        if (key !== k && !(s.properties ?? {})[key]) delete next[key];
      }
    }
    props.onChange(next);
  };
  const body = Object.entries(properties)
    .filter((e): e is [string, Schema] => typeof e[1] === 'object')
    .map(([k, ps]) => (
      <SchemaField
        key={k}
        schema={ps}
        value={value[k]}
        onChange={(v) => update(k, v)}
        ctx={ctx}
        name={k}
        parent={props.name}
        required={required.includes(k)}
      />
    ));
  if (props.parent === undefined) return <>{body}</>; // 根对象不加边框
  return (
    <Group label={labelOf(props.name, props.parent)} help={s.description} optional={!props.required} present={props.value !== undefined} onAdd={() => props.onChange({})} onRemove={() => props.onChange(undefined)}>
      {body}
    </Group>
  );
}

function ArrayField(props: FieldProps) {
  const { ctx } = props;
  const { s: items } = resolve(props.schema.items ?? {}, ctx);
  const list = (Array.isArray(props.value) ? props.value : []) as unknown[];
  const label = labelOf(props.name, props.parent);
  // 字符串数组：可自由输入的标签；枚举数组：多选。
  if (items.type !== 'object') {
    const labels = enumLabels[props.name] ?? {};
    return (
      <Field label={label} required={props.required} help={props.schema.description}>
        <Select
          mode={items.enum ? 'multiple' : 'tags'}
          value={list as string[]}
          options={(items.enum ?? []).map((x: string) => ({ value: x, label: labels[x] ?? x }))}
          onChange={(x) => props.onChange(x)}
          style={{ width: '100%' }}
          placeholder={items.enum ? '选择' : '输入后回车'}
          tokenSeparators={[',']}
        />
      </Field>
    );
  }
  const set = (i: number, v: unknown) => props.onChange(list.map((x, j) => (j === i ? v : x)));
  const blank = () => Object.fromEntries(((items.required ?? []) as string[]).map((k) => [k, '']));
  return (
    <Field label={label} required={props.required} help={props.schema.description}>
      <Space direction="vertical" style={{ width: '100%' }}>
        {list.map((item, i) => (
          <Card
            key={i}
            size="small"
            styles={{ body: { paddingBottom: 0 } }}
            extra={
              <Button size="small" type="text" danger icon={<DeleteOutlined />} onClick={() => props.onChange(list.filter((_, j) => j !== i))} />
            }
            title={`#${i + 1}`}
          >
            <ObjectField schema={items} value={item} onChange={(v) => set(i, v)} ctx={ctx} name={props.name} />
          </Card>
        ))}
        <Button size="small" icon={<PlusOutlined />} onClick={() => props.onChange([...list, blank()])}>
          添加
        </Button>
      </Space>
    </Field>
  );
}

/** 可选的嵌套对象：未配置时显示「添加」，配置后可以移除。 */
function Group(props: { label: string; help?: string; optional: boolean; present: boolean; onAdd: () => void; onRemove: () => void; children: ReactNode }) {
  if (props.optional && !props.present) {
    return (
      <Field label={props.label} help={props.help}>
        <Button size="small" icon={<PlusOutlined />} onClick={props.onAdd}>
          配置{props.label}
        </Button>
      </Field>
    );
  }
  return (
    <Card
      size="small"
      title={<Tooltip title={props.help}>{props.label}</Tooltip>}
      extra={props.optional && <Button size="small" type="text" danger icon={<DeleteOutlined />} onClick={props.onRemove} />}
      style={{ marginBottom: 12 }}
      styles={{ body: { paddingBottom: 0 } }}
    >
      {props.children}
    </Card>
  );
}

/** 参数映射：键 → 模板字符串。 */
function KeyValue(props: { value: Record<string, unknown>; onChange: (m: Record<string, unknown>) => void }) {
  const entries = Object.entries(props.value);
  const rebuild = (list: [string, unknown][]) => props.onChange(Object.fromEntries(list.filter(([k]) => k !== '' || list.length === 1)));
  return (
    <Space direction="vertical" style={{ width: '100%' }}>
      {entries.map(([k, val], i) => (
        <Space.Compact key={i} style={{ width: '100%' }}>
          <Input style={{ width: '35%' }} value={k} placeholder="参数名" onChange={(e) => rebuild(entries.map((x, j) => (j === i ? [e.target.value, x[1]] : x)))} />
          <Input
            style={{ width: '58%' }}
            value={typeof val === 'string' ? val : JSON.stringify(val)}
            placeholder="值，可用 {{ 表达式 }}"
            onChange={(e) => rebuild(entries.map((x, j) => (j === i ? [x[0], e.target.value] : x)))}
          />
          <Button icon={<DeleteOutlined />} onClick={() => rebuild(entries.filter((_, j) => j !== i))} />
        </Space.Compact>
      ))}
      <Button size="small" icon={<PlusOutlined />} onClick={() => props.onChange({ ...props.value, [`arg${entries.length + 1}`]: '' })}>
        添加参数
      </Button>
    </Space>
  );
}

/** 节点配置表单的入口：schema 是 ListNodeTypes 返回的该类型配置 schema（含 $defs）。 */
export function SchemaForm(props: { schema: Schema; value: unknown; onChange: (v: unknown) => void; ctx: Omit<FormContext, 'root'> }) {
  const ctx = { ...props.ctx, root: props.schema };
  return <SchemaField schema={props.schema} value={props.value} onChange={props.onChange} ctx={ctx} name="config" />;
}
