import { Link } from '@tanstack/react-router'
import { Badge, Tabs, Typography } from 'antd'
import { $api } from '../../api/client'

type AlertSettingsPage = 'notifications' | 'contacts' | 'policies' | 'maintenance'

export function AlertSettingsHeader({ active }: { active: AlertSettingsPage }) {
  const failureQuery = $api.useQuery(
    'get',
    '/api/v1/notification-channels/failures',
    {},
    { refetchInterval: 15_000 },
  )

  return (
    <div>
      <Typography.Title level={2} style={{ marginBottom: 8 }}>告警设置</Typography.Title>
      <Tabs activeKey={active} items={[
        {
          key: 'notifications',
          label: (
            <Link to="/alert-settings/notifications">
              <NotificationChannelsLabel hasFailures={failureQuery.data?.has_failures === true} />
            </Link>
          ),
        },
        { key: 'contacts', label: <Link to="/alert-settings/contacts">联系人</Link> },
        { key: 'policies', label: <Link to="/alert-settings/policies">通知策略</Link> },
        { key: 'maintenance', label: <Link to="/alert-settings/maintenance-windows">维护窗口</Link> },
      ]} />
    </div>
  )
}

export function NotificationChannelsLabel({ hasFailures }: { hasFailures: boolean }) {
  return (
    <Badge dot={hasFailures} status="error" offset={[5, 0]}>
      <span title={hasFailures ? '通知渠道存在未清除失败' : undefined}>通知渠道</span>
    </Badge>
  )
}
