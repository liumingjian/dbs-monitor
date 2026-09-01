import { ContentSwitcher, Pagination, Switch, Tab, TabList, Tabs } from '@carbon/react'
import { Link, createRoute } from '@tanstack/react-router'
import { useEffect, useMemo, useState } from 'react'
import type { ComponentType, ReactNode } from 'react'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import { pollingIntervals } from '../../api/polling'
import type { components } from '../../api/schema'
import { AlertStatus } from '../../domain/AlertStatus'
import { Freshness, elapsedLabel } from '../../domain/Freshness'
import { AlertSuppressionTags } from '../../domain/SuppressionTags'
import { unavailabilityCopy, unavailabilityHref } from '../../domain/UnavailabilityBlock'
import { DataGrid } from '../../primitives/DataGrid'
import type { DataGridColumn } from '../../primitives/DataGrid'
import { Icon } from '../../primitives/Icon'
import { NotificationBar } from '../../primitives/NotificationBar'
import { Panel } from '../../primitives/Panel'
import { SkeletonBlock } from '../../primitives/SkeletonBlock'
import type { StatusTone } from '../../primitives/StatusBadge'
import { StatusBadge } from '../../primitives/StatusBadge'
import { Toggle } from '../../primitives/Toggle'
import { TruncatedText } from '../../primitives/TruncatedText'
import { rootRoute } from '../root'
import { browserStorage } from '../root/navCollapse'
import type { TableDensity } from '../root/tableDensity'
import { densityLabel, readTableDensity, writeTableDensity } from '../root/tableDensity'
import { parseAlertListSearch, type AlertListSearch } from './search'
import './alerts.css'

type AlertObservation = components['schemas']['AlertObservation']
type AlertSeverity = components['schemas']['AlertSeverity']

const alertPageSize = 50

/// 告警流收成单列的宽度。**这是本产品唯一一个为手机存在的断点**（DESIGN.md：
/// 「the one exception below that is the alert stream, which collapses to a single column at 768px」）。
/// 672px 以下应用外框还会把侧栏整个撤掉，所以这条断点必须比它宽 —— 单列版式要在
/// 「侧栏还在的窄屏」与「只剩页头的手机」两种情况下都成立。
const singleColumnQuery = '(max-width: 48rem)'

export const alertsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/alerts',
  validateSearch: (search): AlertListSearch | { error: string } => parseAlertListSearch(search),
  component: AlertsPage,
})

function AlertsPage() {
  const search = alertsRoute.useSearch()
  const navigate = alertsRoute.useNavigate()
  const includePaused = 'error' in search ? false : search.include_paused

  // `as` 槽只收组件，不能顺带把路由属性交出去，所以每个去处包成一个「已经知道自己去哪儿」
  // 的组件，并用 useMemo 固定身份 —— 身份一变锚点重挂，键盘焦点会被甩掉（web/CLAUDE.md 先例）。
  const tabLinks = useMemo(() => ({
    current: (props: object) => <Link {...props} to="/alerts" search={{ tab: 'current' as const, include_paused: includePaused, page: 1 }} />,
    history: (props: object) => <Link {...props} to="/alerts" search={{ tab: 'history' as const, include_paused: includePaused, page: 1 }} />,
  }), [includePaused])

  if ('error' in search) {
    return <InvalidAlertSearch
      message={search.error}
      reset={<Link className="cds--link" to="/alerts" search={{ tab: 'current', include_paused: false }}>使用默认筛选</Link>}
    />
  }
  return <AlertObservationLists
    search={search}
    onSearchChange={(next) => void navigate({ search: next })}
    tabLinks={tabLinks}
    heading="全局告警"
    lede="跨实例查看触发、恢复与处置事实"
  />
}

/// 筛选链接解析不了时的出口。链接另起一行放在通知条外面 ——
/// 通知条的正文区被组件库限定为非交互内容（primitives/NotificationBar）。
export function InvalidAlertSearch({ message, reset }: { message: string; reset: ReactNode }) {
  return <div className="alert-stream">
    <NotificationBar tone="critical" title={message}>
      链接里的筛选参数无法解析，因此没有取数。
    </NotificationBar>
    <span className="alert-stream__reset">{reset}</span>
  </div>
}

export type AlertTabLinks = {
  current: ComponentType<object>
  history: ComponentType<object>
}

