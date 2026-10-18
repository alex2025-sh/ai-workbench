import { UserOutlined } from '@ant-design/icons';
import { Alert, Layout, Menu, Select, Space, Tag, Typography } from 'antd';
import { Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import { useIdentity } from './identity';
import MyTasks from './pages/MyTasks';
import RunDetail from './pages/RunDetail';
import Runs from './pages/Runs';
import TaskDetail from './pages/TaskDetail';
import TaskTypeDetail from './pages/TaskTypeDetail';
import TaskTypeEditor from './pages/TaskTypeEditor';
import TaskTypes from './pages/TaskTypes';

const menu = [
  { key: '/tasks', label: '我的任务' },
  { key: '/runs', label: '运行监控' },
  { key: '/config/task-types', label: '任务类型' },
];

export default function App() {
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const { user, users, setUser, error } = useIdentity();
  const selected = menu.find((m) => pathname.startsWith(m.key))?.key ?? '/tasks';

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Layout.Header style={{ display: 'flex', alignItems: 'center', gap: 24 }}>
        <Typography.Title level={4} style={{ color: '#fff', margin: 0, whiteSpace: 'nowrap' }}>
          AI Workbench
        </Typography.Title>
        <Menu theme="dark" mode="horizontal" selectedKeys={[selected]} items={menu} onClick={(e) => navigate(e.key)} style={{ flex: 1 }} />
        <Space>
          <Tag color="orange">DEV</Tag>
          <Select
            value={user || undefined}
            placeholder="切换身份"
            style={{ width: 280 }}
            suffixIcon={<UserOutlined />}
            onChange={setUser}
            options={users.map((u) => ({
              value: u.id,
              label: `${u.id}${[...u.roles, ...u.groups, ...u.permissions].length ? `（${[...u.roles, ...u.groups, ...u.permissions].join('、')}）` : ''}`,
            }))}
          />
        </Space>
      </Layout.Header>
      <Layout.Content style={{ padding: 24, maxWidth: 1400, width: '100%', margin: '0 auto' }}>
        {error && <Alert type="error" showIcon message={`无法读取开发身份（cmd/devgw 是否已启动？）：${error}`} style={{ marginBottom: 16 }} />}
        {/* 切换身份后整页重新加载数据 */}
        <Routes key={user}>
          <Route path="/" element={<Navigate to="/tasks" replace />} />
          <Route path="/tasks" element={<MyTasks />} />
          <Route path="/tasks/:id" element={<TaskDetail />} />
          <Route path="/runs" element={<Runs />} />
          <Route path="/runs/:id" element={<RunDetail />} />
          <Route path="/config/task-types" element={<TaskTypes />} />
          <Route path="/config/task-types/:key" element={<TaskTypeDetail />} />
          <Route path="/config/task-types/:key/edit" element={<TaskTypeEditor />} />
          <Route path="*" element={<Navigate to="/tasks" replace />} />
        </Routes>
      </Layout.Content>
    </Layout>
  );
}
