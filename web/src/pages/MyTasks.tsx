// 任务工作台：我的任务 / 待认领 / 我经手的，按优先级分区的卡片网格。
//
// 每个优先级是一个分区，各自查询、各自游标分页（「查看更多」），分区标题的计数来自后端第一页返回的
// counts（与筛选条件相同、不受分页影响），所以计数是准确的。没有任务的分区不显示。
// 卡片上的操作是同步生效的，提交后立即重新加载该分区（任务可能离开当前分区，如升级后移到「我经手的」）。
import { ReloadOutlined } from '@ant-design/icons';
import { Alert, Button, Col, Empty, Row, Skeleton, Space, Tabs, Tag, Typography } from 'antd';
import { useCallback, useEffect, useState } from 'react';
import { ApiError } from '../api/client';
import { taskApi } from '../api/services';
import type { ListTasksResp, Scope, TaskCount, TaskPriority, TaskStatus, TaskSummary } from '../api/types';
import { TaskBoardCard } from '../components/TaskBoardCard';
import { priorityColor, taskStatusLabel } from '../format';
import { useIdentity } from '../identity';

const statusOptions: TaskStatus[] = ['TASK_STATUS_PENDING', 'TASK_STATUS_PROCESSING', 'TASK_STATUS_SNOOZED', 'TASK_STATUS_SOLVED'];
const activeStatuses: TaskStatus[] = ['TASK_STATUS_PENDING', 'TASK_STATUS_PROCESSING', 'TASK_STATUS_SNOOZED'];
const priorities: TaskPriority[] = ['TASK_PRIORITY_P0', 'TASK_PRIORITY_P1', 'TASK_PRIORITY_P2', 'TASK_PRIORITY_P3'];
/** 分区每页 9 张：三列网格正好三行。 */
const PAGE_SIZE = 9;
type TabKey = Scope;

interface Section {
  items: TaskSummary[];
  cursor: string;
  counts: TaskCount[];
  loading: boolean;
  error?: string;
}

const emptySection: Section = { items: [], cursor: '', counts: [], loading: true };

function toggle<T>(list: T[], v: T): T[] {
  return list.includes(v) ? list.filter((x) => x !== v) : [...list, v];
}

function total(counts: TaskCount[]): number {
  return counts.reduce((n, c) => n + Number(c.count), 0);
}

export default function MyTasks() {
  const { user } = useIdentity();
  const [tab, setTab] = useState<TabKey>('mine');
  const [statuses, setStatuses] = useState<TaskStatus[]>(activeStatuses);
  const [shown, setShown] = useState<TaskPriority[]>([]); // 空 = 全部优先级
  const [sections, setSections] = useState<Partial<Record<TaskPriority, Section>>>({});

  const fetchPage = useCallback(
    (priority: TaskPriority, cursor: string): Promise<ListTasksResp> => {
      const page = { cursor, page_size: PAGE_SIZE };
      // 待认领只有待认领状态；我经手的显示全部状态（含已完结），都不按状态筛选
      return taskApi.list({ scope: tab, statuses: tab === 'mine' ? statuses : [], priorities: [priority], page });
    },
    [tab, statuses],
  );

  const loadSection = useCallback(
    async (priority: TaskPriority, cursor = '') => {
      setSections((prev) => ({ ...prev, [priority]: { ...(prev[priority] ?? emptySection), loading: true, error: undefined } }));
      try {
        const res = await fetchPage(priority, cursor);
        setSections((prev) => {
          const old = prev[priority] ?? emptySection;
          return {
            ...prev,
            [priority]: {
              items: cursor ? [...old.items, ...res.items] : res.items,
              cursor: res.next_cursor,
              counts: cursor ? old.counts : res.counts ?? [],
              loading: false,
            },
          };
        });
      } catch (e) {
        const error = e instanceof ApiError ? `${e.code}: ${e.message}` : String(e);
        setSections((prev) => ({ ...prev, [priority]: { ...(prev[priority] ?? emptySection), loading: false, error } }));
      }
    },
    [fetchPage],
  );

  const visible = shown.length > 0 ? priorities.filter((p) => shown.includes(p)) : priorities;

  const loadAll = useCallback(() => {
    setSections({});
    visible.forEach((p) => void loadSection(p));
  }, [loadSection, shown]);

  useEffect(() => {
    loadAll();
  }, [loadAll, user]);

  // 卡片上的操作完成后，重新加载该任务所在的分区。
  const onChanged = (priority: TaskPriority) => void loadSection(priority);

  const loaded = visible.map((p) => sections[p]);
  const allLoaded = loaded.every((s) => s && !s.loading);
  const nothing = allLoaded && loaded.every((s) => s && !s.error && s.items.length === 0);

  return (
    <>
      <Tabs
        activeKey={tab}
        onChange={(k) => setTab(k as TabKey)}
        items={[
          { key: 'mine', label: '我的任务' },
          { key: 'claimable', label: '待认领' },
          { key: 'involved', label: '我经手的' },
        ]}
        tabBarExtraContent={
          <Button icon={<ReloadOutlined />} onClick={loadAll} loading={!allLoaded}>
            刷新
          </Button>
        }
      />
      <Space direction="vertical" style={{ marginBottom: 8 }}>
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
          {priorities.map((p) => (
            <Tag.CheckableTag key={p} checked={shown.includes(p)} onChange={() => setShown(toggle(shown, p))}>
              {p.replace('TASK_PRIORITY_', '')}
            </Tag.CheckableTag>
          ))}
        </Space>
      </Space>

      {nothing && <Empty style={{ marginTop: 48 }} description={{ mine: '没有任务', claimable: '没有待认领的任务', involved: '没有经手过、现在不在你名下的任务' }[tab]} />}

      {visible.map((p) => {
        const s = sections[p];
        if (!s) return null;
        if (s.loading && s.items.length === 0) return <Skeleton key={p} active paragraph={{ rows: 3 }} style={{ marginTop: 24 }} />;
        if (!s.error && s.items.length === 0) return null;
        const n = total(s.counts);
        return (
          <section key={p} style={{ marginTop: 24 }} aria-label={`${p.replace('TASK_PRIORITY_', '')} 任务`}>
            <Space size={12} align="baseline" style={{ marginBottom: 12 }}>
              <Typography.Title level={5} style={{ margin: 0, color: priorityColor[p] }}>
                {p.replace('TASK_PRIORITY_', '')}
              </Typography.Title>
              <Typography.Text strong>
                {tab === 'claimable' ? `${n} 个待认领` : s.counts.map((c) => `${taskStatusLabel[c.status]} ${c.count}`).join(' · ')}
              </Typography.Text>
            </Space>
            {s.error && <Alert type="error" showIcon message={s.error} style={{ marginBottom: 12 }} />}
            <Row gutter={[16, 16]}>
              {s.items.map((t) => (
                <Col key={t.id} xs={24} md={12} xl={8}>
                  <TaskBoardCard task={t} onChanged={() => onChanged(p)} />
                </Col>
              ))}
            </Row>
            {s.cursor && (
              <div style={{ textAlign: 'center', marginTop: 12 }}>
                <Button type="link" loading={s.loading} onClick={() => void loadSection(p, s.cursor)}>
                  查看更多（还有 {Math.max(0, n - s.items.length)} 个）
                </Button>
              </div>
            )}
          </section>
        );
      })}
    </>
  );
}
