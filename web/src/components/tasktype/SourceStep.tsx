// 第 1 步：基本信息与数据源。SQL 来源可以「试跑」：在只读事务中执行，看列与前 20 行，并据此生成字段。
import { PlayCircleOutlined, ThunderboltOutlined } from '@ant-design/icons';
import { Alert, App as AntApp, Button, Card, Descriptions, Form, Input, Radio, Select, Space, Table, Tag, Typography } from 'antd';
import { useEffect, useState } from 'react';
import { ApiError, decodeJson } from '../../api/client';
import { catalogApi, taskTypeApi } from '../../api/services';
import type { Issue, SourceState, TaskTypeBody, TTField } from '../../api/types';
import { cronText, fmtTime } from '../../format';
import { issuesAt, IssueList } from './shared';

export interface Trial {
  columns: string[];
  rows: Record<string, unknown>[];
  hasMore: boolean;
}

const mono = { fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace', fontSize: 13 } as const;

/** 由列名猜字段类型，配置人员可以再改。 */
function guessType(col: string, sample: unknown): TTField['type'] {
  if (/(_time|_at|time_ms|_date)$/.test(col)) return 'datetime';
  if (/amount|price|fee/.test(col)) return 'money';
  if (typeof sample === 'string' && /^-?\d+$/.test(sample) && sample.length < 6) return 'number';
  return 'text';
}

export function SourceStep(props: {
  body: TaskTypeBody;
  onChange?: (b: TaskTypeBody) => void;
  issues: Issue[];
  trial?: Trial;
  onTrial?: (t: Trial) => void;
  sourceState?: SourceState | null;
}) {
  const { body, onChange } = props;
  const ro = !onChange;
  const { message } = AntApp.useApp();
  const [datasources, setDatasources] = useState<string[]>([]);
  const [running, setRunning] = useState(false);
  const [trialError, setTrialError] = useState<string>();
  const src = body.source;
  const set = (patch: Partial<TaskTypeBody>) => onChange?.({ ...body, ...patch });
  const setSrc = (patch: Partial<TaskTypeBody['source']>) => set({ source: { ...src, ...patch } });

  useEffect(() => {
    if (!ro) catalogApi.datasources().then((r) => setDatasources(r.names), () => undefined);
  }, [ro]);

  const runTrial = async () => {
    setRunning(true);
    setTrialError(undefined);
    try {
      const r = await taskTypeApi.trialSql(src.datasource ?? '', src.query ?? '');
      props.onTrial?.({ columns: r.columns, rows: decodeJson<Record<string, unknown>[]>(r.rows_json) ?? [], hasMore: r.has_more });
    } catch (e) {
      setTrialError(e instanceof ApiError ? e.message : String(e));
    } finally {
      setRunning(false);
    }
  };

  const genFields = () => {
    const t = props.trial;
    if (!t) return;
    const have = new Set(body.fields.map((f) => f.name));
    const added = t.columns.filter((c) => !have.has(c)).map((c) => ({ name: c, label: '', type: guessType(c, t.rows[0]?.[c]) }));
    set({ fields: [...body.fields, ...added] });
    message.success(added.length ? `已添加 ${added.length} 个字段，请在「字段」中补充显示名` : '查询结果的列都已是字段');
  };

  const basic = (
    <Card size="small" title="基本信息" style={{ marginBottom: 16 }}>
      <IssueList issues={[...issuesAt(props.issues, 'name'), ...issuesAt(props.issues, 'key'), ...issuesAt(props.issues, 'priority')]} />
      {ro ? (
        <Descriptions
          size="small"
          column={{ xs: 1, md: 3 }}
          items={[
            { key: 'k', label: '标识', children: <Typography.Text code>{body.key}</Typography.Text> },
            { key: 'n', label: '命名空间', children: body.namespace || '—' },
            { key: 'p', label: '默认优先级', children: <Tag>{body.priority ?? 'P2'}</Tag> },
          ]}
        />
      ) : (
        <Form layout="vertical">
          <Space wrap align="start" size={16}>
            <Form.Item label="名称" required style={{ marginBottom: 8, width: 280 }}>
              <Input value={body.name} onChange={(e) => set({ name: e.target.value })} />
            </Form.Item>
            <Form.Item label="命名空间" style={{ marginBottom: 8, width: 160 }}>
              <Input value={body.namespace} onChange={(e) => set({ namespace: e.target.value })} />
            </Form.Item>
            <Form.Item label="新任务的优先级" style={{ marginBottom: 8 }}>
              <Radio.Group value={body.priority ?? 'P2'} onChange={(e) => set({ priority: e.target.value })} optionType="button"
                options={['P0', 'P1', 'P2', 'P3'].map((p) => ({ value: p, label: p }))} />
            </Form.Item>
          </Space>
          <Form.Item label="描述" style={{ marginBottom: 0 }}>
            <Input.TextArea rows={2} value={body.description} onChange={(e) => set({ description: e.target.value })} placeholder="这是什么任务、处理目标是什么" />
          </Form.Item>
        </Form>
      )}
    </Card>
  );

  const trial = props.trial;
  return (
    <>
      {basic}
      <Card size="small" title="数据源">
        <IssueList issues={issuesAt(props.issues, 'source')} />
        <Space direction="vertical" size={12} style={{ width: '100%' }}>
          <Radio.Group
            disabled={ro}
            value={src.kind}
            optionType="button"
            onChange={(e) => setSrc({ kind: e.target.value })}
            options={[
              { value: 'sql', label: '定时 SQL' },
              { value: 'api', label: 'API 推送' },
              { value: 'kafka', label: 'Kafka' },
            ]}
          />
          {src.kind === 'sql' ? (
            <>
              <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
                平台按 cron 执行查询，<b>结果集就是当前待办</b>：新出现的 key 创建任务，从结果中消失的 key 自动关闭任务，仍在的刷新字段。
                查询必须有一列叫 <Typography.Text code>key</Typography.Text>（去重键），其余列就是业务字段；时间条件直接写在 SQL 里。
              </Typography.Paragraph>
              <Space wrap size={16}>
                <span>
                  <Typography.Text type="secondary">数据源连接 </Typography.Text>
                  {ro ? (
                    <Typography.Text code>{src.datasource}</Typography.Text>
                  ) : (
                    <Select style={{ width: 200 }} value={src.datasource} placeholder="选择只读连接" onChange={(v) => setSrc({ datasource: v })}
                      options={datasources.map((d) => ({ value: d, label: d }))} />
                  )}
                </span>
                <span>
                  <Typography.Text type="secondary">调度 </Typography.Text>
                  {ro ? (
                    <Typography.Text code>{src.schedule}</Typography.Text>
                  ) : (
                    <Input style={{ width: 160, ...mono }} value={src.schedule} onChange={(e) => setSrc({ schedule: e.target.value })} placeholder="*/5 * * * *" />
                  )}
                  <Typography.Text type="secondary" style={{ marginInlineStart: 8 }}>
                    {cronText(src.schedule) || '5 段 cron：分 时 日 月 周'} · {src.timezone || 'Asia/Shanghai'}
                  </Typography.Text>
                </span>
              </Space>
              <Input.TextArea
                readOnly={ro}
                value={src.query}
                onChange={(e) => setSrc({ query: e.target.value })}
                autoSize={{ minRows: 6, maxRows: 18 }}
                style={mono}
                placeholder={'select id as `key`, title, amount\nfrom your_table\nwhere status = \'pending\''}
              />
              {!ro && (
                <Space>
                  <Button icon={<PlayCircleOutlined />} loading={running} disabled={!src.datasource || !src.query} onClick={() => void runTrial()}>
                    试跑
                  </Button>
                  <Button icon={<ThunderboltOutlined />} disabled={!trial} onClick={genFields}>
                    用结果生成字段
                  </Button>
                  <Typography.Text type="secondary">在只读事务中执行，最多返回 20 行</Typography.Text>
                </Space>
              )}
              {trialError && <Alert type="error" showIcon message="试跑失败" description={trialError} />}
              {trial && (
                <Table
                  size="small"
                  bordered
                  pagination={false}
                  scroll={{ x: 'max-content', y: 320 }}
                  rowKey={(_, i) => String(i)}
                  dataSource={trial.rows}
                  title={() => `${trial.rows.length}${trial.hasMore ? '+' : ''} 行`}
                  columns={['key', ...trial.columns].map((c) => ({
                    key: c,
                    dataIndex: c,
                    title: c === 'key' ? <b>key</b> : c,
                    render: (v: unknown) => (v === null || v === undefined ? <Typography.Text type="secondary">NULL</Typography.Text> : String(v)),
                  }))}
                />
              )}
              {props.sourceState && (
                <Descriptions
                  size="small"
                  bordered
                  column={{ xs: 1, md: 3 }}
                  title="运行状态"
                  items={[
                    { key: 'l', label: '上次执行', children: fmtTime(props.sourceState.last_run_at, 'MM-DD HH:mm:ss') },
                    { key: 'n', label: '下次执行', children: fmtTime(props.sourceState.next_run_at, 'MM-DD HH:mm:ss') },
                    { key: 'r', label: '上次结果', children: `${props.sourceState.last_rows} 行 · 新建 ${props.sourceState.last_created} · 关闭 ${props.sourceState.last_closed}` },
                    ...(props.sourceState.last_error
                      ? [{ key: 'e', label: '错误', span: 3, children: <Typography.Text type="danger">{props.sourceState.last_error}</Typography.Text> }]
                      : []),
                  ]}
                />
              )}
            </>
          ) : (
            <>
              <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
                业务方按平台协议推送（需要研发接入）。<Typography.Text code>op: upsert</Typography.Text> 表示 key 在待办里（没有任务就创建，有就刷新字段），
                <Typography.Text code>op: close</Typography.Text> 表示不在（自动关闭任务）；比已应用的消息旧（occurred_at 更小）的消息会被忽略。
                {src.kind === 'kafka' ? ' Kafka 消息发到平台固定的 topic wb.task.events，消息体如下。' : ' API 为 IngestService.Push（gateway 路径 /byai_platform/api/v1/ingest/push，fields 放在 fields_json 中），字段如下。'}
              </Typography.Paragraph>
              <pre style={{ ...mono, background: '#fafafa', padding: 12, borderRadius: 6, margin: 0 }}>
                {JSON.stringify(
                  {
                    task_type: body.key,
                    key: 'A1001',
                    op: 'upsert',
                    occurred_at: 1760918400000,
                    fields: Object.fromEntries(body.fields.map((f) => [f.name, f.type === 'money' ? '1200.50' : '...'])),
                  },
                  null,
                  2,
                )}
              </pre>
            </>
          )}
        </Space>
      </Card>
    </>
  );
}
