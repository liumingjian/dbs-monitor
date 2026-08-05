import { Outlet, createRootRoute } from '@tanstack/react-router'
import { Layout, Typography } from 'antd'

export const rootRoute = createRootRoute({
  component: () => (
    <Layout style={{ minHeight: '100vh' }}>
      <Layout.Header><Typography.Title level={3} style={{ color: 'white', margin: '14px 0' }}>DBS Monitor</Typography.Title></Layout.Header>
      <Layout.Content style={{ padding: 24, maxWidth: 1200, width: '100%', margin: '0 auto' }}><Outlet /></Layout.Content>
    </Layout>
  ),
})
