import { useEffect, useState } from 'react'

export function isFresh(dataUpdatedAt: number, now: number, collectionInterval: number): boolean {
  return now - dataUpdatedAt <= collectionInterval * 2.5
}

export function freshnessLabel(dataUpdatedAt: number, now: number, collectionInterval: number): string {
  const seconds = Math.max(0, Math.round((now - dataUpdatedAt) / 1000))
  if (!isFresh(dataUpdatedAt, now, collectionInterval)) return `已过期 · ${elapsedLabel(seconds)}未更新`
  return seconds < 5 ? '刚刚更新' : `${elapsedLabel(seconds)}前更新`
}

/** The app's one elapsed-time vocabulary: freshness and alert duration read the same. */
export function elapsedLabel(seconds: number): string {
  if (seconds < 60) return `${seconds} 秒`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes} 分钟`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours} 小时`
  return `${Math.floor(hours / 24)} 天`
}

export function Freshness({ dataUpdatedAt, collectionInterval }: { dataUpdatedAt: number; collectionInterval: number }) {
  // Evaluated only at render, the label sat frozen between polls and claimed
  // "刚刚更新" long after the data went stale. A tick makes it tell the truth.
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [])

  const fresh = isFresh(dataUpdatedAt, now, collectionInterval)
  const label = freshnessLabel(dataUpdatedAt, now, collectionInterval)
  return <span
    className="freshness"
    data-fresh={fresh}
    aria-label={fresh ? '数据新鲜' : '数据已过期'}
    title={`最后一次成功获取：${new Date(dataUpdatedAt).toLocaleString()}`}
  >{label}</span>
}
