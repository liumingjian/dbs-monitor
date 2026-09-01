package httpapi

import (
	"sort"
	"strconv"
	"strings"

	"github.com/liumingjian/dbs-monitor/internal/api"
)

// 实例列表的筛选、排序与分页。
//
// 为什么在 Go 里而不是 SQL 里：健康档位、正交标记与告警计数都不是 instance 表上的列，
// 它们由 alerting.RollupInstanceHealth 那台状态机在读取时算出来（告警、维护窗口、能力
// 快照、暂停状态一起参与）。把筛选下推到 SQL 就要在 SQL 里再写一遍那台状态机，两份实现
// 迟早各说各的。一次查询把当前实例读出来、投影完再筛，代价是 500 行的内存排序 ——
// 这是本轮机群规模的上限，而它换回来的是「列表看到什么就能筛什么」只有一个定义。
//
// 浏览器侧因此不再持有全量实例：它拿到的就是当页。

const (
	defaultInstancePage     = 1
	defaultInstancePageSize = 50
	maxInstancePageSize     = 500
)

type instanceListQuery struct {
	page       int
	pageSize   int
	search     string
	engines    []api.InstanceEngine
	statuses   []api.HealthStatus
	flags      []api.InstanceFlag
	severities []api.AlertSeverity
	sort       api.InstanceListSort
}

// newInstanceListQuery 把可选的查询参数收敛成一份取值确定的筛选条件。
// 生成的参数类型每一项都是指针，缺省值只在这里补一次。
func newInstanceListQuery(params api.ListInstancesParams) instanceListQuery {
	query := instanceListQuery{
		page:     defaultInstancePage,
		pageSize: defaultInstancePageSize,
		sort:     api.InstanceSortHealth,
	}
	if params.Page != nil && *params.Page > 0 {
		query.page = *params.Page
	}
	if params.PageSize != nil && *params.PageSize > 0 {
		query.pageSize = *params.PageSize
		if query.pageSize > maxInstancePageSize {
			query.pageSize = maxInstancePageSize
		}
	}
	if params.Q != nil {
		query.search = strings.ToLower(strings.TrimSpace(*params.Q))
	}
	if params.Engine != nil {
		query.engines = *params.Engine
	}
	if params.Status != nil {
		query.statuses = *params.Status
	}
	if params.Flags != nil {
		query.flags = *params.Flags
	}
	if params.Severity != nil {
		query.severities = *params.Severity
	}
	if params.Sort != nil && *params.Sort != "" {
		query.sort = *params.Sort
	}
	return query
}

// selectInstances 返回当页的实例与筛选后的总数。
//
// 筛选组之间是「与」；组内除标记外都是「或」。标记是「与」，因为「维护中且已忽略」
// 才是读者勾两个标记时想问的问题，「维护中或已忽略」等于没筛。
func selectInstances(instances []api.Instance, query instanceListQuery) ([]api.Instance, int) {
	matched := make([]api.Instance, 0, len(instances))
	for _, item := range instances {
		if instanceMatches(item, query) {
			matched = append(matched, item)
		}
	}
	sortInstances(matched, query.sort)
	total := len(matched)
	start := (query.page - 1) * query.pageSize
	if start >= total {
		// 超出末页返回空的一页而不是最后一页：地址栏里的页码是使用者给的事实，
		// 悄悄换成另一页会让「把这个地址发给同事」得到两种结果。
		return []api.Instance{}, total
	}
	end := start + query.pageSize
	if end > total {
		end = total
	}
	return matched[start:end], total
}

