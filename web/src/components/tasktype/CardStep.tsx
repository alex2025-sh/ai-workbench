// 第 4 步：卡片 = 标题模板 + 从字段中挑选的 KV。右侧用一行样例数据（优先取试跑 SQL 的第一行）实时预览，
// 预览由后端渲染，与工作台、Lark 看到的完全一致（含脱敏与格式化）。
import { Card, Col, Dropdown, Form, Input, Row, Select, Space, Typography } from 'antd';
import { useEffect, useState } from 'react';
import { encodeJson } from '../../api/client';
import { taskTypeApi } from '../../api/services';
import type { Card as CardData, Issue, TaskTypeBody } from '../../api/types';
import { TaskCard } from '../TaskCard';
import { issuesAt, IssueList, sampleValues } from './shared';

export function CardStep(props: { body: TaskTypeBody; onChange?: (b: TaskTypeBody) => void; issues: Issue[]; sampleRow?: Record<string, unknown> }) {
  const { body, onChange } = props;
  const ro = !onChange;
  const [node, setNode] = useState<string>();
  const [preview, setPreview] = useState<CardData | null>(null);
  const card = body.card;
  const setCard = (patch: Partial<TaskTypeBody['card']>) => onChange?.({ ...body, card: { ...card, ...patch } });

  // 预览：内容变化后 300ms 请求一次
  useEffect(() => {
    const timer = window.setTimeout(() => {
      const sample = sampleValues(body, props.sampleRow);
      taskTypeApi
        .previewCard({ body_json: encodeJson(body), node_id: node, key: sample.key, values_json: encodeJson(sample.values) })
        .then((r) => setPreview(r.card), () => setPreview(null));
    }, 300);
    return () => window.clearTimeout(timer);
  }, [body, node, props.sampleRow]);

  const fieldOptions = body.fields.map((f) => ({ value: f.name, label: f.label || f.name }));
  return (
    <Row gutter={16}>
      <Col xs={24} lg={12}>
        <Card size="small" title="卡片内容">
          <IssueList issues={issuesAt(props.issues, 'card')} />
          <Form layout="vertical">
            <Form.Item label="标题" tooltip="{字段名} 替换为字段值，{key} 为数据源给出的 key" style={{ marginBottom: 12 }}>
              {ro ? (
                <Typography.Text code>{card.title}</Typography.Text>
              ) : (
                <Space.Compact style={{ width: '100%' }}>
                  <Input value={card.title} onChange={(e) => setCard({ title: e.target.value })} placeholder="如：VIP 工单待审核：{appeal_id}" />
                  <Dropdown
                    trigger={['click']}
                    menu={{
                      items: [{ key: 'key', label: 'key' }, ...fieldOptions.map((f) => ({ key: f.value, label: f.label }))],
                      onClick: ({ key }) => setCard({ title: `${card.title}{${key}}` }),
                    }}
                  >
                    <a style={{ padding: '4px 11px', border: '1px solid #d9d9d9', borderInlineStart: 0, borderRadius: '0 6px 6px 0', whiteSpace: 'nowrap' }}>插入字段</a>
                  </Dropdown>
                </Space.Compact>
              )}
            </Form.Item>
            <Form.Item label="卡片上的字段" tooltip="按选择的顺序显示；工作台卡片显示前 3 个" style={{ marginBottom: 12 }}>
              <Select
                mode="multiple"
                disabled={ro}
                value={card.fields ?? []}
                onChange={(v) => setCard({ fields: v })}
                options={fieldOptions}
                placeholder="选择字段"
              />
            </Form.Item>
            {body.nodes.length > 1 && (
              <Form.Item label="预览哪个节点" style={{ marginBottom: 0 }}>
                <Select style={{ width: 240 }} value={node ?? body.nodes[0].id} onChange={setNode} options={body.nodes.map((n) => ({ value: n.id, label: n.name }))} />
              </Form.Item>
            )}
          </Form>
        </Card>
      </Col>
      <Col xs={24} lg={12}>
        <Typography.Text type="secondary" style={{ display: 'block', marginBottom: 8 }}>
          预览{props.sampleRow ? '（试跑结果的第一行）' : '（样例数据）'}
        </Typography.Text>
        <TaskCard card={preview} />
      </Col>
    </Row>
  );
}
