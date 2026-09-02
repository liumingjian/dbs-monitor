import {
  Button,
  StructuredListBody,
  StructuredListCell,
  StructuredListRow,
  StructuredListWrapper,
} from '@carbon/react'
import { Link, createRoute } from '@tanstack/react-router'
import { useMemo } from 'react'
import type { ReactNode } from 'react'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import type { components } from '../../api/schema'
import { AlertStatus } from '../../domain/AlertStatus'
import { elapsedLabel } from '../../domain/Freshness'
import { MetricChart, metricUnavailability, type MetricChartSeries } from '../../domain/MetricChart'
import { AlertSuppressionTags } from '../../domain/SuppressionTags'
import { UnavailabilityBlock, unavailabilityHref } from '../../domain/UnavailabilityBlock'
import { DataGrid } from '../../primitives/DataGrid'
import type { DataGridColumn } from '../../primitives/DataGrid'
import { Icon } from '../../primitives/Icon'
import { NotificationBar } from '../../primitives/NotificationBar'
import { Panel } from '../../primitives/Panel'
import { SkeletonBlock } from '../../primitives/SkeletonBlock'
import { StatusBadge } from '../../primitives/StatusBadge'
import type { StatusTone } from '../../primitives/StatusBadge'
import { TruncatedText } from '../../primitives/TruncatedText'
import { rootRoute } from '../root'
import { severityLabel } from '../alerts'
import { alertMonitoringSearch } from '../alerts/search'
import { DispositionSection, TriggerSnapshotSection } from './alertEvidence'
import { isMetricID, type MetricID } from './metricOptions'
import './alertDetail.css'

type AlertDetail = components['schemas']['AlertDetail']
type Instance = components['schemas']['Instance']
type RuleVersionRecord = components['schemas']['AlertRuleVersionRecord']

export const instanceAlertDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/instances/$id/alerts/$alertId',
  component: InstanceAlertDetailPage,
})

function InstanceAlertDetailPage() {
  const { id, alertId } = instanceAlertDetailRoute.useParams()
  const detail = $api.useQuery('get', '/api/v1/alert-instances/{id}', { params: { path: { id: alertId } } })

  // 规范：加载态是骨架，不是整页转圈 —— 已经能确定的版式先看得见。
  if (detail.isPending) {
    return <div className="alert-detail">
      <SkeletonBlock lines={2} label="告警详情加载中" width="24rem" />
      <Panel loading title="告警概览" />
      <Panel loading title="触发指标" />
    </div>
  }
  if (detail.error || !detail.data) {
    return <div className="alert-detail">
      <NotificationBar tone="critical" title={apiErrorMessage(detail.error, '告警详情不可用')} />
      <Link
        className="cds--link alert-detail__back"
        to="/instances/$id/alerts"
        params={{ id }}
        search={{ tab: 'current', include_paused: false }}
      >返回告警列表</Link>
    </div>
  }
  return <AlertDetailContent
    detail={detail.data}
    routeInstanceID={id}
    onDispositionChanged={() => void detail.refetch()}
  />
}