/// 告警流。**全产品唯一一个必须在手机上可用的页面**，所以它有两套呈现：
/// 宽屏是一张表，768px 及以下换成一列卡片。两套不同时存在于 DOM 里 ——
/// 用 `display: none` 藏一套会让 `alert-row` 的计数翻倍，而那个标识的计数就等于数据行数。
///
/// 版式仍然是列表页的三段（web/CLAUDE.md 先例）：页头 → 工具条 → 一个 `flush` 的
/// `Panel` 包住表格、分页放进 footer。窄屏只把最后一段的内容换成卡片，三段本身不动。
export function AlertObservationLists({ search, onSearchChange, tabLinks, heading, lede, action, scopedToInstance = false }: {
  search: AlertListSearch
  onSearchChange: (search: AlertListSearch) => void
  tabLinks: AlertTabLinks
  heading?: string
  lede?: string
  action?: ReactNode
  /**
   * 这张表已经限定在一台实例上（实例工作台里的告警页签）。
   *
   * 这时「实例」列每一行都是同一个值，而那台实例的名字就写在上方的工作台页头里 ——
   * 一列不承载信息却要占掉约 90px，剩下的列于是各少一截，实例名自己反而被截成
   * `QA target p…`。所以这一列在实例范围下不渲染。**这不是「窄屏丢列」**
   * （那在任何宽度下都禁止）：全局告警页照常有这一列，两处宽度行为都不随视口改变。
   */
  scopedToInstance?: boolean
}) {
  const page = search.page ?? 1
  const offset = (page - 1) * alertPageSize
  const singleColumn = useSingleColumn()
  const [density, setDensity] = useState<TableDensity>(() => readTableDensity(browserStorage))
  const current = $api.useQuery('get', '/api/v1/alerts/current', {
    params: {
      query: {
        instance_id: search.instance_id,
        include_paused: search.include_paused,
        limit: alertPageSize,
        offset,
      },
    },
  }, { refetchInterval: pollingIntervals.currentAlerts })
  const history = $api.useQuery('get', '/api/v1/alerts/history', {
    params: {
      query: {
        instance_id: search.instance_id,
        limit: alertPageSize,
        offset,
      },
    },
  }, { refetchInterval: pollingIntervals.history })

  function changeDensity(next: TableDensity) {
    setDensity(next)
    writeTableDensity(browserStorage, next)
  }

  const currentRows = [...(current.data?.items ?? [])].sort(compareAlertUrgency)
  const historyRows = history.data?.items ?? []
  const showingCurrent = search.tab === 'current'
  const rows = showingCurrent ? currentRows : historyRows
  const query = showingCurrent ? current : history
  const emptyTitle = showingCurrent ? '没有符合筛选的当前告警' : '暂无告警历史'
  // 总条数还没回来的时候不出分页条：写一个 0 进去会先闪一句「共 0 条」，那不是真的。
  const total = query.data?.total
  const rowTestId = showingCurrent ? 'alert-row' : 'alert-history-row'
  const columns = (showingCurrent ? currentColumns : historyColumns)
    .filter((column) => !(scopedToInstance && column.key === 'instance'))

  return <div className="alert-stream">
    {(heading !== undefined || action !== undefined) && <header className="alert-stream__header">
      {heading !== undefined && <div className="alert-stream__heading">
        <h1 className="dbs-page-title">{heading}</h1>
        {lede !== undefined && <p className="dbs-caption alert-stream__lede">{lede}</p>}
      </div>}
      {action}
    </header>}

    <Tabs selectedIndex={showingCurrent ? 0 : 1}>
      <TabList aria-label="告警流" activation="manual">
        <Tab as={tabLinks.current}><TabLabel label="当前告警" count={current.data?.total} /></Tab>
        <Tab as={tabLinks.history}><TabLabel label="告警历史" count={history.data?.total} /></Tab>
      </TabList>
    </Tabs>

    <div className="alert-stream__toolbar" role="group" aria-label="告警筛选">
      {showingCurrent && <Toggle
        id="alert-filter-include-paused"
        size="sm"
        labelText="包含已暂停冻结告警"
        hideLabel
        toggled={search.include_paused}
        onToggle={(includePaused) => onSearchChange({ ...search, include_paused: includePaused, page: 1 })}
      />}
      {/* 告警历史不轮询（`pollingIntervals.history` 是 false），没有「多久没更新」可讲，
          所以新鲜度只出现在当前告警上 —— 和迁移前一致。 */}
      {showingCurrent && current.dataUpdatedAt > 0 && <span className="alert-stream__freshness">
        <Freshness dataUpdatedAt={current.dataUpdatedAt} collectionInterval={pollingIntervals.currentAlerts} />
      </span>}
    </div>

    {query.error && <NotificationBar
      tone="critical"
      title={apiErrorMessage(query.error, showingCurrent ? '当前告警加载失败' : '告警历史加载失败')}
    />}
    {showingCurrent && !sortCoversEveryRow(current.data?.total) && <NotificationBar
      tone="warning"
      title={`排序只覆盖本页 ${alertPageSize} 条`}
    >接口不支持按状态筛选，正在告警的规则可能落在其他页。</NotificationBar>}

    <Panel
      flush
      title={showingCurrent ? '当前告警' : '告警历史'}
      // 面板标题栏右侧只放「作用于这张表的视图开关」。卡片没有行高档位，所以窄屏不出这个开关。
      actions={singleColumn ? undefined : <DensitySwitcher density={density} onChange={changeDensity} />}
      footer={total === undefined ? undefined : <Pagination
        className="alert-stream__pagination"
        size="md"
        page={page}
        pageSize={alertPageSize}
        pageSizes={[alertPageSize]}
        totalItems={total}
        backwardText="上一页"
        forwardText="下一页"
        itemsPerPageText="每页条数"
        itemRangeText={(min, max, total) => `第 ${min}–${max} 条，共 ${total} 条`}
        pageRangeText={(_current, total) => `共 ${total} 页`}
        pageNumberText="页码"
        onChange={({ page: nextPage }) => onSearchChange({ ...search, page: nextPage })}
      />}
    >
      {singleColumn
        ? <AlertCardList
            rows={rows}
            loading={query.isPending}
            emptyTitle={emptyTitle}
            testId={rowTestId}
            recovery={!showingCurrent}
          />
        : <DataGrid<AlertObservation>
            label={showingCurrent ? '当前告警列表' : '告警历史列表'}
            density={density}
            // 11 / 13 列。组件库的 16px/侧内边距在这个列数下要吃掉 974px 里的 352–416px，
            // 表头就开始被截成「维...」。紧凑档换回一半，见下面列定义上的说明。
            cellPadding="compact"
            loading={query.isPending}
            skeletonRows={8}
            rows={rows}
            rowKey={(alert) => alert.id}
            rowTestId={rowTestId}
            rowTone={severityBarTone}
            rowMuted={(alert) => !isUnresolved(alert.status)}
            columns={columns}
            empty={{
              title: emptyTitle,
              description: showingCurrent ? '调整筛选条件，或确认这些实例当前确实没有触发中的规则。' : undefined,
            }}
          />}
    </Panel>
  </div>
}

