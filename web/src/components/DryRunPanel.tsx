// 试运行面板：编辑样本与人工节点的预设选择，展示每个样本的路径、时间点与模拟发送（含渲染后的卡片）。
// 任务类型详情与编辑页共用；有草稿时后端试运行草稿，否则试运行最新发布版本。
import { PlayCircleOutlined } from '@ant-design/icons';
import { Alert, Button, Card, Col, Divider, Empty, Input, List, Row, Select, Space, Tag, Typography } from 'antd';
import { useState } from 'react';
import { ApiError, decodeJson, encodeJson } from '../api/client';
import { taskTypeApi } from '../api/services';
import type { Card as CardData, DryRunCase, DryRunResp, TaskTypeBody } from '../api/types';
import { fmtDuration } from '../format';
import { TaskCard } from './TaskCard';

// 按 schema 生成一条样本模板：字符串为空、数字为 0，datetime 格式的整数取一小时前。
function sampleTemplate(body: TaskTypeBody): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [k, p] of Object.entries(body.schema?.properties ?? {})) {
    const prop = p as { type?: string; 'x-format'?: string };
    if (prop.type === 'integer' || prop.type === 'number') {
      out[k] = prop['x-format'] === 'datetime' ? Date.now() - 3_600_000 : 0;
    } else {
      out[k] = '';
    }
  }
  return out;
}

export function DryRunPanel({ taskKey, body, beforeRun }: { taskKey: string; body: TaskTypeBody; beforeRun?: () => Promise<void> }) {
  const humans = (body.workflow?.nodes ?? []).filter((n) => n.type === 'human_choice');
  const [samples, setSamples] = useState(() => JSON.stringify([sampleTemplate(body)], null, 2));
  const [choices, setChoices] = useState<Record<string, string>>({});
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<DryRunResp>();
  const [error, setError] = useState<string>();

  const run = async () => {
    let parsed: unknown;
    try {
      parsed = JSON.parse(samples);
    } catch (e) {
      setError(`样本不是合法的 JSON：${(e as Error).message}`);
      return;
    }
    const list = Array.isArray(parsed) ? parsed : [parsed];
    setRunning(true);
    setError(undefined);
    try {
      await beforeRun?.(); // 编辑页：先把未保存的修改存下来，试运行的是最新草稿
      setResult(await taskTypeApi.dryRun({ key: taskKey, sample_json: list.map((s) => encodeJson(s)), choices }));
    } catch (e) {
      setError(e instanceof ApiError ? `${e.code}: ${e.message}` : String(e));
    } finally {
      setRunning(false);
    }
  };

  return (
    <Row gutter={16}>
      <Col span={9}>
        <Card title="输入" size="small">
          <Typography.Paragraph type="secondary">
            样本是 payload 的 JSON 数组，每条走一遍流程。动作只记录不发送，定时器与超时即时快进；对最新发布版本试运行。
          </Typography.Paragraph>
          <Input.TextArea value={samples} onChange={(e) => setSamples(e.target.value)} rows={16} style={{ fontFamily: 'monospace', fontSize: 12 }} />
          {humans.length > 0 && (
            <>
              <Divider orientation="left" plain>
                人工节点预设选择
              </Divider>
              {humans.map((n) => (
                <Space key={n.id} style={{ display: 'flex', marginBottom: 8 }}>
                  <Typography.Text code>{n.id}</Typography.Text>
                  <Select
                    allowClear
                    placeholder="不预设（有超时走超时，否则停在等待）"
                    style={{ width: 300 }}
                    value={choices[n.id]}
                    onChange={(v?: string) => setChoices((c) => ({ ...Object.fromEntries(Object.entries(c).filter(([k]) => k !== n.id)), ...(v ? { [n.id]: v } : {}) }))}
                    options={(n.config?.options ?? []).map((o) => ({ value: o.key, label: o.snooze ? `${o.label}（推迟，暂不支持）` : o.label }))}
                  />
                </Space>
              ))}
            </>
          )}
          <Button type="primary" icon={<PlayCircleOutlined />} loading={running} onClick={() => void run()} style={{ marginTop: 12 }}>
            开始试运行
          </Button>
          {error && <Alert type="error" showIcon message={error} style={{ marginTop: 12 }} />}
        </Card>
      </Col>
      <Col span={15}>
        {!result ? (
          <Empty description="试运行结果显示在这里" />
        ) : (
          <>
            <Alert
              type={result.passed ? 'success' : 'error'}
              showIcon
              message={result.passed ? '试运行通过' : '试运行未通过'}
              style={{ marginBottom: 16 }}
            />
            {result.cases.map((c) => (
              <CaseReport key={c.sample_index} c={c} />
            ))}
          </>
        )}
      </Col>
    </Row>
  );
}

const endLabel: Record<string, string> = {
  succeeded: '流程结束',
  waiting: '停在人工节点等待',
  failed: '失败',
  needs_intervention: '需人工介入',
  invalid: '样本无效',
  running: '未走完',
};

function CaseReport({ c }: { c: DryRunCase }) {
  return (
    <Card
      size="small"
      style={{ marginBottom: 16 }}
      title={
        <Space>
          样本 #{c.sample_index + 1}
          <Tag color={c.issues.length ? 'error' : c.end_status === 'succeeded' ? 'success' : 'processing'}>{endLabel[c.end_status] ?? c.end_status}</Tag>
          {c.assignee_id && <Typography.Text type="secondary">处理人 {c.assignee_id}</Typography.Text>}
        </Space>
      }
    >
      {c.issues.map((i, k) => (
        <Alert key={k} type="error" showIcon message={`${i.code}${i.node_id ? ` · ${i.node_id}` : ''}：${i.message}`} style={{ marginBottom: 8 }} />
      ))}
      <Typography.Text strong>路径</Typography.Text>
      <div style={{ margin: '8px 0 16px' }}>
        {c.path.map((s) => (
          <Tag key={`${s.node_id}:${s.iteration}`} color={s.status === 'failed' ? 'error' : s.status === 'waiting' ? 'processing' : 'default'}>
            {s.node_id}
            {s.port && ` → ${s.port}`}
          </Tag>
        ))}
      </div>
      {c.timeline.length > 0 && (
        <>
          <Typography.Text strong>时间点（相对开始）</Typography.Text>
          <div style={{ margin: '8px 0 16px' }}>
            {c.timeline.map((p, k) => (
              <Tag key={k}>
                {p.node_id} · {p.kind === 'timeout' ? '超时' : '定时'} · {fmtDuration(p.offset_ms)}
              </Tag>
            ))}
          </div>
        </>
      )}
      <Typography.Text strong>模拟发送（{c.sends.length}）</Typography.Text>
      <List
        size="small"
        dataSource={c.sends}
        renderItem={(s) => {
          const card = decodeJson<CardData>(s.rendered_json);
          return (
            <List.Item style={{ display: 'block' }}>
              <Space style={{ marginBottom: card ? 8 : 0 }}>
                <Tag>{s.kind}</Tag>
                <Typography.Text code>{s.node_id}</Typography.Text>→<Typography.Text>{s.target}</Typography.Text>
                {!card && <Typography.Text type="secondary">{s.summary}</Typography.Text>}
              </Space>
              {card && <TaskCard card={card} size="small" />}
            </List.Item>
          );
        }}
      />
    </Card>
  );
}
