import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider, createRouter } from '@tanstack/react-router'
import { ConfigProvider } from 'antd'
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
import './styles.css'

const routeTree = rootRoute.addChildren([
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
const router = createRouter({ routeTree, defaultPreload: 'intent' })
const queryClient = new QueryClient()

declare module '@tanstack/react-router' {
  interface Register { router: typeof router }
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode><ConfigProvider><QueryClientProvider client={queryClient}><RouterProvider router={router} /></QueryClientProvider></ConfigProvider></React.StrictMode>,
)
