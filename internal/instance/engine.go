package instance

import (
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

// Engine 是一台被监控实例运行的数据库产品。
//
// 它是实例模型的一部分，不是指标目录的一部分：一条连接只跑一种产品，
// 而引擎决定哪些采集任务适用、哪些指标存在，也决定 bootstrap database 有没有意义。
// 接入时选定，之后不可改——改了等于换了一台实例，历史数据不再成立。
type Engine string

const (
	// EnginePostgreSQL 是目前唯一接入的引擎。MySQL 会作为第二个值加进来，
	// 加的时候只需要在这里、DDL 的 CHECK 与 OpenAPI 的 InstanceEngine 三处各加一项。
	EnginePostgreSQL Engine = "POSTGRESQL"
)

// Engines 按接入表单的展示顺序列出所有可选引擎。
func Engines() []Engine {
	return []Engine{EnginePostgreSQL}
}

// Valid 报告 engine 是不是一个已知引擎。空值不是已知引擎——调用方要先决定默认值。
func (engine Engine) Valid() bool {
	for _, known := range Engines() {
		if engine == known {
			return true
		}
	}
	return false
}

// DefaultBootstrapDatabase 是操作者没填 bootstrap database 时该用的库名。
// PostgreSQL 建连接必须落到某个库上，约定用 postgres；MySQL 没有这个概念，留空。
func (engine Engine) DefaultBootstrapDatabase() string {
	switch engine {
	case EnginePostgreSQL:
		return "postgres"
	default:
		return ""
	}
}

// ResolveBootstrapDatabase 把表单里填的库名归一成真正拿去建连接、也真正落库的值：
// 去掉两头空白，留空就退回引擎的默认库。
//
// bootstrap database 只用来建立连接，**不限定被监控的范围**——一条连接下的所有库
// 都被这台实例监控，库只是指标的一个维度。
func ResolveBootstrapDatabase(engine Engine, requested string) string {
	trimmed := strings.TrimSpace(requested)
	if trimmed != "" {
		return trimmed
	}
	return engine.DefaultBootstrapDatabase()
}

// BootstrapDatabaseColumn 把归一后的 bootstrap database 编成列值：空串意味着这个引擎
// 没有「连到哪个库」的概念（MySQL 就没有），落 NULL 而不是空字符串。
func BootstrapDatabaseColumn(database string) pgtype.Text {
	if database == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: database, Valid: true}
}
