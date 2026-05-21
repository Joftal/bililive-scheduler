import { Routes, Route, Navigate } from 'react-router-dom';
import { Layout, Menu, Typography } from 'antd';
import { DashboardOutlined, UnorderedListOutlined, PlusOutlined } from '@ant-design/icons';
import { useNavigate, useLocation } from 'react-router-dom';
import Dashboard from './pages/Dashboard';
import TaskList from './pages/TaskList';
import TaskForm from './pages/TaskForm';

const { Header, Content } = Layout;

export default function App() {
  const navigate = useNavigate();
  const location = useLocation();

  const menuItems = [
    { key: '/', icon: <DashboardOutlined />, label: '仪表盘' },
    { key: '/tasks', icon: <UnorderedListOutlined />, label: '任务列表' },
    { key: '/tasks/new', icon: <PlusOutlined />, label: '新建任务' },
  ];

  const selectedKey = location.pathname === '/' ? '/' :
    location.pathname.startsWith('/tasks/new') ? '/tasks/new' :
    location.pathname.startsWith('/tasks') ? '/tasks' : '/';

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Header style={{ display: 'flex', alignItems: 'center', padding: '0 24px' }}>
        <Typography.Text strong style={{ color: '#fff', fontSize: 18, marginRight: 48 }}>
          Bililive Scheduler
        </Typography.Text>
        <Menu
          theme="dark"
          mode="horizontal"
          selectedKeys={[selectedKey]}
          items={menuItems}
          onClick={({ key }) => navigate(key)}
          style={{ flex: 1 }}
        />
      </Header>
      <Content style={{ padding: '24px', background: '#f5f5f5' }}>
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/tasks" element={<TaskList />} />
          <Route path="/tasks/new" element={<TaskForm />} />
          <Route path="/tasks/:id/edit" element={<TaskForm />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </Content>
    </Layout>
  );
}