function TabLabel({ label, count }: { label: string; count: number | undefined }) {
  return <span className="alert-stream__tab">
    {label}
    {count !== undefined && <span className="alert-stream__tab-count dbs-numeric">{count}</span>}
  </span>
}

/// 密度切换。产品级偏好，读写只有 `routes/root/tableDensity.ts` 一个去处（web/CLAUDE.md）。
function DensitySwitcher({ density, onChange }: { density: TableDensity; onChange: (density: TableDensity) => void }) {
  const densities = ['standard', 'dense'] as const satisfies readonly TableDensity[]
  return (
    <ContentSwitcher
      className="alert-stream__density"
      size="sm"
      selectedIndex={densities.indexOf(density)}
      onChange={({ index }) => {
        // 组件库把选中下标标成可选；拿不到下标就是没换档，什么都不做，别兜底成第一档。
        const next = index === undefined ? undefined : densities[index]
        if (next !== undefined) onChange(next)
      }}
    >
      {densities.map((value) => <Switch key={value} name={value} text={densityLabel(value)} />)}
    </ContentSwitcher>
  )
}

/// 窄屏判定。**只有这一页需要它**：其余页面都假设读者面前有一张桌子。
function useSingleColumn(): boolean {
  const [singleColumn, setSingleColumn] = useState(() => matchMedia(singleColumnQuery).matches)
  useEffect(() => {
    const query = matchMedia(singleColumnQuery)
    const update = () => setSingleColumn(query.matches)
    // 订阅之前先对一次：首帧到订阅之间视口可能已经变了（旋转屏幕、恢复窗口）。
    update()
    query.addEventListener('change', update)
    return () => query.removeEventListener('change', update)
  }, [])
  return singleColumn
}

