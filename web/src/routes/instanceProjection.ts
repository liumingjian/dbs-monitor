import type { components } from '../api/schema'

type Instance = components['schemas']['Instance']

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
