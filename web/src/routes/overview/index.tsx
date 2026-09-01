import { Link, createRoute } from '@tanstack/react-router'
import { $api } from '../../api/client'
import { apiErrorMessage } from '../../api/errors'
import { pollingIntervals } from '../../api/polling'
import type { components } from '../../api/schema'
import { Freshness } from '../../domain/Freshness'
import { HealthStatus } from '../../domain/HealthStatus'
import { SuppressionTags } from '../../domain/SuppressionTags'
import type { DataGridColumn } from '../../primitives/DataGrid'
import { DataGrid } from '../../primitives/DataGrid'
import { MetricBar } from '../../primitives/MetricBar'
import { NotificationBar } from '../../primitives/NotificationBar'
import { Panel } from '../../primitives/Panel'
import { TruncatedText } from '../../primitives/TruncatedText'
import { attributionLabel, collectionFreshnessLabel, collectionFreshnessTitle } from '../instanceProjection'
import { defaultTimeRange } from '../instances.$id/timeRange'
import { rootRoute } from '../root'
import type { OverviewCount, StorageWatermarkEntry, TopSqlEntry } from './overview'
import {
  collectionCountTiles,
  healthCountTiles,
  storageRatio,
  storageTone,
  topSqlSummaries,
  usagePercentLabel,
} from './overview'
import './overview.css'

type Instance = components['schemas']['Instance']

/// 机群总览，登录后的落地页（`/`）。
///
/// 五块从上到下，对应值班的人打开页面时依次问的几个问题：
///   1. 机群健康计数 —— 整体还好吗；
///   2. 采集自监控 —— 我看到的「还好」是不是因为根本没在采；
///   3. 需要关注的实例 Top 10 —— 我现在该看谁；
///   4. 容量水位 Top 10 —— 这类指标不会报警，但会要命；
///   5. Top SQL 前 5 —— 谁在费掉这些资源。
///
/// 第二块排在第三块前面是刻意的：五百台里最容易悄悄烂掉的就是采集这一层，
/// 而它烂掉时前一块会显示一片「正常」。第五块排在最后是因为它回答的是「为什么」，
/// 而值班的人先要知道「是不是出事了」。
///
/// **不做**：全量实例格子墙（五百个格子没有信息量）、装饰性 KPI 环形图
/// （DESIGN.md 的 Data visualisation 一节里压根没有环形图这个组件）。
export const overviewRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  component: OverviewPage,
})

function OverviewPage() {
  const overviewQuery = $api.useQuery('get', '/api/v1/overview', {}, { refetchInterval: pollingIntervals.overview })
  const overview = overviewQuery.data
  // 载入中一律骨架占位，不是整页转圈：已经取到的部分要先看得见，
  // 而四块的版式在骨架阶段就已经站好，数据到位时页面不跳。
  const loading = overview === undefined && overviewQuery.isPending

  return (
    <div className="overview-page">
      <header className="overview-page__header">
        <h1 className="dbs-page-title">机群总览</h1>
        {overview !== undefined && (
          <Freshness dataUpdatedAt={overviewQuery.dataUpdatedAt} collectionInterval={pollingIntervals.overview} />
        )}
      </header>

      {overviewQuery.isError && (
        <NotificationBar tone="critical" title={apiErrorMessage(overviewQuery.error, '机群汇总加载失败')}>
          机群汇总没有取到。下面的数字不是「零台出事」，而是还不知道。
        </NotificationBar>
      )}

      <Panel
        title="机群健康"
        description={overview === undefined ? undefined : `${overview.total} 台实例，按健康档位分布。每个数字可点，落到筛好的实例列表。`}
        loading={loading}
      >
        {overview !== undefined && <CountTiles tiles={healthCountTiles(overview.health)} label="机群健康计数" />}
      </Panel>

      <Panel
        title="采集自监控"
        description="采集自己断掉时没有任何告警会响——上面那一块会显示一片「正常」。"
        loading={loading}
      >
        {overview !== undefined && <CountTiles tiles={collectionCountTiles(overview.collection)} label="采集自监控计数" />}
      </Panel>

      <Panel title="需要关注的实例" description="按健康档位与告警严重度排序，最多十台。" flush>
        <DataGrid
          label="需要关注的实例"
          columns={attentionColumns}
          rows={overview?.attention ?? []}
          rowKey={(instance) => instance.id}
          rowTestId="overview-attention-row"
          density="standard"
          loading={loading}
          skeletonRows={5}
          empty={{ title: '没有需要处理的实例', description: '严重、警告与未知三档现在都是空的。' }}
        />
      </Panel>

      <Panel
        title="容量水位"
        description="磁盘使用率最高的十台。这类指标不会报警，但会要命。"
        loading={loading}
      >
        {overview !== undefined && <StorageWatermarks entries={overview.storage} />}
      </Panel>

      <Panel
        title="Top SQL 前 5"
        description="跨实例按总耗时排序。显示的是归一化后的语句（字面量已是 $1 占位符）。"
        loading={loading}
      >
        {overview !== undefined && <TopSqlList entries={overview.top_sql} />}
      </Panel>
    </div>
  )
}