/// 手机上的告警流：一条告警一张卡片。
///
/// 十一列在 390px 上没有出路 —— 平均分下来一列 35px，或者退化成一根横向滚动的长条，
/// 而这正是有人被叫醒之后先打开的那块屏幕。卡片把同一份事实竖着排：先是状态与级别，
/// 再是规则与实例，然后是数值与时间；处置与维护窗口由标记条表达，与表格里的「标记」列同源。
/// 唯一不出现在卡片上的是「通知结果」——那一列在接口产出投递记录之前恒为「—」。
function AlertCardList({ rows, loading, emptyTitle, testId, recovery }: {
  rows: AlertObservation[]
  loading: boolean
  emptyTitle: string
  testId: string
  recovery: boolean
}) {
  if (loading) {
    return <ul className="alert-cards" aria-busy="true" aria-label="告警加载中">
      {Array.from({ length: 5 }, (_, index) => <li className="alert-card" key={index}>
        <SkeletonBlock lines={3} decorative />
      </li>)}
    </ul>
  }
  if (rows.length === 0) {
    return <p className="alert-cards__empty dbs-body">{emptyTitle}</p>
  }
  return <ul className="alert-cards">
    {rows.map((alert) => <li className="alert-card" key={alert.id} data-testid={testId} data-tone={severityBarTone(alert)}>
      <div className="alert-card__status">
        <AlertStatus status={alert.status} />
        <SeverityBadge severity={alert.severity} status={alert.status} />
        <span className="alert-card__duration dbs-numeric">{durationLabel(alert.duration_ms)}</span>
      </div>
      <p className="alert-card__rule"><TruncatedText>{alert.rule_name}</TruncatedText></p>
      <p className="alert-card__subject dbs-caption">
        <TruncatedText>{`${alert.instance_name} · ${alert.metric_id}`}</TruncatedText>
      </p>
      <dl className="alert-card__facts">
        <div>
          <dt className="dbs-caption">触发值 / 阈值</dt>
          <dd className="dbs-numeric">{valueAgainstThreshold(alert)}</dd>
        </div>
        <div>
          <dt className="dbs-caption">首次触发</dt>
          <dd className="dbs-numeric"><TimeCell value={alert.first_triggered_at} /></dd>
        </div>
        {recovery && <div>
          <dt className="dbs-caption">恢复时间</dt>
          <dd className="dbs-numeric"><TimeCell value={alert.recovered_at} /></dd>
        </div>}
        {recovery && <div>
          <dt className="dbs-caption">规则版本</dt>
          <dd className="dbs-numeric">{`版本 ${alert.rule_version}`}</dd>
        </div>}
      </dl>
      <AlertSuppressionTags
        className="alert-card__markers"
        inMaintenance={alert.in_maintenance}
        disposition={alert.disposition}
        paused={alert.paused}
        pausedAt={alert.paused_at}
      />
      {alert.unavailability !== undefined && <div className="alert-card__reason">
        <AlertUnavailabilityReason alert={alert} />
      </div>}
      <Link
        className="cds--link alert-card__detail"
        to="/instances/$id/alerts/$alertId"
        params={{ id: alert.instance_id, alertId: alert.id }}
      >详情</Link>
    </li>)}
  </ul>
}

function statusRank(status: components['schemas']['AlertStatus']): number {
  switch (status) {
    case 'FIRING':
      return 4
    case 'NO_DATA':
      return 3
    case 'PENDING':
      return 2
    case 'RECOVERED':
      return 1
    case 'OK':
      return 0
    default:
      return assertNever(status)
  }
}

function severityRank(severity: AlertSeverity): number {
  switch (severity) {
    case 'critical':
      return 3
    case 'warning':
      return 2
    case 'info':
      return 1
    default:
      return assertNever(severity)
  }
}

export function isUnresolved(status: components['schemas']['AlertStatus']): boolean {
  return statusRank(status) >= statusRank('PENDING')
}

/** The sort runs on what is loaded, and the endpoint takes only limit/offset. */
export function sortCoversEveryRow(total: number | undefined): boolean {
  return total === undefined || total <= alertPageSize
}

