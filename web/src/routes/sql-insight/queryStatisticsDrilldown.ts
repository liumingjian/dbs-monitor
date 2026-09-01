import { withSessionTab } from '../instances.$id/sessionTabs'
import { defaultTimeRange } from '../instances.$id/timeRange'

/// 下钻到该实例的查询统计：会话与阻塞页的「查询统计排行」标签，带一份默认时间范围。
/// 地址由路由器按那一页的契约拼，这里只负责给出一份合法的 search 对象。
///
/// 这是本页私有的一件：它知道的是「点这一行去哪儿」，那是 SQL 洞察页自己的动线。
/// 读法（文本、耗时、行的身份）在 `domain/topSql.ts`，因为总览第五块也要用同一份；
/// 去处不上浮到 domain/ —— domain/ 不认识路由，也不该认识某一页的页签词汇。
export function queryStatisticsDrilldown() {
  return withSessionTab(defaultTimeRange(), 'query-statistics')
}
