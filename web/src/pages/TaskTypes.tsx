// 任务类型列表（只读）：草稿与变更集尚未实现，列出已发布的最新版本。
import { Alert, Input, Table } from 'antd';
import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { ApiError } from '../api/client';
import { taskTypeApi } from '../api/services';
import type { TaskTypeSummary } from '../api/types';
import { fmtTime } from '../format';

export default function TaskTypes() {
  const [keyword, setKeyword] = useState('');
  const [items, setItems] = useState<TaskTypeSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string>();

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
      <Input.Search placeholder="按 key 或名称搜索" allowClear style={{ width: 280, marginBottom: 16 }} onSearch={(v) => setKeyword(v.trim())} />
      {error && <Alert type="error" showIcon message={error} style={{ marginBottom: 16 }} />}
      <Table<TaskTypeSummary>
        rowKey="key"
        loading={loading}
        dataSource={items}
        pagination={false}
        columns={[
          { title: 'Key', dataIndex: 'key', render: (k: string) => <Link to={`/config/task-types/${k}`}>{k}</Link> },
          { title: '名称', dataIndex: 'name' },
          { title: 'Namespace', dataIndex: 'namespace' },
          { title: '最新版本', dataIndex: 'latest_version', render: (v: number) => `v${v}` },
          { title: '发布时间', dataIndex: 'updated_at', render: (v: string) => fmtTime(v, 'YYYY-MM-DD HH:mm') },
        ]}
      />
    </>
  );
}
