import { ClearOutlined, DashboardOutlined, PlusOutlined, SettingOutlined } from '@ant-design/icons'
import { Link, createRoute } from '@tanstack/react-router'
import { Alert, Button, Checkbox, Form, Input, InputNumber, Modal, Select, Space, Table, Tooltip, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useState } from 'react'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import { pollingIntervals } from '../../api/polling'
import type { components } from '../../api/schema'
import { Freshness } from '../../domain/Freshness'
import { HEALTH_STATUSES, HealthStatus } from '../../domain/HealthStatus'
import { SuppressionTags } from '../../domain/SuppressionTags'
import { defaultTimeRange } from '../instances.$id/timeRange'
import { rootRoute } from '../root'

type InstanceCreateInput = components['schemas']['InstanceCreateInput']
type Instance = components['schemas']['Instance']
type HealthStatusValue = components['schemas']['HealthStatus']
type AlertSeverity = components['schemas']['AlertSeverity']
export type OrthogonalFlag = 'NO_DATA' | 'MAINTENANCE' | 'RECENTLY_RECOVERED' | 'IGNORED' | 'CONFIGURATION_MISSING'

export type InstanceFilters = {
  statuses?: readonly HealthStatusValue[]
  flags?: readonly OrthogonalFlag[]
  alertSeverity?: AlertSeverity
  hasInfo?: boolean
  hasConfigurationMissing?: boolean
}

function assertNever(value: never): never {
  throw new Error(`unexpected instance projection value: ${value}`)
}

function healthRank(status: HealthStatusValue): number {
  switch (status) {
    case 'CRITICAL':
      return 5
    case 'WARNING':
      return 4
    case 'UNKNOWN':
      return 3
    case 'HEALTHY':
      return 2
    case 'PAUSED':
      return 1
    default:
      return assertNever(status)
  }
}

function hasFlag(instance: Instance, flag: OrthogonalFlag): boolean {
  switch (flag) {
    case 'NO_DATA':
      return instance.health.flags.no_data
    case 'MAINTENANCE':
      return instance.health.flags.in_maintenance
    case 'RECENTLY_RECOVERED':
      return instance.health.flags.recently_recovered
    case 'IGNORED':
      return instance.health.flags.ignored > 0
    case 'CONFIGURATION_MISSING':
      return instance.health.flags.configuration_missing > 0
    default:
      return assertNever(flag)
  }
}

function hasSeverity(instance: Instance, severity: AlertSeverity): boolean {
  switch (severity) {
    case 'critical':
      return instance.health.counts.critical > 0
    case 'warning':
      return instance.health.counts.warning > 0
    case 'info':
      return instance.health.counts.info > 0
    default:
      return assertNever(severity)
  }
}

export function filterAndSortInstances(instances: readonly Instance[], filters: InstanceFilters): Instance[] {
  return instances.filter((instance) => {
    if (filters.statuses?.length && !filters.statuses.includes(instance.health.status)) {
      return false
    }
    if (filters.flags?.length && !filters.flags.every((flag) => hasFlag(instance, flag))) {
      return false
    }
    if (filters.alertSeverity && !hasSeverity(instance, filters.alertSeverity)) {
      return false
    }
    if (filters.hasInfo && instance.health.counts.info === 0) {
      return false
    }
    if (filters.hasConfigurationMissing && instance.health.flags.configuration_missing === 0) {
      return false
    }
    return true
  }).sort(compareInstances)
}

function compareInstances(left: Instance, right: Instance): number {
  const healthDifference = healthRank(right.health.status) - healthRank(left.health.status)
  if (healthDifference !== 0) {
    return healthDifference
  }
  return left.name.localeCompare(right.name)
}

export const instancesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances',
  component: InstancesPage,
})

