// 任务类型列表：已发布的与新建尚未发布的；新建后进入编辑页（四步表单）。
import { PlusOutlined } from '@ant-design/icons';
import { Alert, Button, Input, Space, Table, Tag } from 'antd';
import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { ApiError } from '../api/client';
import { taskTypeApi } from '../api/services';
import type { TaskTypeSummary } from '../api/types';
import { fmtTime } from '../format';
import { CreateTaskTypeModal } from './TaskTypeEditor';

export default function TaskTypes() {
  const [keyword, setKeyword] = useState('');
  const [items, setItems] = useState<TaskTypeSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string>();
  const [creating, setCreating] = useState(false);

  useEffect(() => {
    setLoading(true);
    taskTypeApi
      .list({ keyword })
      .then((r) => {
        setItems(r.items);
        setError(undefined);
      })
      .catch((e) => setError(e instanceof ApiError ? `${e.code}: ${e.message}` : String(e)))
      .finally(() => setLoading(false));
  }, [keyword]);

  return (
    <>
      <Space style={{ marginBottom: 16, width: '100%', justifyContent: 'space-between' }}>
        <Input.Search placeholder="按 key 或名称搜索" allowClear style={{ width: 280 }} onSearch={(v) => setKeyword(v.trim())} />
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreating(true)}>
          新建任务类型
        </Button>
      </Space>
      <CreateTaskTypeModal open={creating} onClose={() => setCreating(false)} />
      {error && <Alert type="error" showIcon message={error} style={{ marginBottom: 16 }} />}
      <Table<TaskTypeSummary>
        rowKey="key"
        loading={loading}
        dataSource={items}
        pagination={false}
        columns={[
          {
            title: 'Key',
            dataIndex: 'key',
            render: (k: string, t) => <Link to={t.latest_version > 0 ? `/config/task-types/${k}` : `/config/task-types/${k}/edit`}>{k}</Link>,
          },
          { title: '名称', dataIndex: 'name' },
          { title: '命名空间', dataIndex: 'namespace' },
          { title: '数据源', dataIndex: 'source_kind', render: (k: string) => ({ sql: '定时 SQL', api: 'API 推送', kafka: 'Kafka' } as Record<string, string>)[k] ?? '—' },
          {
            title: '最新版本',
            render: (_, t) => (
              <Space>
                {t.latest_version > 0 ? `v${t.latest_version}` : <Tag>未发布</Tag>}
                {t.has_draft && <Tag color="gold">草稿</Tag>}
              </Space>
            ),
          },
          { title: '更新时间', dataIndex: 'updated_at', render: (v: string) => fmtTime(v, 'YYYY-MM-DD HH:mm') },
          { title: '', render: (_, t) => <Link to={`/config/task-types/${t.key}/edit`}>{t.has_draft ? '继续编辑' : '编辑'}</Link> },
        ]}
      />
    </>
  );
}
