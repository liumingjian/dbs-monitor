// Package dbengine holds the one authoritative spelling of "engine" — the database product a
// piece of the platform is talking about.
//
// 引擎这个词在两处出现，它们必须是同一个词：实例侧（一台被监控实例跑的产品，#216）与指标目录侧
// （目录每一行属于哪个产品，#215）。两处的**取值集合不同**，但类型只有一个：
//
//   - 实例只能取 InstanceEngines()——实例永远不会是 AGNOSTIC，一条连接总是连到某个具体产品上。
//     这条不变量在三处强制：DDL 的 CHECK、Engine.ValidForInstance、OpenAPI 的 InstanceEngine。
//   - 目录可以取 CatalogEngines()，多一个 Agnostic：host.* / agent.* / collector.* 这类指标挂在
//     实例上，量的却是主机与采集自身，不属于任何数据库产品。
package dbengine

import "slices"

// Engine is the database product. 新接入一个产品（MySQL）时，只在这里加一个常量、把它放进
// InstanceEngines() 与 CatalogEngines()，再同步 DDL 的 CHECK 与 OpenAPI 的两个枚举。
type Engine string

const (
	// PostgreSQL 是目前唯一接入的产品。
	PostgreSQL Engine = "POSTGRESQL"
	// Agnostic 只对指标目录行有意义，实例取不到这个值。
	Agnostic Engine = "AGNOSTIC"
)

func (engine Engine) String() string {
	return string(engine)
}

// InstanceEngines 按接入表单的展示顺序列出实例可选的引擎。
func InstanceEngines() []Engine {
	return []Engine{PostgreSQL}
}

// CatalogEngines 是指标目录里允许出现的引擎全集：实例侧的全部，再加 Agnostic。
func CatalogEngines() []Engine {
	return append(InstanceEngines(), Agnostic)
}

// ValidForInstance 报告 engine 能不能作为一台实例的引擎。空值与 Agnostic 都不行——
// 前者要调用方先决定默认值，后者根本不是一个可连接的产品。
func (engine Engine) ValidForInstance() bool {
	return slices.Contains(InstanceEngines(), engine)
}

// ValidForCatalog 报告 engine 能不能出现在指标目录的一行上。
func (engine Engine) ValidForCatalog() bool {
	return slices.Contains(CatalogEngines(), engine)
}