/**
 * The endpoint returns every evaluated rule, so a single FIRING row can sit below
 * twenty OK ones. Rank what is burning first, then by severity, then by how long.
 */
export function compareAlertUrgency(left: AlertObservation, right: AlertObservation): number {
  const byStatus = statusRank(right.status) - statusRank(left.status)
  if (byStatus !== 0) return byStatus
  const bySeverity = severityRank(right.severity) - severityRank(left.severity)
  if (bySeverity !== 0) return bySeverity
  return right.duration_ms - left.duration_ms
}

export function severityLabel(severity: AlertSeverity): string {
  switch (severity) {
    case 'critical': return '严重'
    case 'warning': return '警告'
    case 'info': return 'Info'
    default: return assertNever(severity)
  }
}

/// 级别的视觉档位。**文字永远在**，颜色只是让扫视时先看见那一行。
///
/// 已经不烧了的行（OK / 已恢复）走中性档，而不是干脆不显示级别：级别是规则的属性，
/// 藏掉它就要在两个地方解释「这一行为什么没有级别」；可是把一屏评估通过的规则涂成一片红，
/// 又会把真正在烧的那两行淹掉。所以是同一个字、换一档颜色。
function severityTone(severity: AlertSeverity, status: components['schemas']['AlertStatus']): StatusTone {
  if (!isUnresolved(status)) return 'unknown'
  switch (severity) {
    case 'critical': return 'critical'
    case 'warning': return 'warning'
    case 'info': return 'unknown'
    default: return assertNever(severity)
  }
}

function SeverityBadge({ severity, status }: { severity: AlertSeverity; status: components['schemas']['AlertStatus'] }) {
  return <StatusBadge tone={severityTone(severity, status)}>{severityLabel(severity)}</StatusBadge>
}

/// 行首 3px 色条：只画「正在烧、而且够严重」的行。每一行都上色等于没有色条。
/// 它重复的是同一行「状态」「级别」两列已经写着的字，不是唯一信号。
///
/// 与它成对的是 `rowMuted`（已恢复 / 评估通过的行退到灰底次要色）：一屏里
/// 黑字白底的那几行就是还在烧的。两件事同一个维度的两端，一行不会同时命中。
function severityBarTone(alert: AlertObservation): StatusTone | undefined {
  if (!isUnresolved(alert.status)) return undefined
  switch (alert.severity) {
    case 'critical': return 'critical'
    case 'warning': return 'warning'
    case 'info': return undefined
    default: return assertNever(alert.severity)
  }
}

/**
 * 列定义。**只给 `minWidth`，页面不设任何 `overflow-x`** —— 横向行为整个由
 * `primitives/DataGrid` 决定（web/CLAUDE.md）。
 *
 * 迁移前「状态与标记」一格里塞着状态、级别、抑制标记三件事，40px 的行放不下；
 * 拆成三列之后一格只写一个事实，列与列之间扫视时对得齐。
 * 触发值与阈值反过来并成一格：读它们的方式本来就是比大小，`96 / 80` 是一个事实而不是两个。
 *
 * 宽度按 web/CLAUDE.md 的列宽契约算出来的，不是估的（1280px 下页面可用 974px）：
 * 每列 `minWidth` = max(表头自然宽, 这一格里压不动的内容宽) + 紧凑档的 16px 内边距，
 * 各列一律 `grow: 1`。两条都要守住才有意义 —— `grow` 一旦不等，某些列分到的宽度就会
 * 低于自己的 `minWidth`，表头先被截掉的正是它们。合计 934 / 958 ≤ 974，因此 1280px 下
 * 每列都不低于自己的下限，富余按下限比例分给最需要的长文本列。
 *
 * 压不动的内容：状态与级别徽标、`96 / 80`、「08-11 10:15」、行内图标链接。
 * 压得动的：实例名、规则名、指标 ID —— 它们截断并带悬停全文，且都能在详情页读到全文。
 */
