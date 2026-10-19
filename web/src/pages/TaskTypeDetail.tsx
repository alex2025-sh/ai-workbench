// 任务类型详情（已发布版本，只读）：定义、试运行与版本；「编辑」进入草稿与画布。
// 默认打开「定义」，当前 Tab 记在 URL 的 tab 参数里，便于分享链接。
import { ArrowLeftOutlined, EditOutlined } from '@ant-design/icons';
import { Button, Card, Descriptions, Result, Space, Spin, Table, Tabs, Typography } from 'antd';
import { useEffect, useMemo, useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { ApiError, decodeJson } from '../api/client';
import { taskTypeApi } from '../api/services';
import type { GetTaskTypeResp, TaskTypeBody, VersionInfo } from '../api/types';
import { DryRunPanel } from '../components/DryRunPanel';
import type { Binding, Definition } from '../components/definition/model';
import { TaskTypeDefinition } from '../components/definition/TaskTypeDefinition';
import { fmtTime } from '../format';

export default function TaskTypeDetail() {
  const { key = '' } = useParams();
  const [tt, setTt] = useState<GetTaskTypeResp>();
  const [versions, setVersions] = useState<VersionInfo[]>([]);
  const [error, setError] = useState<Error>();
  const [params, setParams] = useSearchParams();
  const tab = params.get('tab') ?? 'definition';

  useEffect(() => {
    Promise.all([taskTypeApi.get(key), taskTypeApi.versions(key)])
      .then(([t, v]) => {
        setTt(t);
        setVersions(v.items);
      })
      .catch((e) => setError(e as Error));
  }, [key]);

  const body = useMemo(() => decodeJson<TaskTypeBody>(tt?.body_json), [tt]);
  const bindings = useMemo(() => decodeJson<Record<string, Binding>>(tt?.bindings_json) ?? {}, [tt]);

  if (error) {
    const notFound = error instanceof ApiError && error.code === 'NotFound';
    return <Result status={notFound ? '404' : 'error'} title={notFound ? '任务类型不存在' : '加载失败'} subTitle={error.message} />;
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
        <Descriptions size="small" column={{ xs: 1, md: 3 }}>
          <Descriptions.Item label="Namespace">{tt.summary.namespace}</Descriptions.Item>
          <Descriptions.Item label="版本">v{tt.version.version}</Descriptions.Item>
          <Descriptions.Item label="发布">{fmtTime(tt.version.published_at, 'YYYY-MM-DD HH:mm')} · {tt.version.published_by}</Descriptions.Item>
        </Descriptions>
        {body.description && <Typography.Paragraph type="secondary">{body.description}</Typography.Paragraph>}
      </Card>
      <Tabs
        activeKey={tab}
        onChange={(k) => setParams(k === 'definition' ? {} : { tab: k }, { replace: true })}
        items={[
          {
            key: 'definition',
            label: '定义',
            children: <TaskTypeDefinition body={body as unknown as Definition} bindings={bindings} version={tt.version.version} />,
          },
          { key: 'dry-run', label: '试运行', children: <DryRunPanel taskKey={key} body={body} /> },
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
                  { title: '版本', dataIndex: 'version', render: (v: number) => `v${v}` },
                  { title: '状态', dataIndex: 'state' },
                  { title: '发布人', dataIndex: 'published_by' },
                  { title: '发布时间', dataIndex: 'published_at', render: (v: string) => fmtTime(v, 'YYYY-MM-DD HH:mm') },
                  { title: '内容哈希', dataIndex: 'content_hash', render: (v: string) => <Typography.Text code>{v.slice(0, 12)}</Typography.Text> },
                ]}
              />
            ),
          },
        ]}
      />
    </>
  );
}
