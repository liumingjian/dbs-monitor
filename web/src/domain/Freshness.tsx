export function isFresh(dataUpdatedAt: number, now: number, collectionInterval: number): boolean {
  return now - dataUpdatedAt <= collectionInterval * 2.5
}

export function Freshness({ dataUpdatedAt, collectionInterval }: { dataUpdatedAt: number; collectionInterval: number }) {
  const fresh = isFresh(dataUpdatedAt, Date.now(), collectionInterval)
  return <span aria-label={fresh ? '数据新鲜' : '数据已过期'}>{fresh ? '刚刚更新' : '数据已过期'}</span>
}