const currentColumns: DataGridColumn<AlertObservation>[] = [
  { key: 'status', header: '状态', minWidth: 69, cell: (alert) => <AlertStatus status={alert.status} /> },
  { key: 'severity', header: '级别', minWidth: 57, cell: (alert) => <SeverityBadge severity={alert.severity} status={alert.status} /> },
  { key: 'instance', header: '实例', minWidth: 96, cell: (alert) => <TruncatedText>{alert.instance_name}</TruncatedText> },
  // 规则名是这一行的身份，截断它等于让读者认不出这是什么告警：文本列里富余宽度优先给它。
  { key: 'rule', header: '规则', minWidth: 126, cell: (alert) => <TruncatedText className="alert-stream__rule">{alert.rule_name}</TruncatedText> },
  // 指标 ID 是等宽的点分标识：装不下就省略号 + 悬停全文，绝不从中间折行。
  { key: 'metric', header: '指标', minWidth: 96, cell: (alert) => <TruncatedText className="dbs-numeric">{alert.metric_id}</TruncatedText> },
  { key: 'value', header: '触发值 / 阈值', minWidth: 106, numeric: true, cell: (alert) => <TruncatedText className="dbs-numeric">{valueAgainstThreshold(alert)}</TruncatedText> },
  { key: 'duration', header: '持续时间', minWidth: 69, numeric: true, cell: (alert) => durationLabel(alert.duration_ms) },
  { key: 'first-triggered', header: '首次触发', minWidth: 115, numeric: true, cell: (alert) => <TimeCell value={alert.first_triggered_at} /> },
  {
    key: 'markers',
    header: '标记',
    minWidth: 69,
    cell: (alert) => <AlertSuppressionTags
      className="alert-stream__markers"
      inMaintenance={alert.in_maintenance}
      disposition={alert.disposition}
      paused={alert.paused}
      pausedAt={alert.paused_at}
    />,
  },
  { key: 'unavailability', header: 'No Data 原因', minWidth: 90, cell: (alert) => <AlertUnavailabilityReason alert={alert} iconAction /> },
  {
    key: 'actions',
    header: '操作',
    minWidth: 41,
    align: 'end',
    // 行内操作是图标链接（与实例列表同一做法）：可访问名由 `aria-label` 显式给出、
    // 悬停提示是同一句话。写成「详情」两个字要 71px，而这个去处每一行都一样，
    // 那 23px 花在「这一行到底怎么了」的列上更值。
    cell: (alert) => <Link
      className="cds--link alert-stream__detail"
      to="/instances/$id/alerts/$alertId"
      params={{ id: alert.instance_id, alertId: alert.id }}
      aria-label="详情"
      title="详情"
    ><Icon name="view" /></Link>,
  },
]

const historyColumns: DataGridColumn<AlertObservation>[] = [
  { key: 'status', header: '状态', minWidth: 62, cell: (alert) => <AlertStatus status={alert.status} /> },
  { key: 'severity', header: '级别', minWidth: 57, cell: (alert) => <SeverityBadge severity={alert.severity} status={alert.status} /> },
  { key: 'instance', header: '实例', minWidth: 71, cell: (alert) => <TruncatedText>{alert.instance_name}</TruncatedText> },
  { key: 'rule', header: '规则', minWidth: 88, cell: (alert) => <TruncatedText className="alert-stream__rule">{alert.rule_name}</TruncatedText> },
  { key: 'triggered', header: '触发时间', minWidth: 108, numeric: true, cell: (alert) => <TimeCell value={alert.first_triggered_at} /> },
  { key: 'recovered', header: '恢复时间', minWidth: 108, numeric: true, cell: (alert) => <TimeCell value={alert.recovered_at} /> },
  { key: 'duration', header: '持续时间', minWidth: 69, numeric: true, cell: (alert) => durationLabel(alert.duration_ms) },
  { key: 'value', header: '触发值 / 阈值', minWidth: 94, numeric: true, cell: (alert) => <TruncatedText className="dbs-numeric">{valueAgainstThreshold(alert)}</TruncatedText> },
  { key: 'version', header: '规则版本', minWidth: 65, numeric: true, cell: (alert) => `版本 ${alert.rule_version}` },
  // 通知投递记录接口尚未产出（schema 上恒为空数组），这一列如实写「—」，不编一个状态出来。
  { key: 'notification', header: '通知结果', minWidth: 65, cell: () => '—' },
  { key: 'disposition', header: '处置记录', minWidth: 65, cell: (alert) => dispositionLabel(alert.disposition) },
  { key: 'maintenance', header: '维护窗口', minWidth: 65, cell: (alert) => maintenanceWindowLabel(alert.in_maintenance) },
  {
    key: 'actions',
    header: '操作',
    minWidth: 41,
    align: 'end',
    // 行内操作是图标链接（与实例列表同一做法）：可访问名由 `aria-label` 显式给出、
    // 悬停提示是同一句话。写成「详情」两个字要 71px，而这个去处每一行都一样，
    // 那 23px 花在「这一行到底怎么了」的列上更值。
    cell: (alert) => <Link
      className="cds--link alert-stream__detail"
      to="/instances/$id/alerts/$alertId"
      params={{ id: alert.instance_id, alertId: alert.id }}
      aria-label="详情"
      title="详情"
    ><Icon name="view" /></Link>,
  },
]

