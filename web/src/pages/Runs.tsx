// 运行监控列表：按状态、任务类型过滤；needs_intervention 不同步到任务中心，在这里置顶提醒。
import { ReloadOutlined, WarningOutlined } from '@ant-design/icons';
import { Alert, Badge, Button, Input, Select, Space, Table, Tag, Tooltip, Typography } from 'antd';
import { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { ApiError } from '../api/client';
import { runApi, taskTypeApi } from '../api/services';
import type { RunSummary } from '../api/types';
import { fmtTime, runStatusColor, runStatusLabel } from '../format';

export default function Runs() {
  const [status, setStatus] = useState<string>();
  const [taskType, setTaskType] = useState<string>();
  const [taskId, setTaskId] = useState('');
  const [types, setTypes] = useState<string[]>([]);
  const [items, setItems] = useState<RunSummary[]>([]);
  const [cursor, setCursor] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string>();
  const [intervention, setIntervention] = useState(0);

  const load = useCallback(
    async (after = '') => {
      setLoading(true);
      setError(undefined);
      try {
        const res = await runApi.list({ status, task_type_key: taskType, task_id: taskId || undefined, page: { cursor: after, page_size: 20 } });
        setItems((prev) => (after ? [...prev, ...res.items] : res.items));
        setCursor(res.next_cursor);
        const ni = await runApi.list({ status: 'needs_intervention', page: { page_size: 100 } });
        setIntervention(ni.items.length);
      } catch (e) {
        setError(e instanceof ApiError ? `${e.code}: ${e.message}` : String(e));
      } finally {
        setLoading(false);
      }
    },
    [status, taskType, taskId],
  );

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    taskTypeApi
      .list()
      .then((r) => setTypes(r.items.map((t) => t.key)))
      .catch(() => setTypes([]));
  }, []);

  return (
    <>
      <Space wrap style={{ marginBottom: 16 }}>
        <Badge count={intervention} size="small">
          <Button
            icon={<WarningOutlined />}
            danger={intervention > 0}
            type={status === 'needs_intervention' ? 'primary' : 'default'}
            onClick={() => setStatus(status === 'needs_intervention' ? undefined : 'needs_intervention')}
          >
            需人工介入
          </Button>
        </Badge>
        <Select
          allowClear
          placeholder="状态"
          style={{ width: 140 }}
          value={status}
          onChange={setStatus}
          options={Object.entries(runStatusLabel).map(([value, label]) => ({ value, label }))}
        />
        <Select
          allowClear
          placeholder="任务类型"
          style={{ width: 200 }}
          value={taskType}
          onChange={setTaskType}
          options={types.map((k) => ({ value: k, label: k }))}
        />
        <Input.Search placeholder="任务 ID" allowClear style={{ width: 160 }} onSearch={(v) => setTaskId(v.trim())} />
        <Button icon={<ReloadOutlined />} onClick={() => void load()} loading={loading}>
          刷新
        </Button>
      </Space>
      {error && <Alert type="error" showIcon message={error} style={{ marginBottom: 16 }} />}
      <Table<RunSummary>
        rowKey="run_id"
        loading={loading && items.length === 0}
        dataSource={items}
        pagination={false}
        columns={[
          { title: 'Run', dataIndex: 'run_id', render: (v: string) => <Link to={`/runs/${v}`}>#{v}</Link> },
          { title: '任务类型', render: (_, r) => `${r.task_type_key}@${r.task_type_version}` },
          { title: '状态', dataIndex: 'status', render: (s: string) => <Tag color={runStatusColor[s]}>{runStatusLabel[s] ?? s}</Tag> },
          {
            title: '卡在',
            render: (_, r) =>
              r.blocked_node ? (
                <Tooltip title={r.blocked_reason}>
                  <Typography.Text code>{r.blocked_node}</Typography.Text>
                  <Typography.Text type="danger" ellipsis style={{ maxWidth: 320, marginLeft: 4, verticalAlign: 'bottom' }}>
                    {r.blocked_reason}
                  </Typography.Text>
                </Tooltip>
              ) : (
                '—'
              ),
          },
          { title: '创建', dataIndex: 'created_at', render: (v: string) => fmtTime(v, 'MM-DD HH:mm:ss') },
          { title: '更新', dataIndex: 'updated_at', render: (v: string) => fmtTime(v, 'MM-DD HH:mm:ss') },
        ]}
      />
      {cursor && (
        <div style={{ textAlign: 'center', marginTop: 16 }}>
          <Button onClick={() => void load(cursor)} loading={loading}>
            加载更多
          </Button>
        </div>
      )}
    </>
  );
}
