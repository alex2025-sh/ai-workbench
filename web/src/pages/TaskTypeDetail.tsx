// 任务类型详情（已发布版本，只读）：与编辑页同样的四部分（数据源、字段、节点、卡片）只读展示，另有版本与 JSON。
// 当前 Tab 记在 URL 的 tab 参数里，便于分享链接。
import { ArrowLeftOutlined, EditOutlined } from '@ant-design/icons';
import { Button, Card, Descriptions, Result, Space, Spin, Table, Tabs, Typography } from 'antd';
import { useEffect, useMemo, useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { ApiError, decodeJson } from '../api/client';
import { taskTypeApi } from '../api/services';
import type { GetTaskTypeResp, TaskTypeBody, VersionInfo } from '../api/types';
import { JsonEditor } from '../components/JsonEditor';
import { CardStep } from '../components/tasktype/CardStep';
import { FieldsStep } from '../components/tasktype/FieldsStep';
import { NodesStep } from '../components/tasktype/NodesStep';
import { SourceStep } from '../components/tasktype/SourceStep';
import { fmtTime } from '../format';

export default function TaskTypeDetail() {
  const { key = '' } = useParams();
  const [params, setParams] = useSearchParams();
  const tab = params.get('tab') ?? 'definition';
  const [tt, setTt] = useState<GetTaskTypeResp>();
  const [versions, setVersions] = useState<VersionInfo[]>([]);
  const [error, setError] = useState<Error>();

  useEffect(() => {
    Promise.all([taskTypeApi.get(key), taskTypeApi.versions(key)])
      .then(([t, v]) => {
        setTt(t);
        setVersions(v.items);
      })
      .catch((e) => setError(e as Error));
  }, [key]);
  const body = useMemo(() => decodeJson<TaskTypeBody>(tt?.body_json), [tt]);

  if (error) {
    const nf = error instanceof ApiError && error.code === 'NotFound';
    return <Result status={nf ? '404' : 'error'} title={nf ? '任务类型不存在或尚未发布' : '加载失败'} subTitle={error.message}
      extra={nf && <Link to={`/config/task-types/${key}/edit`}>去编辑草稿</Link>} />;
  }
  if (!tt || !body) return <Spin style={{ display: 'block', marginTop: 80 }} />;

  return (
    <>
      <Space style={{ marginBottom: 16 }}>
        <Link to="/config/task-types">
          <ArrowLeftOutlined /> 任务类型
        </Link>
      </Space>
      <Card
        title={`${tt.summary.name}（${tt.summary.key}）`}
        extra={
          <Link to={`/config/task-types/${key}/edit`}>
            <Button type="primary" icon={<EditOutlined />}>
              {tt.summary.has_draft ? '继续编辑草稿' : '编辑'}
            </Button>
          </Link>
        }
        style={{ marginBottom: 16 }}
      >
        <Descriptions
          size="small"
          column={{ xs: 1, md: 3 }}
          items={[
            { key: 'ns', label: '命名空间', children: tt.summary.namespace || '—' },
            { key: 'v', label: '版本', children: `v${tt.version.version}` },
            { key: 'p', label: '发布', children: `${fmtTime(tt.version.published_at, 'YYYY-MM-DD HH:mm')} · ${tt.version.published_by}` },
          ]}
        />
        {body.description && <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>{body.description}</Typography.Paragraph>}
      </Card>
      <Tabs
        activeKey={tab}
        onChange={(k) => setParams(k === 'definition' ? {} : { tab: k }, { replace: true })}
        items={[
          {
            key: 'definition',
            label: '定义',
            children: (
              <Space direction="vertical" size={16} style={{ width: '100%' }}>
                <SourceStep body={body} issues={[]} sourceState={tt.source_state} />
                <FieldsStep body={body} issues={[]} />
                <NodesStep body={body} issues={[]} />
                <CardStep body={body} issues={[]} />
              </Space>
            ),
          },
          {
            key: 'versions',
            label: '版本',
            children: (
              <Table<VersionInfo>
                rowKey="version"
                size="small"
                pagination={false}
                dataSource={versions}
                columns={[
                  { title: '版本', dataIndex: 'version', render: (v: number) => <Link to={`/config/task-types/${key}?tab=versions`}>v{v}</Link> },
                  { title: '发布人', dataIndex: 'published_by' },
                  { title: '发布时间', dataIndex: 'published_at', render: (v: string) => fmtTime(v, 'YYYY-MM-DD HH:mm') },
                  { title: '内容哈希', dataIndex: 'content_hash', render: (v: string) => <Typography.Text code>{v.slice(0, 12)}</Typography.Text> },
                ]}
              />
            ),
          },
          { key: 'json', label: 'JSON', children: <JsonEditor value={body} onChange={() => undefined} readOnly height="70vh" /> },
        ]}
      />
    </>
  );
}
