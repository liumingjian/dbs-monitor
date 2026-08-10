import type { operations } from '../../api/schema'

export type MetricID = operations['getMetricSeries']['parameters']['query']['metric'][number]

export type MetricOption = {
  id: MetricID
  label: string
}

export const defaultMetric: MetricID = 'pg.connection.total'

const metricIDs = [
  'pg.availability.reachable',
  'pg.probe.latency_ms',
  'collector.last_success_time',
  'agent.status',
  'host.cpu.usage_percent',
  'host.memory.usage_percent',
  'host.disk.usage_percent',
  'host.disk.free_bytes',
  'host.disk.iops',
  'host.disk.throughput_bytes_per_sec',
  'host.network.bytes_per_sec',
  'pg.connection.total',
  'pg.connection.active',
  'pg.connection.idle_in_transaction',
  'pg.tps',
  'pg.xact.commit_per_sec',
  'pg.xact.rollback_per_sec',
  'pg.tuples.read_per_sec',
  'pg.tuples.write_per_sec',
  'pg.temp.files_per_sec',
  'pg.temp.bytes_per_sec',
  'pg.transaction.long_count',
  'pg.transaction.max_duration_sec',
  'pg.lock.waiting_count',
  'pg.session.blocked_count',
  'pg.query.long_running_count',
  'pg.prepared_xacts.count',
  'pg.replication.role',
  'pg.replication.connection_state',
  'pg.replication.replay_lag_ms',
  'pg.replication.wal_lag_bytes',
  'pg.replication_slot.retained_wal_bytes',
] as const satisfies readonly MetricID[]

export const metricOptions = metricIDs.map((id) => ({ id, label: metricLabel(id) })) satisfies readonly MetricOption[]

export function metricOption(id: MetricID): MetricOption {
  return { id, label: metricLabel(id) }
}

function metricLabel(id: MetricID): string {
  switch (id) {
    case 'pg.availability.reachable': return '实例连通性'
    case 'pg.probe.latency_ms': return '主动探针延迟'
    case 'collector.last_success_time': return '最近成功采集时间'
    case 'agent.status': return 'Agent 状态'
    case 'host.cpu.usage_percent': return 'CPU 使用率'
    case 'host.memory.usage_percent': return '内存使用率'
    case 'host.disk.usage_percent': return '磁盘使用率'
    case 'host.disk.free_bytes': return '磁盘剩余空间'
    case 'host.disk.iops': return '磁盘 IOPS'
    case 'host.disk.throughput_bytes_per_sec': return '磁盘吞吐'
    case 'host.network.bytes_per_sec': return '网络流量'
    case 'pg.connection.total': return '总连接数'
    case 'pg.connection.active': return '活跃连接数'
    case 'pg.connection.idle_in_transaction': return 'idle in transaction 连接数'
    case 'pg.tps': return 'TPS'
    case 'pg.xact.commit_per_sec': return '提交速率'
    case 'pg.xact.rollback_per_sec': return '回滚速率'
    case 'pg.tuples.read_per_sec': return '读行速率'
    case 'pg.tuples.write_per_sec': return '写行速率'
    case 'pg.temp.files_per_sec': return '临时文件数量速率'
    case 'pg.temp.bytes_per_sec': return '临时文件写入速率'
    case 'pg.transaction.long_count': return '长事务数量'
    case 'pg.transaction.max_duration_sec': return '最长事务时长'
    case 'pg.lock.waiting_count': return '锁等待数量'
    case 'pg.session.blocked_count': return '被阻塞会话数'
    case 'pg.query.long_running_count': return '长查询数量'
    case 'pg.prepared_xacts.count': return '2PC 数量'
    case 'pg.replication.role': return '实例角色'
    case 'pg.replication.connection_state': return '复制连接状态'
    case 'pg.replication.replay_lag_ms': return '复制回放延迟'
    case 'pg.replication.wal_lag_bytes': return 'WAL 延迟字节数'
    case 'pg.replication_slot.retained_wal_bytes': return 'Replication slot WAL 积压'
    default: return assertNever(id)
  }
}

function assertNever(value: never): never {
  throw new Error(`unhandled metric ID: ${value}`)
}
