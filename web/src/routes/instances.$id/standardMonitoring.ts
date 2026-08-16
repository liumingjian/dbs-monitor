import type { MetricID } from './metricOptions'

export type StandardMonitoringChart = {
  key: string
  title: string
  description: string
  metrics: readonly [MetricID, ...MetricID[]]
  drilldown?: 'long-query-samples'
}

export type StandardMonitoringGroup = {
  key: 'resource' | 'database' | 'replication'
  title: string
  charts: readonly StandardMonitoringChart[]
}

export const standardMonitoringGroups: readonly StandardMonitoringGroup[] = [
  {
    key: 'resource',
    title: '资源指标',
    charts: [
      { key: 'cpu', title: 'CPU', description: '实例实际可用 CPU 资源的使用率。', metrics: ['host.cpu.usage_percent'] },
      { key: 'memory', title: '内存', description: '扣除可回收页缓存后的工作集内存使用率。', metrics: ['host.memory.usage_percent'] },
      { key: 'disk-space', title: '磁盘空间', description: 'PostgreSQL 数据目录所在文件系统的使用率与剩余空间。', metrics: ['host.disk.usage_percent', 'host.disk.free_bytes'] },
      { key: 'disk-io', title: '磁盘 IO / IOPS', description: '实例相关磁盘设备的操作次数与吞吐速率。', metrics: ['host.disk.iops', 'host.disk.throughput_bytes_per_sec'] },
      { key: 'network', title: '网络流量', description: '实例相关网卡的收发字节速率。', metrics: ['host.network.bytes_per_sec'] },
    ],
  },
  {
    key: 'database',
    title: '数据库指标',
    charts: [
      { key: 'connections', title: '连接数', description: '实例当前总连接数，包含监控账号连接。', metrics: ['pg.connection.total'] },
      { key: 'active-connections', title: '活跃连接数', description: '当前处于 active 状态的业务连接数。', metrics: ['pg.connection.active'] },
      { key: 'idle-in-transaction', title: 'idle in transaction', description: '事务已开启但当前空闲的业务连接数。', metrics: ['pg.connection.idle_in_transaction'] },
      { key: 'tps', title: 'TPS', description: '实例级提交与回滚事务的合计速率。', metrics: ['pg.tps'] },
      { key: 'commit-rollback', title: '提交 / 回滚', description: '事务提交速率与回滚速率的对比。', metrics: ['pg.xact.commit_per_sec', 'pg.xact.rollback_per_sec'] },
      { key: 'tuples', title: '读写行数', description: '数据库读取与写入行数的变化速率。', metrics: ['pg.tuples.read_per_sec', 'pg.tuples.write_per_sec'] },
      { key: 'temp-files', title: '临时文件', description: '临时文件生成速率与写入字节速率。', metrics: ['pg.temp.files_per_sec', 'pg.temp.bytes_per_sec'] },
      { key: 'long-transactions', title: '长事务', description: '超过阈值的事务数量及当前最长持续时间。', metrics: ['pg.transaction.long_count', 'pg.transaction.max_duration_sec'] },
      { key: 'lock-waits', title: '锁等待', description: '当前正在等待锁的业务会话数。', metrics: ['pg.lock.waiting_count'] },
      { key: 'blocked-sessions', title: '阻塞会话', description: '当前被其他会话阻塞的业务会话数。', metrics: ['pg.session.blocked_count'] },
      { key: 'long-queries', title: '长查询数量', description: '采样时仍在执行且超过阈值的查询数量。', metrics: ['pg.query.long_running_count'], drilldown: 'long-query-samples' },
      { key: 'prepared-xacts', title: '2PC', description: '当前尚未完成的预备事务数量。', metrics: ['pg.prepared_xacts.count'] },
    ],
  },
  {
    key: 'replication',
    title: '复制指标',
    charts: [
      { key: 'role', title: '实例角色', description: '实例当前在复制拓扑中的主库、备库或单实例角色。', metrics: ['pg.replication.role'] },
      { key: 'connection-state', title: '复制连接状态', description: '复制发送端或接收端的连接状态。', metrics: ['pg.replication.connection_state'] },
      { key: 'wal-lag', title: 'WAL 延迟', description: '基于 WAL 位点差计算的复制延迟字节数，也是告警口径。', metrics: ['pg.replication.wal_lag_bytes'] },
      { key: 'replay-lag', title: '复制回放时间延迟', description: '回放时间延迟仅供辅助判断，不用于告警。', metrics: ['pg.replication.replay_lag_ms'] },
      { key: 'slot-wal', title: 'Replication slot 积压', description: '复制槽保留但尚未消费的 WAL 字节数。', metrics: ['pg.replication_slot.retained_wal_bytes'] },
    ],
  },
]

const standardMonitoringCharts = standardMonitoringGroups.flatMap((group) => group.charts)

export const standardMonitoringMetricIDs: MetricID[] = standardMonitoringCharts.flatMap((chart) => chart.metrics)

export function findStandardMonitoringChart(metric: MetricID | undefined): StandardMonitoringChart | undefined {
  if (metric === undefined) return undefined
  return standardMonitoringCharts.find((chart) => chart.metrics.includes(metric))
}
