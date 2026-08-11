import { Link } from '@tanstack/react-router'
import { Tabs, Typography } from 'antd'

type AlertSettingsPage = 'notifications' | 'contacts' | 'policies' | 'maintenance'

export function AlertSettingsHeader({ active }: { active: AlertSettingsPage }) {
  return (
    <div>
      <Typography.Title level={2} style={{ marginBottom: 8 }}>告警设置</Typography.Title>
      <Tabs activeKey={active} items={[
        { key: 'notifications', label: <Link to="/alert-settings/notifications">通知渠道</Link> },
        { key: 'contacts', label: <Link to="/alert-settings/contacts">联系人</Link> },
        { key: 'policies', label: <Link to="/alert-settings/policies">通知策略</Link> },
        { key: 'maintenance', label: <Link to="/alert-settings/maintenance-windows">维护窗口</Link> },
      ]} />
    </div>
  )
}
