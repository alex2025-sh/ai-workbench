// 选中节点的配置面板：id、名称，以及按 schema 生成的配置表单（可切到 JSON）。
import { DeleteOutlined } from '@ant-design/icons';
import { Alert, Button, Card, Input, Popconfirm, Space, Tabs, Typography } from 'antd';
import { useEffect, useMemo, useState } from 'react';
import { decodeJson } from '../../api/client';
import type { NodeTypeInfo } from '../../api/types';
import { JsonEditor } from '../JsonEditor';
import { SchemaForm } from '../schemaform/SchemaForm';
import type { WfNode } from './model';

const ID_PATTERN = /^[a-z][a-z0-9_]{0,63}$/;

export function NodeConfigPanel(props: {
  node: WfNode;
  info?: NodeTypeInfo;
  takenIds: string[];
  views: string[];
  skills: string[];
  onChange: (n: WfNode) => void;
  onRename: (newId: string) => void;
  onDelete: () => void;
}) {
  const { node, info } = props;
  const schema = useMemo(() => decodeJson<Record<string, unknown>>(info?.config_schema_json), [info]);
  const [id, setId] = useState(node.id);
  useEffect(() => setId(node.id), [node.id]);

  const idError = id === node.id ? undefined : !ID_PATTERN.test(id) ? '只能用小写字母、数字、下划线，字母开头' : props.takenIds.includes(id) ? 'id 已存在' : undefined;
  const { id: _id, type: _type, ...rest } = node;
  const hasConfig = schema && Object.keys((schema.$defs as Record<string, { properties?: object }>)?.[`config_${node.type}`]?.properties ?? {}).length > 0;

  return (
    <Card
      size="small"
      title={
        <Space>
          {info?.display_name ?? node.type}
          <Typography.Text type="secondary" style={{ fontWeight: 'normal' }}>
            {node.type}
          </Typography.Text>
        </Space>
      }
      extra={
        node.type !== 'trigger' && (
          <Popconfirm title="删除节点及其连线？" onConfirm={props.onDelete}>
            <Button size="small" type="text" danger icon={<DeleteOutlined />} />
          </Popconfirm>
        )
      }
      style={{ height: '100%', overflow: 'auto' }}
    >
      {info?.description && <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>{info.description}</Typography.Paragraph>}
      <div style={{ marginBottom: 12 }}>
        <Typography.Text>节点 id</Typography.Text>
        <Input
          value={id}
          status={idError ? 'error' : undefined}
          onChange={(e) => setId(e.target.value)}
          onBlur={() => (idError || id === node.id ? setId(node.id) : props.onRename(id))}
          onPressEnter={(e) => (e.target as HTMLInputElement).blur()}
        />
        {idError && <Typography.Text type="danger" style={{ fontSize: 12 }}>{idError}</Typography.Text>}
      </div>
      <div style={{ marginBottom: 12 }}>
        <Typography.Text>名称</Typography.Text>
        <Input value={node.name ?? ''} placeholder="显示在画布与任务进度中" onChange={(e) => props.onChange({ ...node, name: e.target.value || undefined })} />
      </div>
      <Tabs
        size="small"
        items={[
          {
            key: 'form',
            label: '配置',
            children: !schema ? (
              <Alert type="warning" message="没有这种节点的配置 schema" />
            ) : hasConfig ? (
              <SchemaForm
                schema={schema}
                value={node.config ?? {}}
                onChange={(c) => props.onChange({ ...node, config: c && Object.keys(c as object).length ? (c as Record<string, unknown>) : undefined })}
                ctx={{ views: props.views, skills: props.skills }}
              />
            ) : (
              <Typography.Text type="secondary">这种节点没有配置项。</Typography.Text>
            ),
          },
          {
            key: 'json',
            label: 'JSON',
            children: (
              <>
                <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
                  节点的 name、config、retry、side_effects；id 与类型在上方修改。
                </Typography.Paragraph>
                <JsonEditor
                  value={rest}
                  height="420px"
                  onChange={(v) => {
                    if (v && typeof v === 'object' && !Array.isArray(v)) props.onChange({ ...(v as object), id: node.id, type: node.type } as WfNode);
                  }}
                />
              </>
            ),
          },
        ]}
      />
    </Card>
  );
}
