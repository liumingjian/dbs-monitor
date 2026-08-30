import { Button } from '@carbon/react'
import { Link, createRoute } from '@tanstack/react-router'
import { useMemo } from 'react'
import { $api } from '../../api/client'
import { Icon } from '../../primitives/Icon'
import { AlertObservationLists, InvalidAlertSearch } from '../alerts'
import { parseAlertListSearch, type AlertListSearch } from '../alerts/search'
import { rootRoute } from '../root'
import { defaultTimeRange } from './timeRange'
import { WorkbenchHeader } from './workbench'
import './instanceAlerts.css'

export const instanceAlertsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id/alerts',
  validateSearch: (search): AlertListSearch | { error: string } => parseAlertListSearch(search),
  component: InstanceAlertsPage,
})

/// 实例范围内的告警流。与全局告警是同一个组件，只是把 `instance_id` 钉死在这一台实例上，
/// 并且不再出自己的 `h1` —— 页头已经由实例工作台的页签条给了。
function InstanceAlertsPage() {
  const { id } = instanceAlertsRoute.useParams()
  const search = instanceAlertsRoute.useSearch()
  const navigate = instanceAlertsRoute.useNavigate()
  const instance = $api.useQuery('get', '/api/v1/instances/{id}', { params: { path: { id } } })
  const includePaused = 'error' in search ? false : search.include_paused

  // `as` 槽只收组件，不能顺带把路由属性交出去，所以每个去处包成一个「已经知道自己去哪儿」
  // 的组件，并用 useMemo 固定身份（web/CLAUDE.md 先例）。
  const links = useMemo(() => ({
    current: (props: object) => <Link {...props} to="/instances/$id/alerts" params={{ id }} search={{ tab: 'current' as const, include_paused: includePaused, page: 1 }} />,
    history: (props: object) => <Link {...props} to="/instances/$id/alerts" params={{ id }} search={{ tab: 'history' as const, include_paused: includePaused, page: 1 }} />,
    rules: (props: object) => <Link {...props} to="/instances/$id/alerts/rules" params={{ id }} />,
  }), [id, includePaused])

  if ('error' in search) {
    return <InvalidAlertSearch
      message={search.error}
      reset={<Link className="cds--link" to="/instances/$id/alerts" params={{ id }} search={{ tab: 'current', include_paused: false }}>使用默认筛选</Link>}
    />
  }

  return <div className="instance-alerts">
    <WorkbenchHeader id={id} instanceName={instance.data?.name} activeKey="alerts" search={defaultTimeRange()} />
    <AlertObservationLists
      search={{ ...search, instance_id: id }}
      onSearchChange={(next) => void navigate({ search: { ...next, instance_id: undefined } })}
      tabLinks={links}
      action={<Button as={links.rules} kind="tertiary" size="md" renderIcon={RulesIcon}>告警规则</Button>}
    />
  </div>
}

function RulesIcon() {
  return <Icon name="settings" />
}
