import { UserOutlined } from '@ant-design/icons';
import { Alert, Grid, Layout, Menu, Select, Space, Tag, Typography } from 'antd';
import { Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import { useIdentity } from './identity';
import MyTasks from './pages/MyTasks';
import TaskDetail from './pages/TaskDetail';
import TaskTypeDetail from './pages/TaskTypeDetail';
import TaskTypeEditor from './pages/TaskTypeEditor';
import TaskTypes from './pages/TaskTypes';
import Teams from './pages/Teams';

const menu = [
  { key: '/tasks', label: '任务工作台' },
  { key: '/config/task-types', label: '任务类型' },
  { key: '/config/teams', label: '团队' },
];

export default function App() {
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const { user, users, setUser, error } = useIdentity();
  const screens = Grid.useBreakpoint();
  const selected = menu.find((m) => pathname.startsWith(m.key))?.key ?? '/tasks';

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Layout.Header style={{ display: 'flex', alignItems: 'center', gap: screens.md ? 24 : 8, paddingInline: screens.md ? 50 : 12 }}>
        {screens.md && (
          <Typography.Title level={4} style={{ color: '#fff', margin: 0, whiteSpace: 'nowrap' }}>
            AI Workbench
          </Typography.Title>
        )}
        <Menu theme="dark" mode="horizontal" selectedKeys={[selected]} items={menu} onClick={(e) => navigate(e.key)} style={{ flex: 1, minWidth: 0 }} />
        <Space>
          {screens.md && <Tag color="orange">DEV</Tag>}
          <Select
            value={user || undefined}
            placeholder="切换身份"
            style={{ width: screens.md ? 280 : 140 }}
            popupMatchSelectWidth={false}
            suffixIcon={<UserOutlined />}
            onChange={setUser}
            options={users.map((u) => ({
              value: u.id,
              label: `${u.id}${[...u.roles, ...u.groups, ...u.permissions].length ? `（${[...u.roles, ...u.groups, ...u.permissions].join('、')}）` : ''}`,
            }))}
          />
        </Space>
      </Layout.Header>
      <Layout.Content style={{ padding: screens.md ? 24 : 12, maxWidth: 1400, width: '100%', margin: '0 auto' }}>
        {error && <Alert type="error" showIcon message={`无法读取开发身份（cmd/devgw 是否已启动？）：${error}`} style={{ marginBottom: 16 }} />}
        {/* 切换身份后整页重新加载数据 */}
        <Routes key={user}>
          <Route path="/" element={<Navigate to="/tasks" replace />} />
          <Route path="/tasks" element={<MyTasks />} />
          <Route path="/tasks/:id" element={<TaskDetail />} />
          <Route path="/config/task-types" element={<TaskTypes />} />
          <Route path="/config/task-types/:key" element={<TaskTypeDetail />} />
          <Route path="/config/task-types/:key/edit" element={<TaskTypeEditor />} />
          <Route path="/config/teams" element={<Teams />} />
          <Route path="*" element={<Navigate to="/tasks" replace />} />
        </Routes>
      </Layout.Content>
    </Layout>
  );
}