/// 一组可点的计数。
///
/// 每一块整块是一个链接：数字本身就是入口，读者不用先找一个「查看」按钮。
/// 去处由 `InstanceListSearch` 对象给出，路由器按实例列表的契约拼地址 ——
/// 页面里没有一处手拼的 query 字符串。
export function CountTiles({ tiles, label }: { tiles: OverviewCount[]; label: string }) {
  return (
    <ul className="overview-tiles" aria-label={label}>
      {tiles.map((tile) => (
        <li key={tile.key}>
          <Link className="overview-tile" to="/instances" search={tile.search} data-testid="overview-count">
            <MetricBar label={tile.label} value={tile.count} tone={tile.tone} emphasis />
          </Link>
        </li>
      ))}
    </ul>
  )
}

/// 容量水位：十行「实例名 + 百分比 + 比例条」，不是十个环形图。
/// 缺读数的实例不在榜上，也不以 0% 出现——从没量过与「盘是空的」是两件事。
export function StorageWatermarks({ entries }: { entries: StorageWatermarkEntry[] }) {
  if (entries.length === 0) {
    return <p className="overview-empty dbs-caption">还没有磁盘水位读数。这项由 Agent 上报，装了 Agent 才有。</p>
  }
  return (
    <ul className="overview-watermarks" aria-label="容量水位前十">
      {entries.map((entry) => (
        <li key={entry.instance_id}>
          <Link
            className="overview-watermark"
            to="/instances/$id"
            params={{ id: entry.instance_id }}
            search={defaultTimeRange()}
            data-testid="overview-watermark"
          >
            <MetricBar
              label={<TruncatedText>{entry.instance_name}</TruncatedText>}
              value={usagePercentLabel(entry.usage_percent)}
              ratio={storageRatio(entry.usage_percent)}
              tone={storageTone(entry.usage_percent)}
            />
          </Link>
        </li>
      ))}
    </ul>
  )
}

/// Top SQL 前 5：五行「语句 + 总耗时 + 比例条」，整行是去 SQL 洞察全页的入口。
///
/// 这里不重复一张表：五行里再摆四列，每列在 974px 下分不到能读的宽度，
/// 而这一块要回答的只是「最费资源的是哪几条」——「是哪台、跑了多少次」压进注解一行。
export function TopSqlList({ entries }: { entries: TopSqlEntry[] }) {
  if (entries.length === 0) {
    return <p className="overview-empty dbs-caption">
      还没有查询统计。这一块来自 pg_stat_statements，装了这个扩展并采集过一轮之后才有。
    </p>
  }
  return (
    <ul className="overview-top-sql" aria-label="Top SQL 前 5">
      {topSqlSummaries(entries).map((summary) => (
        <li key={summary.key}>
          <Link className="overview-top-sql__row" to="/sql-insight" data-testid="overview-top-sql">
            <MetricBar
              label={<TruncatedText className="overview-top-sql__statement">{summary.statement}</TruncatedText>}
              value={summary.elapsed}
              ratio={summary.ratio}
              caption={summary.caption}
            />
          </Link>
        </li>
      ))}
    </ul>
  )
}

/// 「需要关注」的五列，974px 预算内：实例(260) · 健康(120) · 告警(90) · 告警归因(250) ·
/// 采集新鲜度(127) = 847。列比实例列表少，因为这张表回答的是「该看谁」，
/// 而不是「这台的每一项都怎么样」——后者点进实例名就有。
const attentionColumns: DataGridColumn<Instance>[] = [
  {
    key: 'name',
    header: '实例',
    minWidth: 260,
    cell: (instance) => (
      <Link className="cds--link" to="/instances/$id" params={{ id: instance.id }} search={defaultTimeRange()}>
        <TruncatedText>{instance.name}</TruncatedText>
      </Link>
    ),
  },
  {
    key: 'health',
    header: '健康',
    minWidth: 120,
    cell: (instance) => (
      <span className="overview-attention__health">
        <HealthStatus status={instance.health.status} pausedAt={instance.collection_pause.updated_at} />
        <SuppressionTags flags={instance.health.flags} />
      </span>
    ),
  },
  {
    key: 'counts',
    header: '告警',
    minWidth: 90,
    align: 'end',
    cell: (instance) => (
      <span className="overview-attention__counts dbs-numeric">
        {`C${instance.health.counts.critical} W${instance.health.counts.warning} I${instance.health.counts.info}`}
      </span>
    ),
  },
  {
    key: 'attribution',
    header: '告警归因',
    minWidth: 250,
    cell: (instance) => <TruncatedText>{attributionLabel(instance)}</TruncatedText>,
  },
  {
    key: 'freshness',
    header: '采集新鲜度',
    minWidth: 127,
    cell: (instance) => (
      <TruncatedText title={collectionFreshnessTitle(instance)}>{collectionFreshnessLabel(instance)}</TruncatedText>
    ),
  },
]
