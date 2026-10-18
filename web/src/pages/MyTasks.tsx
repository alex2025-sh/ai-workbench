// 我的任务 / 可认领：后端按优先级、截止时间排序，前端平铺并用左侧色条区分优先级（游标分页下分组计数不准，不做分组）。
import { ReloadOutlined } from '@ant-design/icons';
import { Alert, Button, Empty, List, Space, Tabs, Tag, Typography } from 'antd';
import { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { ApiError } from '../api/client';
import { taskApi } from '../api/services';
import type { ListTasksResp, TaskPriority, TaskStatus, TaskSummary } from '../api/types';
import { fmtTime, priorityColor, priorityLabel, taskStatusColor, taskStatusLabel } from '../format';

const statusOptions: TaskStatus[] = [
  'TASK_STATUS_PENDING',
  'TASK_STATUS_PROCESSING',
  'TASK_STATUS_SNOOZED',
  'TASK_STATUS_SOLVED',
];
const activeStatuses: TaskStatus[] = ['TASK_STATUS_PENDING', 'TASK_STATUS_PROCESSING', 'TASK_STATUS_SNOOZED'];
const priorityOptions: TaskPriority[] = ['TASK_PRIORITY_P0', 'TASK_PRIORITY_P1', 'TASK_PRIORITY_P2', 'TASK_PRIORITY_P3'];

type TabKey = 'mine' | 'claimable';

function toggle<T>(list: T[], v: T): T[] {
  return list.includes(v) ? list.filter((x) => x !== v) : [...list, v];
}

export default function MyTasks() {
  const [tab, setTab] = useState<TabKey>('mine');
  const [statuses, setStatuses] = useState<TaskStatus[]>(activeStatuses);
  const [priorities, setPriorities] = useState<TaskPriority[]>([]);
  const [items, setItems] = useState<TaskSummary[]>([]);
  const [cursor, setCursor] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string>();

  const load = useCallback(
    async (after = '') => {
      setLoading(true);
      setError(undefined);
      try {
        const page = { cursor: after, page_size: 20 };
        const res: ListTasksResp =
          tab === 'mine'
            ? await taskApi.listMine({ statuses, priorities, page })
            : await taskApi.listClaimable({ priorities, page });
        setItems((prev) => (after ? [...prev, ...res.items] : res.items));
        setCursor(res.next_cursor);
      } catch (e) {
        setError(e instanceof ApiError ? `${e.code}: ${e.message}` : String(e));
      } finally {
        setLoading(false);
      }
    },
    [tab, statuses, priorities],
  );

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <>
      <Tabs
        activeKey={tab}
        onChange={(k) => setTab(k as TabKey)}
        items={[
          { key: 'mine', label: '我的任务' },
          { key: 'claimable', label: '可认领' },
        ]}
        tabBarExtraContent={
          <Button icon={<ReloadOutlined />} onClick={() => void load()} loading={loading}>
            刷新
          </Button>
        }
      />
      <Space direction="vertical" style={{ marginBottom: 16 }}>
        {tab === 'mine' && (
          <Space wrap>
            <Typography.Text type="secondary">状态</Typography.Text>
            {statusOptions.map((s) => (
              <Tag.CheckableTag key={s} checked={statuses.includes(s)} onChange={() => setStatuses(toggle(statuses, s))}>
                {taskStatusLabel[s]}
              </Tag.CheckableTag>
            ))}
          </Space>
        )}
        <Space wrap>
          <Typography.Text type="secondary">优先级</Typography.Text>
          {priorityOptions.map((p) => (
            <Tag.CheckableTag key={p} checked={priorities.includes(p)} onChange={() => setPriorities(toggle(priorities, p))}>
              {priorityLabel[p]}
            </Tag.CheckableTag>
          ))}
        </Space>
      </Space>
      {error && <Alert type="error" message={error} showIcon style={{ marginBottom: 16 }} />}
      <List
        bordered
        loading={loading && items.length === 0}
        dataSource={items}
        locale={{ emptyText: <Empty description={tab === 'mine' ? '没有任务' : '没有可认领的任务'} /> }}
        renderItem={(t) => (
          <List.Item style={{ borderLeft: `4px solid ${priorityColor[t.priority]}` }}>
            <List.Item.Meta
              title={<Link to={`/tasks/${t.id}`}>{t.title}</Link>}
              description={t.subtitle || t.task_type_key}
            />
            <Space size="large">
              <Tag color={priorityColor[t.priority]}>{priorityLabel[t.priority]}</Tag>
              <Tag color={taskStatusColor[t.status]}>{taskStatusLabel[t.status]}</Tag>
              <Typography.Text type="secondary">{t.assignee_id || '—'}</Typography.Text>
              <Typography.Text type="secondary">截止 {fmtTime(t.due_at)}</Typography.Text>
              <Typography.Text type="secondary">更新 {fmtTime(t.updated_at)}</Typography.Text>
            </Space>
          </List.Item>
        )}
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
