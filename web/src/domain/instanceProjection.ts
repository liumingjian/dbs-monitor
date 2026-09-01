import type { StatusTone } from '../primitives/StatusBadge'
import type { components } from '../api/schema'

type Instance = components['schemas']['Instance']
type InstanceAgentStatus = components['schemas']['InstanceAgentStatus']
type MetricSeriesEntry = components['schemas']['MetricSeriesEntry']
type InstancesMetricSeriesResponse = components['schemas']['InstancesMetricSeriesResponse']
type SemanticSlot = components['schemas']['SemanticSlot']

function assertNever(value: never): never {
  throw new Error(`unexpected instance projection value: ${String(value)}`)
}

export function attributionLabel(instance: Instance): string {
  const attribution = instance.health.attribution
  if (!attribution) return '无未恢复告警'
  return attribution.current_value === undefined ? attribution.rule_name : `${attribution.rule_name} (${attribution.current_value})`
}

export function lastCollectedAtLabel(collectedAt: string | undefined): string {
  return collectedAt ? new Date(collectedAt).toLocaleString() : '尚无成功采集'
}

export function dataFreshnessLabel(seconds: number | undefined): string {
  if (seconds === undefined) return '未知'
  if (seconds < 60) return `${seconds} 秒前`
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`
  return `${Math.floor(seconds / 3600)} 小时前`
}

/// Agent 失效的读法。**只有失效才有话说**：在线与未安装都不是新鲜度的问题，
/// 给它们一句「Agent 在线」只会把每一行都填满同一句废话。未安装尤其不是失效 ——
/// 没装 Agent 的实例本来就靠服务端直采。
export function agentFailureLabel(status: InstanceAgentStatus): string | undefined {
  switch (status) {
    case 'online':
    case 'not_installed':
      return undefined
    case 'offline':
      return 'Agent 离线'
    case 'permission_denied':
      return 'Agent 权限不足'
    case 'error':
      return 'Agent 异常'
    default:
      return assertNever(status)
  }
}

/// 采集新鲜度这一格。
///
/// 「Agent 离线」本来就是新鲜度失效的一种，所以它不再单独占一列，而是接在新鲜度后面 ——
/// 两列在说同一件事时，留下的那一列要把话说全。
export function collectionFreshnessLabel(instance: Instance): string {
  const freshness = dataFreshnessLabel(instance.data_freshness_seconds)
  const agent = agentFailureLabel(instance.agent_status)
  return agent === undefined ? freshness : `${freshness} · ${agent}`
}

/// 悬停时给出绝对时刻：格子里写的是「多久以前」（扫视时先看的那一个），
/// 二十多个字符的时间戳放不进 127px，但它一个字也不该丢。
export function collectionFreshnessTitle(instance: Instance): string {
  return `${collectionFreshnessLabel(instance)} · ${lastCollectedAtLabel(instance.last_collected_at)}`
}

/// 连接饱和度的读法。百分比就是这一列存在的理由：「连接数 380」在不知道上限是 400
/// 还是 4000 时没有意义，而 500 台里没人记得住每台的 max_connections。
/// 取不到值时写破折号，**不写 0** —— 0% 是一个具体的、令人放心的读数，缺数不是。
export function connectionSaturationLabel(percent: number | null): string {
  if (percent === null) return '—'
  return `${Math.round(percent)}%`
}

/// 使用率的状态档位。只有「快满了」（75%）与「满了」（90%）两档上色，其余中性：
/// 每一行都上色等于没有颜色。
///
/// 连接饱和度与磁盘水位共用这一份阈值，两处读起来才是同一种紧张程度 —— 一个百分比在
/// 一张列表里是黄的、在总览里同样的数字是白的，读者只能记住「这一页的规矩」。
/// 缺数不上色：破折号不是一个令人放心的读数，但它也不是一个越界的读数。
export function usageTone(percent: number | null): StatusTone | undefined {
  if (percent === null) return undefined
  if (percent >= 90) return 'critical'
  if (percent >= 75) return 'warning'
  return undefined
}

/// 从批量趋势响应里取出某台实例的某个**语义位**。找不到就是 undefined —— 调用方据此
/// 显示缺数，而不是拿一条空序列冒充「这台是平的」。
///
/// 按位取而不按指标 ID 取：位到指标的解析在服务端逐台按实例自己的引擎完成（ADR-0001），
/// 所以同一个位在两台不同引擎的实例上可以是两个指标，而这里一句都不用改。
/// 这也是「总览页、实例列表、告警规则模板只能引用语义位」在前端的落点。
export function instanceSlotEntry(
  response: InstancesMetricSeriesResponse | undefined,
  instanceID: string,
  slot: SemanticSlot,
): MetricSeriesEntry | undefined {
  return response?.instances
    .find((entry) => entry.instance_id === instanceID)
    ?.metrics.find((metric) => metric.slot === slot)
}

/// 缩略图要画的那串值。缺数保持 `null`（缩略图在那里断开），不补零 ——
/// 补零会把「没采到」画成「掉到 0」。
export function trendValues(entry: MetricSeriesEntry | undefined): (number | null)[] {
  const points = entry?.series[0]?.points
  if (points === undefined) return []
  return points.map((point) => (typeof point[1] === 'number' ? point[1] : null))
}

/// 序列里最后一个真正有值的点。全是缺数就返回 null，由调用方写成破折号。
export function latestValue(entry: MetricSeriesEntry | undefined): number | null {
  const points = entry?.series[0]?.points
  if (points === undefined) return null
  for (let index = points.length - 1; index >= 0; index -= 1) {
    const value = points[index][1]
    if (typeof value === 'number') return value
  }
  return null
}