function AlertDetailContent({ detail, routeInstanceID, onDispositionChanged }: {
  detail: AlertDetail
  routeInstanceID: string
  onDispositionChanged: () => void
}) {
  const metric = isMetricID(detail.metric_id) ? detail.metric_id : undefined
  const monitoringSearch = metric ? alertMonitoringSearch({
    metric_id: metric,
    first_triggered_at: detail.first_triggered_at,
    recovered_at: detail.recovered_at,
    updated_at: detail.updated_at,
  }) : undefined
  const currentMetricHref = monitoringSearch
    ? alertMonitoringHref(detail.instance_id, monitoringSearch)
    : '#trigger-metric-heading'
  const instance = $api.useQuery('get', '/api/v1/instances/{id}', {
    params: { path: { id: detail.instance_id } },
  })

  // `as` 槽只收组件，路由属性写在闭包里，用 useMemo 固定身份（web/CLAUDE.md 先例）。
  const links = useMemo(() => ({
    monitoring: monitoringSearch === undefined
      ? undefined
      : (props: object) => <Link {...props} to="/instances/$id/monitoring" params={{ id: detail.instance_id }} search={monitoringSearch} />,
    collection: (props: object) => <Link {...props} to="/instances/$id/collection" params={{ id: detail.instance_id }} />,
  }), [detail.instance_id, monitoringSearch])

  return <div className="alert-detail">
    <Link
      className="cds--link alert-detail__back"
      to="/instances/$id/alerts"
      params={{ id: routeInstanceID }}
      search={{ tab: detail.status === 'RECOVERED' ? 'history' : 'current', include_paused: false }}
    >← 返回告警列表</Link>

    <header className="alert-detail__header">
      <div className="alert-detail__heading">
        <div className="alert-detail__title-row">
          <h1 className="dbs-page-title">{detail.rule_name}</h1>
          <AlertStatus status={detail.status} />
          <StatusBadge tone={severityTone(detail.severity)}>{severityLabel(detail.severity)}</StatusBadge>
        </div>
        {/* 实例名与指标 ID 是这一页的身份，装不下就省略号 + 悬停全文，不从中间折行。 */}
        <p className="dbs-caption alert-detail__subject">
          <TruncatedText>{`${detail.instance_name} · ${detail.metric_id}`}</TruncatedText>
        </p>
      </div>
      <div className="alert-detail__actions">
        {links.monitoring && <Button as={links.monitoring} size="md" renderIcon={Icon.glyph.chartLine}>查看标准监控</Button>}
        <Button as={links.collection} kind="tertiary" size="md" renderIcon={Icon.glyph.database}>查看采集状态</Button>
      </div>
    </header>

    <Panel title="告警概览">
      <KeyValueList label="告警概览" columns={3} items={[
        { key: 'status', label: '状态', value: <AlertStatus status={detail.status} /> },
        { key: 'severity', label: '级别', value: <StatusBadge tone={severityTone(detail.severity)}>{severityLabel(detail.severity)}</StatusBadge> },
        {
          key: 'markers',
          label: '标记',
          value: <AlertSuppressionTags
            inMaintenance={detail.in_maintenance}
            disposition={detail.disposition}
            paused={detail.paused}
            pausedAt={detail.paused_at}
          />,
        },
        { key: 'value', label: '触发值 / 阈值', value: <span className="dbs-numeric">{`${optionalNumber(detail.current_value)} / ${optionalNumber(detail.threshold)}`}</span> },
        { key: 'duration', label: '持续时间', value: <span className="dbs-numeric">{durationLabel(detail.duration_ms)}</span> },
        { key: 'version', label: '规则版本', value: <span className="dbs-numeric">{`版本 ${detail.rule_version}`}</span> },
        { key: 'first', label: '首次触发', value: <span className="dbs-numeric">{optionalTime(detail.first_triggered_at)}</span> },
        { key: 'updated', label: '最近评估', value: <span className="dbs-numeric">{optionalTime(detail.updated_at)}</span> },
        { key: 'recovered', label: '恢复时间', value: <span className="dbs-numeric">{optionalTime(detail.recovered_at)}</span> },
      ]} />
    </Panel>

    <Panel title="触发指标" id="trigger-metric-heading">
      {metric && monitoringSearch
        ? <AlertMetricChart detail={detail} metricID={metric} monitoringSearch={monitoringSearch} />
        : <div className="alert-detail__missing-metric">
          <NotificationBar tone="critical" title="告警指标不在指标字典中">
            没有对应的采集定义，因此画不出触发时的曲线。
          </NotificationBar>
          {/* 指标 ID 单独一行并截断：把它塞进通知正文里，那串不可断的点分标识会把
              通知在手机宽度上直接撑出屏幕（实测 390px 溢出 12px）。 */}
          <p className="alert-detail__placeholder dbs-numeric">
            <TruncatedText>{detail.metric_id}</TruncatedText>
          </p>
        </div>}
    </Panel>

    <Panel title="规则快照">
      <div className="alert-detail__snapshot">
        {/*
          规则快照是等宽 JSON。**横向滚动，不折行** —— 从中间折断一个键名或阈值，
          等宽文本立刻不能读了（票 #201：不破坏等宽文本可读性）。滚动条落在这个 <pre>
          自己身上，与表格无关，也不会把页面撑出横向滚动。
        */}
        <pre className="alert-detail__json dbs-numeric" tabIndex={0} aria-label="规则快照 JSON">
          {JSON.stringify(detail.rule_snapshot, null, 2)}
        </pre>
        <DataGrid<RuleVersionRecord>
          label="规则版本切换记录"
          density="dense"
          rows={detail.rule_version_history}
          rowKey={(record) => `${record.version}-${record.evaluated_at}`}
          columns={ruleVersionColumns}
          empty={{ title: '暂无版本切换记录' }}
        />
      </div>
    </Panel>

    <Panel title="No Data 原因">
      {detail.unavailability
        ? <UnavailabilityBlock
            code={detail.unavailability}
            href={alertUnavailabilityHref(
              detail.instance_id,
              detail.metric_id,
              detail.unavailability,
              currentMetricHref,
            )}
          />
        : <p className="alert-detail__placeholder dbs-body">无 No Data 原因，本次评估拿到了数据。</p>}
    </Panel>

    <Panel title="采集状态">
      {instance.isPending
        ? <SkeletonBlock lines={2} label="采集状态加载中" />
        : <KeyValueList label="采集状态" columns={3} items={[
          { key: 'agent', label: 'Agent', value: instance.data ? <StatusBadge tone={agentStatusTone(instance.data.agent_status)}>{agentStatusLabel(instance.data.agent_status)}</StatusBadge> : '—' },
          { key: 'collected', label: '最近成功采集', value: <span className="dbs-numeric">{optionalTime(instance.data?.last_collected_at)}</span> },
          { key: 'pause', label: '采集暂停', value: collectionPauseStatus(instance.data) },
        ]} />}
    </Panel>

    {/* 两块空态面板先并成一行，两块高面板再并成一行：反过来排会让矮的那半列
        在高的那半列旁边留一大片空白。 */}
    <div className="alert-detail__grid">
      <Panel title="通知结果">
        <NotificationResults results={detail.notification_results} />
      </Panel>
      <Panel title="关联性能事件">
        <p className="alert-detail__placeholder dbs-body">暂无关联性能事件。</p>
      </Panel>
      <DispositionSection
        alertInstanceID={detail.id}
        recovered={detail.status === 'RECOVERED'}
        onChanged={onDispositionChanged}
      />
      <TriggerSnapshotSection alertInstanceID={detail.id} />
    </div>
  </div>
}

