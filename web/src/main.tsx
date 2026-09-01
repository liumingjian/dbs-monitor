import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider, createRouter } from '@tanstack/react-router'
import React from 'react'
import ReactDOM from 'react-dom/client'
import { alertsRoute } from './routes/alerts'
import { instanceAlertDetailRoute } from './routes/instances.$id/alerts.$alertId'
import { instanceAlertsRoute } from './routes/instances.$id/alerts'
import {
  alertSettingsRoute,
  contactSettingsRoute,
  maintenanceNewRoute,
  maintenanceSettingsRoute,
  notificationSettingsRoute,
  policySettingsRoute,
} from './routes/alert-settings'
import { instanceRoute } from './routes/instances.$id'
import { parseSearch, stringifySearch } from './routes/searchParams'
import { performanceEventDetailRoute } from './routes/instances.$id/performanceEventDetail'
import { performanceEventsRoute } from './routes/instances.$id/performanceEventsPage'
import { collectionManagementRoute } from './routes/instances.$id/collection'
import { standardMonitoringRoute } from './routes/instances.$id/monitoring'
import { alertRulesRoute } from './routes/instances.$id/alerts/rules'
import { instanceSettingsRoute } from './routes/instances.$id/settings'
import { longQuerySamplesRoute, queryStatisticsRoute, sessionsRoute } from './routes/instances.$id/sessions'
import { instancesRoute } from './routes/instances'
import { loginRoute } from './routes/login'
import { overviewRoute } from './routes/overview'
import { rootRoute } from './routes/root'
import { sqlInsightRoute } from './routes/sql-insight'
import { AppErrorBoundary, NotFoundPage, RouteErrorPage } from './routes/root/errorBoundary'
import { usersRoute } from './routes/users'
// Carbon 令牌层。全应用唯一的 Sass 入口，必须只 import 一次；见 styles/index.scss 顶部。
// 排在 styles.css 之前：令牌先落地，styles.css 里剩下的全局元素样式才引用得到它们。
import './styles/index.scss'
import './styles.css'

// 落地页是机群总览，地址就是 `/`（从前它只是一条跳到 `/instances` 的重定向）。
// `/instances` 原样保留：那个地址被人存过书签、发过给同事，不能因为改了落地页就失效。

const routeTree = rootRoute.addChildren([
  overviewRoute,
  loginRoute,
  alertsRoute,
  instancesRoute,
  sqlInsightRoute,
  instanceRoute,
  standardMonitoringRoute,
  performanceEventsRoute,
  performanceEventDetailRoute,
  instanceAlertsRoute,
  instanceAlertDetailRoute,
  alertRulesRoute,
  collectionManagementRoute,
  instanceSettingsRoute,
  // 会话与阻塞：合并后的多标签页面，加上两个旧子地址的重定向。
  sessionsRoute,
  longQuerySamplesRoute,
  queryStatisticsRoute,
  usersRoute,
  // 告警设置：合并后的多标签页面，加上四个旧地址的重定向。
  alertSettingsRoute,
  notificationSettingsRoute,
  contactSettingsRoute,
  policySettingsRoute,
  maintenanceSettingsRoute,
  maintenanceNewRoute,
])
// `defaultErrorComponent` 是路由级的错误边界：一个页面在渲染或取数时抛出异常，
// 它接住那一段路由，外框与导航都还在。`AppErrorBoundary` 是外面那一层，接住外框自己
// 和路由器初始化时抛出的异常 —— 那两类发生在路由匹配之外，没有它就是整页白屏。
const router = createRouter({
  routeTree,
  // 查询参数的编解码整台路由器共用一份：多选筛选写成重复键，地址因此是能发给同事的
  // 那种地址，也和服务端读同一批参数的写法一致（`web/src/routes/searchParams.ts`）。
  parseSearch,
  stringifySearch,
  defaultPreload: 'intent',
  defaultNotFoundComponent: NotFoundPage,
  defaultErrorComponent: RouteErrorPage,
})
const queryClient = new QueryClient()

declare module '@tanstack/react-router' {
  interface Register { router: typeof router }
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <AppErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </AppErrorBoundary>
  </React.StrictMode>,
)
