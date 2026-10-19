// 卡片模板预览：按 Lark / 任务详情的版式画出模板本身，引用的字段显示为标记。
// 版式与 TaskCard 一致（顶部色条、摘要、KV 表格），这样配置人员看到的就是处理人将看到的结构。
import { RobotOutlined } from '@ant-design/icons';
import { Card, Descriptions, Space, Tag, Typography } from 'antd';
import { useContext } from 'react';
import type { ViewTemplate } from './model';
import { NodeRef } from './refs';
import { FieldTitles, Tmpl } from './text';

const colors: Record<string, string> = { blue: '#1677ff', orange: '#fa8c16', red: '#f5222d', green: '#52c41a', grey: '#8c8c8c' };

export function ViewPreview(props: {
  name: string;
  view: ViewTemplate | string;
  isDefault: boolean;
  usedBy: string[];
  highlight: boolean;
}) {
  const titles = useContext(FieldTitles);
  const v = props.view;
  const extra = (
    <Space size={4} wrap>
      {props.isDefault && <Tag color="gold">默认</Tag>}
      {props.usedBy.length === 0 && !props.isDefault ? (
        <Typography.Text type="secondary">未被节点使用</Typography.Text>
      ) : (
        props.usedBy.map((id) => <NodeRef key={id} id={id} />)
      )}
    </Space>
  );

  return (
    <div id={`view-${props.name}`} style={{ scrollMarginTop: 16 }}>
      <Space style={{ marginBottom: 8, width: '100%', justifyContent: 'space-between' }} wrap>
        <Typography.Text strong style={{ fontFamily: 'ui-monospace, Menlo, monospace' }}>
          {props.name}
        </Typography.Text>
        {extra}
      </Space>
      {typeof v === 'string' ? (
        <Card size="small">
          <Typography.Text type="secondary">
            引用共享组件 <Tag color="purple">{v}</Tag>，内容在组件库中维护。
          </Typography.Text>
        </Card>
      ) : (
        <Card
          size="small"
          title={<Tmpl text={v.title} />}
          style={{
            boxShadow: props.highlight ? '0 0 0 2px #1677ff' : undefined,
            transition: 'box-shadow 0.3s',
          }}
          styles={{ header: { borderTop: `4px solid ${colors[v.color ?? 'blue'] ?? colors.blue}`, borderRadius: 0, whiteSpace: 'normal' } }}
        >
          {v.summary && (
            <Typography.Paragraph style={{ marginBottom: 12 }}>
              <Tmpl text={v.summary} />
            </Typography.Paragraph>
          )}
          {v.ai_summary?.enabled && (
            <Typography.Paragraph type="secondary" style={{ marginBottom: 12, fontSize: 12 }}>
              <RobotOutlined /> 发送后异步生成 AI 摘要并更新卡片
            </Typography.Paragraph>
          )}
          <Descriptions
            bordered
            size="small"
            column={1}
            items={v.fields.map((f, i) => {
              const m = f.value.match(/^\s*\{\{\s*payload\.(\w+)\s*\}\}\s*$/);
              return { key: i, label: f.label || (m && titles[m[1]]) || '—', children: <Tmpl text={f.value} /> };
            })}
            labelStyle={{ width: 112, color: 'rgba(0, 0, 0, 0.55)' }}
          />
          {(v.links ?? []).length > 0 && (
            <Space style={{ marginTop: 12 }} wrap>
              {v.links!.map((l, i) => (
                <Typography.Link key={i}>
                  {l.label}
                </Typography.Link>
              ))}
            </Space>
          )}
        </Card>
      )}
    </div>
  );
}