function InstancesPage() {
  const instancesQuery = $api.useQuery('get', '/api/v1/instances', {}, { refetchInterval: pollingIntervals.instances })
  const currentUserQuery = $api.useQuery('get', '/api/v1/me')
  const createInstanceMutation = $api.useMutation('post', '/api/v1/instances')
  const [createOpen, setCreateOpen] = useState(false)
  const [actionError, setActionError] = useState('')
  const [filters, setFilters] = useState<InstanceFilters>({})
  const [createForm] = Form.useForm<InstanceCreateInput>()
  const canCreate = currentUserQuery.data?.role === 'PLATFORM_ADMIN'
  const createDisabledReason = canCreate ? undefined : '需要平台管理员角色'
  const visibleInstances = filterAndSortInstances(instancesQuery.data ?? [], filters)

  function createInstance(values: InstanceCreateInput) {
    setActionError('')
    createInstanceMutation.mutate({ body: values }, {
      onSuccess: () => {
        setCreateOpen(false)
        createForm.resetFields()
        void instancesQuery.refetch()
      },
      onError: (failure) => setActionError(apiErrorMessage(failure, '创建实例失败，请检查连接信息')),
    })
  }

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Space style={{ width: '100%', justifyContent: 'space-between' }}>
        <Typography.Title level={2} style={{ margin: 0 }}>PostgreSQL 实例</Typography.Title>
        <Tooltip title={createDisabledReason}>
          <span>
            <Button type="primary" icon={<PlusOutlined />} disabled={!canCreate} onClick={() => setCreateOpen(true)}>
              新建实例
            </Button>
          </span>
        </Tooltip>
      </Space>
      {actionError && <Alert type="error" title={actionError} closable onClose={() => setActionError('')} />}
      <Space className="instance-filter-bar" wrap size="middle" aria-label="实例筛选">
        <Select
          aria-label="主状态"
          mode="multiple"
          placeholder="主状态"
          value={filters.statuses ? [...filters.statuses] : undefined}
          style={{ minWidth: 220 }}
          options={healthStatusOptions}
          onChange={(statuses) => setFilters((current) => ({ ...current, statuses }))}
        />
        <Select
          aria-label="正交标记"
          mode="multiple"
          placeholder="正交标记"
          value={filters.flags ? [...filters.flags] : undefined}
          style={{ minWidth: 250 }}
          options={orthogonalFlagOptions}
          onChange={(flags) => setFilters((current) => ({ ...current, flags }))}
        />
        <Select
          aria-label="至少一条该级告警"
          allowClear
          placeholder="至少一条该级告警"
          value={filters.alertSeverity}
          style={{ minWidth: 190 }}
          options={alertSeverityOptions}
          onChange={(alertSeverity) => setFilters((current) => ({ ...current, alertSeverity }))}
        />
        <Checkbox checked={filters.hasInfo === true} onChange={(event) => setFilters((current) => ({ ...current, hasInfo: event.target.checked }))}>
          存在 info
        </Checkbox>
        <Checkbox checked={filters.hasConfigurationMissing === true} onChange={(event) => setFilters((current) => ({ ...current, hasConfigurationMissing: event.target.checked }))}>
          存在配置缺失
        </Checkbox>
        <Button icon={<ClearOutlined />} onClick={() => setFilters({})}>清除筛选</Button>
        {instancesQuery.dataUpdatedAt > 0 && <Freshness dataUpdatedAt={instancesQuery.dataUpdatedAt} collectionInterval={30_000} />}
      </Space>
      <Table<Instance>
        loading={instancesQuery.isPending}
        rowKey="id"
        dataSource={visibleInstances}
        pagination={{ pageSize: 50, showSizeChanger: false }}
        scroll={{ x: 1180 }}
        columns={instanceColumns}
      />
      <Modal title="新建实例" open={createOpen} footer={null} destroyOnHidden onCancel={() => setCreateOpen(false)}>
        <Form<InstanceCreateInput> form={createForm} layout="vertical" onFinish={createInstance}>
          <Form.Item name="name" label="名称" rules={[{ required: true, whitespace: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="host" label="主机" rules={[{ required: true, whitespace: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="port" label="端口" initialValue={5432} rules={[{ required: true }]}>
            <InputNumber min={1} max={65535} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="database" label="数据库" rules={[{ required: true, whitespace: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="username" label="用户名" rules={[{ required: true, whitespace: true }]}>
            <Input autoComplete="off" />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true }]}>
            <Input type="password" autoComplete="new-password" />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={createInstanceMutation.isPending}>连接测试并创建</Button>
        </Form>
      </Modal>
    </Space>
  )
}

const orthogonalFlagOptions: { value: OrthogonalFlag; label: string }[] = [
  { value: 'NO_DATA', label: '无数据' },
  { value: 'MAINTENANCE', label: '维护中' },
  { value: 'RECENTLY_RECOVERED', label: '近期恢复' },
  { value: 'IGNORED', label: '已忽略' },
  { value: 'CONFIGURATION_MISSING', label: '配置缺失' },
]

const healthStatusOptions = HEALTH_STATUSES.map((value) => ({ value, label: healthLabel(value) }))

const alertSeverityOptions: { value: AlertSeverity; label: string }[] = [
  { value: 'critical', label: '严重告警' },
  { value: 'warning', label: '警告告警' },
  { value: 'info', label: 'Info 告警' },
]

function healthLabel(status: HealthStatusValue): string {
  switch (status) {
    case 'CRITICAL':
      return '严重'
    case 'WARNING':
      return '警告'
    case 'UNKNOWN':
      return '未知'
    case 'HEALTHY':
      return '正常'
    case 'PAUSED':
      return '已暂停'
    default:
      return assertNever(status)
  }
}

function agentStatusLabel(status: components['schemas']['InstanceAgentStatus']): string {
  switch (status) {
    case 'online':
      return '在线'
    case 'offline':
      return '离线'
    case 'not_installed':
      return '未安装'
    case 'permission_denied':
      return '权限不足'
    case 'error':
      return '异常'
    default:
      return assertNever(status)
  }
}

function attributionLabel(instance: Instance): string {
  const attribution = instance.health.attribution
  if (!attribution) return '无未恢复告警'
  return attribution.current_value === undefined ? attribution.rule_name : `${attribution.rule_name} (${attribution.current_value})`
}

function lastCollectedAtLabel(collectedAt: string | undefined): string {
  return collectedAt ? new Date(collectedAt).toLocaleString() : '尚无成功采集'
}

function freshnessLabel(seconds: number | undefined): string {
  if (seconds === undefined) return '未知'
  if (seconds < 60) return `${seconds} 秒前`
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`
  return `${Math.floor(seconds / 3600)} 小时前`
}

const instanceColumns: TableColumnsType<Instance> = [
  {
    title: <Tooltip title="最高未恢复告警级别">实例健康</Tooltip>,
    width: 390,
    render: (_, instance) => (
      <Space direction="vertical" size={4}>
        <Space>
          <HealthStatus status={instance.health.status} pausedAt={instance.collection_pause.updated_at} />
          <Typography.Text strong>{instance.name}</Typography.Text>
        </Space>
        <Typography.Text type="secondary">{attributionLabel(instance)}</Typography.Text>
        <Space size={4} wrap>
          <Typography.Text code>C{instance.health.counts.critical}</Typography.Text>
          <Typography.Text code>W{instance.health.counts.warning}</Typography.Text>
          <Typography.Text code>I{instance.health.counts.info}</Typography.Text>
          <SuppressionTags flags={instance.health.flags} />
        </Space>
      </Space>
    ),
  },
  { title: '地址', render: (_, instance) => `${instance.host}:${instance.port}` },
  { title: 'Agent 状态', render: (_, instance) => agentStatusLabel(instance.agent_status) },
  { title: '最近采集时间', render: (_, instance) => lastCollectedAtLabel(instance.last_collected_at) },
  { title: '数据新鲜度', render: (_, instance) => freshnessLabel(instance.data_freshness_seconds) },
  {
    title: '操作',
    render: (_, instance) => (
      <Space wrap>
        <Link to="/instances/$id" params={{ id: instance.id }} search={defaultTimeRange()}>
          <DashboardOutlined /> 总览
        </Link>
        <Link to="/instances/$id/settings" params={{ id: instance.id }}>
          <SettingOutlined /> 接入设置
        </Link>
      </Space>
    ),
  },
]
