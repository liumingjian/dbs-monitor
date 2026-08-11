import { createRoute } from '@tanstack/react-router'
import { Empty, Space } from 'antd'
import { rootRoute } from '../root'
import { AlertSettingsHeader } from './header'

export const maintenanceSettingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/alert-settings/maintenance-windows',
  component: MaintenanceSettingsPage,
})

function MaintenanceSettingsPage() {
  return (
    <Space orientation="vertical" size="large" style={{ width: '100%' }}>
      <AlertSettingsHeader active="maintenance" />
      <Empty description="暂无维护窗口" />
    </Space>
  )
}