/// 通知投递结果。接口目前恒返回空数组（schema 上的注释写明「Empty until notification
/// delivery records are produced」），所以这里说的是「还没有记录」，不是「通知失败了」。
function NotificationResults({ results }: { results: AlertDetail['notification_results'] }) {
  if (results.length === 0) {
    return <p className="alert-detail__placeholder dbs-body">暂无通知结果</p>
  }
  return <p className="alert-detail__placeholder dbs-body">{`${results.length} 条通知结果`}</p>
}

/// 键值清单。原来是 AntD 的 `Descriptions`，这里用 Carbon 的结构化列表表达同一件事。
function KeyValueList({ label, items, columns = 1 }: {
  label: string
  items: { key: string; label: string; value: ReactNode }[]
  columns?: 1 | 2 | 3
}) {
  return <StructuredListWrapper aria-label={label} isCondensed className="alert-detail__list" data-columns={columns}>
    <StructuredListBody>
      {items.map((item) => (
        <StructuredListRow key={item.key}>
          <StructuredListCell noWrap>{item.label}</StructuredListCell>
          <StructuredListCell>{item.value}</StructuredListCell>
        </StructuredListRow>
      ))}
    </StructuredListBody>
  </StructuredListWrapper>
}

function AlertMetricChart({ detail, metricID, monitoringSearch }: {
  detail: AlertDetail
  metricID: MetricID
  monitoringSearch: ReturnType<typeof alertMonitoringSearch>
}) {
  const metrics = $api.useQuery('get', '/api/v1/instances/{id}/metrics/series', {
    params: {
      path: { id: detail.instance_id },
      query: {
        metric: [metricID],
        from: monitoringSearch.from,
        to: monitoringSearch.to,
        step: monitoringSearch.step,
      },
    },
  })
  const response = metrics.data?.metrics[0]
  const series: MetricChartSeries[] = response?.unavailability === null
    ? response.series.map((item) => ({ name: detail.metric_id, unit: response.unit, points: item.points }))
    : []
  const unavailability = detail.unavailability ?? metricUnavailability(response)
  const remediationHref = unavailability
    ? alertUnavailabilityHref(
        detail.instance_id,
        detail.metric_id,
        unavailability,
        alertMonitoringHref(detail.instance_id, monitoringSearch),
      )
    : '#trigger-metric-heading'

  return <MetricChart
    label={detail.rule_name}
    series={series}
    step={metrics.data?.step ?? 'auto'}
    unavailability={unavailability}
    unavailabilityHref={remediationHref}
    loading={metrics.isFetching}
  />
}

