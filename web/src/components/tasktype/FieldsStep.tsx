// 第 2 步：业务字段。顺序即详情页的显示顺序；显示名同时用作卡片上的标签；脱敏在工作台与 Lark 都生效。
import { ArrowDownOutlined, ArrowUpOutlined, DeleteOutlined, PlusOutlined } from '@ant-design/icons';
import { Button, Card, Input, Select, Space, Table, Tag, Tooltip, Typography } from 'antd';
import type { FieldType, Issue, TaskTypeBody, TTField } from '../../api/types';
import { fieldTypeLabel, issuesAt, IssueList, piiLabel } from './shared';

const mono = { fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace' } as const;

/** 枚举取值的文本形式：每行「值=显示名」。 */
function optionsText(f: TTField): string {
  return (f.options ?? []).map((o) => `${o.value}=${o.label}`).join('\n');
}
function parseOptions(text: string): TTField['options'] {
  return text
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)
    .map((l) => {
      const [value, ...rest] = l.split('=');
      return { value: value.trim(), label: (rest.join('=') || value).trim() };
    });
}

export function FieldsStep(props: { body: TaskTypeBody; onChange?: (b: TaskTypeBody) => void; issues: Issue[] }) {
  const { body, onChange } = props;
  const ro = !onChange;
  const fields = body.fields;
  const setFields = (fs: TTField[]) => onChange?.({ ...body, fields: fs });
  const update = (i: number, patch: Partial<TTField>) => setFields(fields.map((f, n) => (n === i ? { ...f, ...patch } : f)));
  const move = (i: number, d: number) => {
    const fs = [...fields];
    [fs[i], fs[i + d]] = [fs[i + d], fs[i]];
    setFields(fs);
  };
  const usedIn = (name: string) =>
    [
      body.card.title.includes(`{${name}}`) || body.card.fields?.includes(name) ? '卡片' : '',
      body.nodes.some((n) => n.assignee.field === name) ? '分配' : '',
      body.nodes.some((n) => n.link?.url.includes(`{${name}}`)) ? '链接' : '',
    ].filter(Boolean);

  return (
    <Card
      size="small"
      title={`字段（${fields.length}）`}
      extra={
        !ro && (
          <Button size="small" icon={<PlusOutlined />} onClick={() => setFields([...fields, { name: '', label: '', type: 'text' }])}>
            添加字段
          </Button>
        )
      }
    >
      <IssueList issues={issuesAt(props.issues, 'fields')} />
      <Typography.Paragraph type="secondary">
        {body.source.kind === 'sql' ? 'SQL 来源的字段名就是查询结果的列名（key 列除外），可在「数据源」试跑后一键生成。' : '推送消息 fields 中的字段，按这里的声明校验。'}
        金额用「金额」类型（按字符串保存，不丢精度）；时间可以是 Unix 秒、毫秒或日期文本。
      </Typography.Paragraph>
      <Table<TTField>
        size="small"
        pagination={false}
        rowKey={(_, i) => String(i)}
        dataSource={fields}
        scroll={{ x: 'max-content' }}
        columns={[
          {
            title: '字段名',
            width: 180,
            render: (_, f, i) =>
              ro ? <Typography.Text style={mono}>{f.name}</Typography.Text> : (
                <Input size="small" style={mono} value={f.name} status={issuesAt(props.issues, `fields.${i}.name`).length ? 'error' : undefined}
                  onChange={(e) => update(i, { name: e.target.value.trim() })} placeholder="列名" />
              ),
          },
          {
            title: '显示名',
            width: 160,
            render: (_, f, i) => (ro ? f.label || '—' : <Input size="small" value={f.label} onChange={(e) => update(i, { label: e.target.value })} placeholder="如：工单号" />),
          },
          {
            title: '类型',
            width: 120,
            render: (_, f, i) =>
              ro ? fieldTypeLabel[f.type ?? 'text'] : (
                <Select size="small" style={{ width: 100 }} value={f.type ?? 'text'} onChange={(v: FieldType) => update(i, { type: v, options: v === 'enum' ? f.options ?? [] : undefined })}
                  options={Object.entries(fieldTypeLabel).map(([value, label]) => ({ value, label }))} />
              ),
          },
          {
            title: '脱敏',
            width: 130,
            render: (_, f, i) =>
              ro ? (f.pii ? <Tag color="orange">{piiLabel[f.pii]}</Tag> : '—') : (
                <Select size="small" style={{ width: 110 }} value={f.pii ?? ''} onChange={(v) => update(i, { pii: (v || undefined) as TTField['pii'] })}
                  options={[{ value: '', label: '不脱敏' }, ...Object.entries(piiLabel).map(([value, label]) => ({ value, label }))]} />
              ),
          },
          {
            title: '枚举取值',
            render: (_, f, i) =>
              f.type !== 'enum' ? <Typography.Text type="secondary">—</Typography.Text> : ro ? (
                <Space size={[4, 4]} wrap>{(f.options ?? []).map((o) => <Tooltip key={o.value} title={o.value}><Tag>{o.label}</Tag></Tooltip>)}</Space>
              ) : (
                <Input.TextArea size="small" autoSize={{ minRows: 1, maxRows: 6 }} style={{ width: 220, ...mono }} defaultValue={optionsText(f)}
                  onBlur={(e) => update(i, { options: parseOptions(e.target.value) })} placeholder={'每行一个：值=显示名\nCOMMITED=待审核'} />
              ),
          },
          {
            title: '用于',
            width: 120,
            render: (_, f) => <Space size={4}>{usedIn(f.name).map((u) => <Tag key={u} color="blue">{u}</Tag>)}</Space>,
          },
          ...(ro
            ? []
            : [
                {
                  title: '',
                  width: 110,
                  render: (_: unknown, __: TTField, i: number) => (
                    <Space size={0}>
                      <Button size="small" type="text" icon={<ArrowUpOutlined />} disabled={i === 0} onClick={() => move(i, -1)} aria-label="上移" />
                      <Button size="small" type="text" icon={<ArrowDownOutlined />} disabled={i === fields.length - 1} onClick={() => move(i, 1)} aria-label="下移" />
                      <Button size="small" type="text" danger icon={<DeleteOutlined />} onClick={() => setFields(fields.filter((_, n) => n !== i))} aria-label="删除" />
                    </Space>
                  ),
                },
              ]),
        ]}
      />
    </Card>
  );
}
