import { Link } from '@carbon/react'
import type { components } from '../api/schema'
import { Icon } from '../primitives/Icon'
import { NotificationBar } from '../primitives/NotificationBar'
import './UnavailabilityBlock.css'

export type Unavailability = components['schemas']['Unavailability']

type Copy = { title: string; description: string; action: string }
type Destinations = { current: string; collection: string }
type UnavailabilityBlockProps = {
  code: Unavailability
  href: string
  detail?: string
}

export function unavailabilityCopy(code: Unavailability): Copy {
  switch (code) {
    case 'NO_SAMPLES_YET': return { title: '等待首个样本', description: '指标已启用，尚未完成首次采集。', action: '稍后刷新' }
    case 'NO_DATA_IN_RANGE': return { title: '所选范围没有数据', description: '该时间范围内没有采集点。', action: '扩大时间范围' }
    case 'STALE': return { title: '数据已过期', description: '最后一次成功数据已超过新鲜度门槛。', action: '检查采集状态' }
    case 'COLLECTION_PAUSED': return { title: '采集已暂停', description: '本实例当前处于暂停采集状态。', action: '查看采集设置' }
    case 'COLLECTION_FAILED': return { title: '采集失败', description: '最近一次采集未能完成。', action: '查看失败原因' }
    case 'DB_UNREACHABLE': return { title: '数据库不可达', description: '平台无法连接目标 PostgreSQL。', action: '检查网络与连接信息' }
    case 'AGENT_OFFLINE': return { title: 'Agent 离线', description: '平台未在门槛内收到 Agent 上报。', action: '检查 Agent 服务' }
    case 'PERMISSION_DENIED': return { title: '权限不足', description: '监控账号缺少读取该指标的权限。', action: '补齐监控权限' }
    case 'EXTENSION_MISSING': return { title: '扩展未安装', description: '该指标依赖的 PostgreSQL 扩展不存在。', action: '安装所需扩展' }
    case 'FEATURE_DISABLED': return { title: '功能未启用', description: '目标数据库未启用该指标依赖的功能。', action: '启用数据库功能' }
    case 'VERSION_UNSUPPORTED': return { title: '版本不支持', description: '目标 PostgreSQL 版本不提供该指标。', action: '查看支持矩阵' }
    case 'NOT_APPLICABLE_ROLE': return { title: '当前角色不适用', description: '该指标不适用于当前主备角色或拓扑。', action: '查看实例角色' }
    case 'COUNTER_RESET': return { title: '计数器已重置', description: '该点无法计算可靠速率，因此保留为断点。', action: '等待下一个采集周期' }
    default: return assertNever(code)
  }
}

export function unavailabilityHref(code: Unavailability, destinations: Destinations): string {
  switch (code) {
    case 'NO_SAMPLES_YET':
    case 'NO_DATA_IN_RANGE':
    case 'COUNTER_RESET':
      return destinations.current
    case 'STALE':
    case 'COLLECTION_PAUSED':
    case 'COLLECTION_FAILED':
    case 'DB_UNREACHABLE':
    case 'AGENT_OFFLINE':
    case 'PERMISSION_DENIED':
    case 'EXTENSION_MISSING':
    case 'FEATURE_DISABLED':
    case 'VERSION_UNSUPPORTED':
    case 'NOT_APPLICABLE_ROLE':
      return destinations.collection
    default:
      return assertNever(code)
  }
}

/// 不可用说明块。**「缺数不是 0」在视图层的落点**：指标取不到值时这里出现的是一句
/// 「为什么没有」加一个去处，而不是一个 0 —— 缺数从不被画成一条贴地的线。
///
/// 十三个原因码一律走中性的说明档，不按严重度上色：这块讲的是「这里为什么没有数据」，
/// 不是对实例健康的判断，给它状态色会和真正的告警抢注意力。
///
/// 去处是一个真链接（`<a href>`），不是按钮：它就是地址切换，中键新开与复制链接都得留着。
/// 通知条的正文区被组件库限定为非交互内容，所以链接另起一行放在通知外面。
export function UnavailabilityBlock({ code, href, detail }: UnavailabilityBlockProps) {
  const copy = unavailabilityCopy(code)
  return <div className="dbs-unavailability">
    <NotificationBar tone="info" title={copy.title}>
      <span className="dbs-unavailability__line">{copy.description}</span>
      {detail && <span className="dbs-unavailability__line">{detail}</span>}
    </NotificationBar>
    <Link className="dbs-unavailability__action" href={href} renderIcon={Icon.glyph.arrowRight}>{copy.action}</Link>
  </div>
}

function assertNever(value: never): never {
  throw new Error(`unhandled unavailability: ${value}`)
}
