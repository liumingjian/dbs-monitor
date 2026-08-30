import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Link, RouterProvider, createRoute, createRouter, redirect } from '@tanstack/react-router'
import { Button, ConfigProvider, Result } from 'antd'
import React from 'react'
import ReactDOM from 'react-dom/client'
import { alertsRoute } from './routes/alerts'
import { instanceAlertDetailRoute } from './routes/instances.$id/alerts.$alertId'
import { instanceAlertsRoute } from './routes/instances.$id/alerts'
import { notificationSettingsRoute } from './routes/alert-settings/notifications'
import { contactSettingsRoute } from './routes/alert-settings/contacts'
import { maintenanceNewRoute, maintenanceSettingsRoute } from './routes/alert-settings/maintenance'
import { policySettingsRoute } from './routes/alert-settings/policies'
import { instanceRoute } from './routes/instances.$id'
import { performanceEventDetailRoute } from './routes/instances.$id/performanceEventDetail'
import { performanceEventsRoute } from './routes/instances.$id/performanceEventsPage'
import { collectionManagementRoute } from './routes/instances.$id/collection'
import { standardMonitoringRoute } from './routes/instances.$id/monitoring'
import { alertRulesRoute } from './routes/instances.$id/alerts/rules'
import { instanceSettingsRoute } from './routes/instances.$id/settings'
import { longQuerySamplesRoute } from './routes/instances.$id/longQuerySamples'
import { queryStatisticsRoute } from './routes/instances.$id/queryStatisticsPage'
import { sessionsRoute } from './routes/instances.$id/sessions'
import { instancesRoute } from './routes/instances'
import { loginRoute } from './routes/login'
import { rootRoute } from './routes/root'
import { usersRoute } from './routes/users'
// Carbon 令牌层。全应用唯一的 Sass 入口，必须只 import 一次；见 styles/index.scss 顶部。
// 排在 styles.css 之前：迁移期间旧页面仍由 styles.css 决定外观。
import './styles/index.scss'
import './styles.css'

// `/` matched nothing, so the entry URL rendered a bare English "Not Found".
const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  beforeLoad: () => { throw redirect({ to: '/instances' }) },
})

const routeTree = rootRoute.addChildren([
  indexRoute,
  loginRoute,
  alertsRoute,
  instancesRoute,
  instanceRoute,
  standardMonitoringRoute,
  performanceEventsRoute,
  performanceEventDetailRoute,
  instanceAlertsRoute,
  instanceAlertDetailRoute,
  alertRulesRoute,
  collectionManagementRoute,
  instanceSettingsRoute,
  sessionsRoute,
  longQuerySamplesRoute,
  queryStatisticsRoute,
  usersRoute,
  notificationSettingsRoute,
  contactSettingsRoute,
  policySettingsRoute,
  maintenanceSettingsRoute,
  maintenanceNewRoute,
])
const router = createRouter({ routeTree, defaultPreload: 'intent', defaultNotFoundComponent: () => <Result
    status="404"
    title="页面不存在"
    subTitle="该地址没有对应的页面，可能是链接过期或输入有误。"
    extra={<Link to="/instances"><Button type="primary">返回实例列表</Button></Link>}
  /> })
const queryClient = new QueryClient()

declare module '@tanstack/react-router' {
  interface Register { router: typeof router }
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode><ConfigProvider><QueryClientProvider client={queryClient}><RouterProvider router={router} /></QueryClientProvider></ConfigProvider></React.StrictMode>,
)