type AlertUnavailabilityReasonProps = {
  alert: Pick<AlertObservation, 'instance_id' | 'metric_id' | 'unavailability'> &
    Partial<Pick<AlertObservation, 'id'>>
  /**
   * 表格里用图标链接代替文字链接。40px 的行给这一格只有八十几个像素，
   * 「补齐监控权限」六个字会把原因本身挤没；图标的可访问名与悬停提示仍然是那句话，
   * 与实例列表的行内图标操作是同一个做法。宽松版式（手机卡片）保持文字。
   */
  iconAction?: boolean
}

/// 「这一格为什么没有数字」。原因与去处写在同一行：40px 的行放不下两行，
/// 而只写原因、不给去处，读者还得自己想下一步该点哪里。
export function AlertUnavailabilityReason({ alert, iconAction = false }: AlertUnavailabilityReasonProps) {
  if (!alert.unavailability) return '—'
  const copy = unavailabilityCopy(alert.unavailability)
  const href = unavailabilityHref(alert.unavailability, {
    current: alert.id
      ? `/instances/${encodeURIComponent(alert.instance_id)}/alerts/${encodeURIComponent(alert.id)}`
      : `/instances/${encodeURIComponent(alert.instance_id)}/alerts`,
    collection: `/instances/${encodeURIComponent(alert.instance_id)}/collection?metric=${encodeURIComponent(alert.metric_id)}`,
  })
  return <span className="alert-reason" data-icon-action={iconAction ? 'true' : undefined}>
    <span className="alert-reason__title" title={copy.title}>{copy.title}</span>
    <a
      className="cds--link alert-reason__action"
      href={href}
      aria-label={iconAction ? copy.action : undefined}
      title={copy.action}
    >{iconAction ? <Icon name="arrowRight" /> : copy.action}</a>
  </span>
}

function dispositionLabel(disposition: components['schemas']['AlertDisposition']): string {
  switch (disposition) {
    case 'NONE': return '—'
    case 'ACKED': return '已确认'
    case 'IGNORED': return '已忽略'
    default: return assertNever(disposition)
  }
}

function maintenanceWindowLabel(inMaintenance: boolean | null | undefined): string {
  if (inMaintenance === undefined) return '—'
  return inMaintenance ? '是' : '否'
}

const alertNumber = new Intl.NumberFormat('zh-CN', { maximumSignificantDigits: 4 })

export function optionalNumber(value: number | undefined): string {
  // String(8.242500000000001) wrapped the cell to three lines. Rounding is fine;
  // compacting is not — an operator has to be able to read a threshold back.
  return value === undefined ? '—' : alertNumber.format(value)
}

function valueAgainstThreshold(alert: Pick<AlertObservation, 'current_value' | 'threshold'>): string {
  return `${optionalNumber(alert.current_value)} / ${optionalNumber(alert.threshold)}`
}

/// 表格里的时刻。**短格式 + 悬停全文**：完整的本地化时间戳要 170px，一屏十一列给不出来，
/// 而扫视时读的是「哪一天几点」，年份与秒都不参与判断。全文在 `title` 里，一个悬停就有。
const shortTimeFormat = new Intl.DateTimeFormat('zh-CN', {
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  hour12: false,
})

function TimeCell({ value }: { value: string | undefined }) {
  if (value === undefined) return '—'
  const at = new Date(value)
  return <TruncatedText className="dbs-numeric" title={at.toLocaleString()}>{shortTimeFormat.format(at)}</TruncatedText>
}

export function durationLabel(milliseconds: number): string {
  // Flooring to minutes reported every sub-minute alert as "0 分钟".
  return elapsedLabel(Math.floor(milliseconds / 1000))
}

function assertNever(value: never): never {
  throw new Error(`unexpected alert observation value: ${value}`)
}
