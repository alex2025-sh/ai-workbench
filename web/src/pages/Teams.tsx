// 团队 /config/teams：节点的处理人可以是团队。成员角色只有两种：分配者（也是执行者）与执行者。
//   - 认领模式：通知发到团队的 Lark 群（没配群时私发给全部成员），成员在「团队队列」中认领；
//   - 分配模式：通知私发给分配者，分配者在「团队队列」中指定执行者。
// 成员变化即时生效，不需要重新发布任务类型。维护团队需要 team.admin 权限。
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons';
import { Alert, App as AntApp, Button, Drawer, Form, Input, Select, Space, Table, Tag, Tooltip, Typography } from 'antd';
import { useCallback, useEffect, useState } from 'react';
import { ApiError } from '../api/client';
import { teamApi } from '../api/services';
import type { Team, TeamMember } from '../api/types';
import { useIdentity } from '../identity';

const empty: Team = { key: '', name: '', chat_id: '', members: [{ user_id: '', role: 'assigner' }], updated_by: '' };

export default function Teams() {
  const { can } = useIdentity();
  const { message } = AntApp.useApp();
  const [items, setItems] = useState<Team[]>([]);
  const [error, setError] = useState<string>();
  const [editing, setEditing] = useState<{ team: Team; isNew: boolean }>();
  const [saving, setSaving] = useState(false);

  const load = useCallback(() => {
    teamApi.list().then(
      (r) => setItems(r.items),
      (e) => setError(e instanceof ApiError ? `${e.code}: ${e.message}` : String(e)),
    );
  }, []);
  useEffect(load, [load]);

  const admin = can('team.admin');
  const t = editing?.team;
  const setT = (patch: Partial<Team>) => editing && setEditing({ ...editing, team: { ...editing.team, ...patch } });
  const setMember = (i: number, patch: Partial<TeamMember>) => setT({ members: t!.members.map((m, n) => (n === i ? { ...m, ...patch } : m)) });

  return (
    <>
      <Space style={{ marginBottom: 16, width: '100%', justifyContent: 'space-between' }}>
        <Typography.Text type="secondary">团队用作节点的处理人：成员认领，或由分配者指定执行者。</Typography.Text>
        <Tooltip title={admin ? '' : '需要 team.admin 权限'}>
          <Button type="primary" icon={<PlusOutlined />} disabled={!admin} onClick={() => setEditing({ team: { ...empty, members: [...empty.members] }, isNew: true })}>
            新建团队
          </Button>
        </Tooltip>
      </Space>
      {error && <Alert type="error" showIcon message={error} style={{ marginBottom: 16 }} />}
      <Table<Team>
        rowKey="key"
        dataSource={items}
        pagination={false}
        columns={[
          { title: '团队', render: (_, x) => <Space direction="vertical" size={0}><b>{x.name}</b><Typography.Text type="secondary" code>{x.key}</Typography.Text></Space> },
          { title: '分配者', render: (_, x) => <Space size={[4, 4]} wrap>{x.members.filter((m) => m.role === 'assigner').map((m) => <Tag color="purple" key={m.user_id}>{m.user_id}</Tag>)}</Space> },
          { title: '执行者', render: (_, x) => <Space size={[4, 4]} wrap>{x.members.filter((m) => m.role === 'executor').map((m) => <Tag key={m.user_id}>{m.user_id}</Tag>)}</Space> },
          { title: 'Lark 群', dataIndex: 'chat_id', render: (v: string) => v || <Typography.Text type="secondary">未配置（私发给成员）</Typography.Text> },
          { title: '', width: 80, render: (_, x) => admin && <a onClick={() => setEditing({ team: { ...x, members: [...x.members] }, isNew: false })}>编辑</a> },
        ]}
      />
      <Drawer
        title={editing?.isNew ? '新建团队' : `编辑团队 ${t?.name}`}
        width={560}
        open={!!editing}
        onClose={() => setEditing(undefined)}
        extra={
          <Button
            type="primary"
            loading={saving}
            onClick={async () => {
              setSaving(true);
              try {
                await teamApi.save({ key: t!.key, name: t!.name, chat_id: t!.chat_id, members: t!.members.filter((m) => m.user_id) });
                message.success('已保存');
                setEditing(undefined);
                load();
              } catch (e) {
                message.error((e as Error).message);
              } finally {
                setSaving(false);
              }
            }}
          >
            保存
          </Button>
        }
      >
        {t && (
          <Form layout="vertical">
            <Form.Item label="标识（key）" required>
              <Input value={t.key} disabled={!editing?.isNew} onChange={(e) => setT({ key: e.target.value.trim() })} placeholder="如 vip_review" />
            </Form.Item>
            <Form.Item label="名称" required>
              <Input value={t.name} onChange={(e) => setT({ name: e.target.value })} placeholder="如 VIP 审核小组" />
            </Form.Item>
            <Form.Item label="Lark 群 chat_id" tooltip="认领模式的通知发到这个群；不填则私发给全部成员">
              <Input value={t.chat_id} onChange={(e) => setT({ chat_id: e.target.value.trim() })} placeholder="oc_xxx" />
            </Form.Item>
            <Form.Item label="成员" required tooltip="分配者可以把任务分配给成员，也可以自己处理">
              <Space direction="vertical" style={{ width: '100%' }}>
                {t.members.map((m, i) => (
                  <Space.Compact key={i} style={{ width: '100%' }}>
                    <Input value={m.user_id} onChange={(e) => setMember(i, { user_id: e.target.value.trim() })} placeholder="成员 email（线上经 BSP 选人）" />
                    <Select
                      style={{ width: 120 }}
                      value={m.role}
                      onChange={(v) => setMember(i, { role: v })}
                      options={[
                        { value: 'assigner', label: '分配者' },
                        { value: 'executor', label: '执行者' },
                      ]}
                    />
                    <Button icon={<DeleteOutlined />} onClick={() => setT({ members: t.members.filter((_, n) => n !== i) })} aria-label="删除成员" />
                  </Space.Compact>
                ))}
                <Button icon={<PlusOutlined />} onClick={() => setT({ members: [...t.members, { user_id: '', role: 'executor' }] })}>
                  添加成员
                </Button>
              </Space>
            </Form.Item>
          </Form>
        )}
      </Drawer>
    </>
  );
}