func instanceMatches(item api.Instance, query instanceListQuery) bool {
	if query.search != "" && !instanceMatchesSearch(item, query.search) {
		return false
	}
	if len(query.engines) > 0 && !containsEngine(query.engines, item.Engine) {
		return false
	}
	if len(query.statuses) > 0 && !containsStatus(query.statuses, item.Health.Status) {
		return false
	}
	for _, flag := range query.flags {
		if !instanceHasFlag(item, flag) {
			return false
		}
	}
	if len(query.severities) > 0 {
		matched := false
		for _, severity := range query.severities {
			if instanceHasSeverity(item, severity) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// instanceMatchesSearch 命中实例名或地址。地址不再单独占一列（500 台时没人靠 IP 认机器），
// 但它仍然是「我知道这台机器的 IP，想找到它」这条动线唯一的入口，所以留在搜索索引里。
func instanceMatchesSearch(item api.Instance, search string) bool {
	address := item.Host + ":" + strconv.Itoa(item.Port)
	return strings.Contains(strings.ToLower(item.Name), search) ||
		strings.Contains(strings.ToLower(address), search)
}

func containsEngine(engines []api.InstanceEngine, engine api.InstanceEngine) bool {
	for _, candidate := range engines {
		if candidate == engine {
			return true
		}
	}
	return false
}

func containsStatus(statuses []api.HealthStatus, status api.HealthStatus) bool {
	for _, candidate := range statuses {
		if candidate == status {
			return true
		}
	}
	return false
}

func instanceHasFlag(item api.Instance, flag api.InstanceFlag) bool {
	switch flag {
	case api.InstanceFlagNoData:
		return item.Health.Flags.NoData
	case api.InstanceFlagMaintenance:
		return item.Health.Flags.InMaintenance
	case api.InstanceFlagRecentlyRecovered:
		return item.Health.Flags.RecentlyRecovered
	case api.InstanceFlagIgnored:
		return item.Health.Flags.Ignored > 0
	case api.InstanceFlagConfigurationMissing:
		return item.Health.Flags.ConfigurationMissing > 0
	case api.InstanceFlagStaleData:
		return instanceDataIsStale(item)
	case api.InstanceFlagAgentOffline:
		return item.AgentStatus == api.InstanceAgentOffline
	default:
		return false
	}
}

// staleDataAfterSeconds 是「采集落后到该有人管了」的界。十分钟：本轮最慢的常规采集任务
// 是分钟级的，连着漏十次才到这里，不会把一次抖动叫成烂掉。
const staleDataAfterSeconds = 600

// instanceDataIsStale 是总览「数据不新鲜」与列表 STALE_DATA 筛选共用的那一个判定。
//
// 从来没采到过也算不新鲜——它连一个基准都还没有，比落后一小时更该被看见。
// 已暂停的不算：暂停是有人按下的开关，不是悄悄烂掉，它由「采集暂停数」单独计。
func instanceDataIsStale(item api.Instance) bool {
	if item.Health.Status == api.HealthPaused {
		return false
	}
	if item.DataFreshnessSeconds == nil {
		return true
	}
	return *item.DataFreshnessSeconds > staleDataAfterSeconds
}

func instanceHasSeverity(item api.Instance, severity api.AlertSeverity) bool {
	switch severity {
	case api.Critical:
		return item.Health.Counts.Critical > 0
	case api.Warning:
		return item.Health.Counts.Warning > 0
	case api.Info:
		return item.Health.Counts.Info > 0
	default:
		return false
	}
}

// healthRank 决定「谁排在前面要人处理」。与前端的健康档位顺序是同一套次序。
func healthRank(status api.HealthStatus) int {
	switch status {
	case api.HealthCritical:
		return 5
	case api.HealthWarning:
		return 4
	case api.HealthUnknown:
		return 3
	case api.HealthHealthy:
		return 2
	case api.HealthPaused:
		return 1
	default:
		return 0
	}
}

// sortInstances 排序。任何排序的最后两级都是名称与 id：同序值的行如果每次查询顺序不同，
// 翻页就会重复或漏掉行 —— 这是分页最容易出、又最难看出来的错。
func sortInstances(instances []api.Instance, order api.InstanceListSort) {
	sort.SliceStable(instances, func(left, right int) bool {
		first, second := instances[left], instances[right]
		switch order {
		case api.InstanceSortName:
			if first.Name != second.Name {
				return first.Name < second.Name
			}
		case api.InstanceSortNameDescending:
			if first.Name != second.Name {
				return first.Name > second.Name
			}
		case api.InstanceSortStalest:
			leftStaleness, rightStaleness := instanceStaleness(first), instanceStaleness(second)
			if leftStaleness != rightStaleness {
				return leftStaleness > rightStaleness
			}
		case api.InstanceSortHealth:
			if healthRank(first.Health.Status) != healthRank(second.Health.Status) {
				return healthRank(first.Health.Status) > healthRank(second.Health.Status)
			}
		default:
			if healthRank(first.Health.Status) != healthRank(second.Health.Status) {
				return healthRank(first.Health.Status) > healthRank(second.Health.Status)
			}
		}
		if first.Name != second.Name {
			return first.Name < second.Name
		}
		return first.Id.String() < second.Id.String()
	})
}

// instanceStaleness 是「落后了多少秒」。从来没采到过的排在最前面：它不是「很新鲜」，
// 而是连一个基准都还没有。缺数不折算成 0。
func instanceStaleness(item api.Instance) int {
	if item.DataFreshnessSeconds == nil {
		return int(^uint(0) >> 1)
	}
	return *item.DataFreshnessSeconds
}
