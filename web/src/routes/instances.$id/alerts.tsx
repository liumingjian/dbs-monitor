import { Link, createRoute } from '@tanstack/react-router'
import { Alert, Button, Space } from 'antd'
import { $api } from '../../api/client'
import { AlertObservationLists } from '../alerts'
import { parseAlertListSearch, type AlertListSearch } from '../alerts/search'
import { defaultTimeRange } from './timeRange'
import { WorkbenchHeader } from './workbench'
import { rootRoute } from '../root'

export const instanceAlertsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id/alerts',
  validateSearch: (search): AlertListSearch | { error: string } => parseAlertListSearch(search),
  component: InstanceAlertsPage,
})

function InstanceAlertsPage() {
  const { id } = instanceAlertsRoute.useParams()
  const search = instanceAlertsRoute.useSearch()
  const navigate = instanceAlertsRoute.useNavigate()
  const instance = $api.useQuery('get', '/api/v1/instances/{id}', { params: { path: { id } } })

  if ('error' in search) {
    return <Alert
      type="error"
      showIcon
      title={search.error}
      action={<Link to="/instances/$id/alerts" params={{ id }} search={{ tab: 'current', include_paused: false }}><Button>使用默认筛选</Button></Link>}
    />
  }

  const scopedSearch = { ...search, instance_id: id }
  return <Space direction="vertical" size="large" style={{ width: '100%' }}>
    <WorkbenchHeader id={id} instanceName={instance.data?.name} activeKey="alerts" search={defaultTimeRange()} />
    <AlertObservationLists
      search={scopedSearch}
      onSearchChange={(next) => void navigate({ search: { ...next, instance_id: undefined } })}
    />
  </Space>
}