function alertUnavailabilityHref(
  instanceID: string,
  metricID: string,
  code: components['schemas']['Unavailability'],
  currentHref: string,
): string {
  return unavailabilityHref(code, {
    current: currentHref,
    collection: `/instances/${encodeURIComponent(instanceID)}/collection?metric=${encodeURIComponent(metricID)}`,
  })
}

function alertMonitoringHref(instanceID: string, search: ReturnType<typeof alertMonitoringSearch>): string {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(search)) params.set(key, String(value))
  return `/instances/${encodeURIComponent(instanceID)}/monitoring?${params.toString()}`
}

const ruleVersionColumns: DataGridColumn<RuleVersionRecord>[] = [
  { key: 'version', header: '版本', minWidth: 80, numeric: true, cell: (record) => String(record.version) },
  { key: 'evaluated', header: '生效评估时间', minWidth: 180, cell: (record) => <TruncatedText className="dbs-numeric">{optionalTime(record.evaluated_at)}</TruncatedText> },
]

function collectionPauseStatus(instance: Instance | undefined): ReactNode {
  if (!instance) return '—'
  if (!instance.collection_pause.paused) return '否'
  return <StatusBadge tone="unknown">已暂停</StatusBadge>
}

/// 级别的视觉档位。详情页只讲这一条告警，所以级别就是它的级别，不再按状态降档。
/// 文字永远在，颜色不是唯一信号。
function severityTone(severity: components['schemas']['AlertSeverity']): StatusTone {
  switch (severity) {
    case 'critical': return 'critical'
    case 'warning': return 'warning'
    case 'info': return 'unknown'
    default: return assertNever(severity)
  }
}

function agentStatusLabel(status: components['schemas']['InstanceAgentStatus']): string {
  switch (status) {
    case 'online': return '在线'
    case 'offline': return '离线'
    case 'not_installed': return '未安装'
    case 'permission_denied': return '权限不足'
    case 'error': return '异常'
    default: return assertNever(status)
  }
}

function agentStatusTone(status: components['schemas']['InstanceAgentStatus']): StatusTone {
  switch (status) {
    case 'online': return 'normal'
    case 'offline': return 'unknown'
    case 'not_installed': return 'unknown'
    case 'permission_denied': return 'warning'
    case 'error': return 'critical'
    default: return assertNever(status)
  }
}

function optionalNumber(value: number | undefined): string {
  return value === undefined ? '—' : String(value)
}

function optionalTime(value: string | undefined): string {
  return value === undefined ? '—' : new Date(value).toLocaleString()
}

/** 与告警流、数据新鲜度共用同一套时长词汇（domain/Freshness）。 */
function durationLabel(milliseconds: number): string {
  return elapsedLabel(Math.floor(milliseconds / 1000))
}

function assertNever(value: never): never {
  throw new Error(`unexpected alert detail value: ${value}`)
}
