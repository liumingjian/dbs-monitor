import type { components } from '../api/schema'

export type InstanceEngine = components['schemas']['InstanceEngine']

/// 接入表单里可选的引擎，顺序就是下拉框的顺序。
/// 目前只有 PostgreSQL；MySQL 接入时在生成的枚举里多一项，这里跟着多一项。
export const instanceEngines = ['POSTGRESQL'] as const satisfies readonly InstanceEngine[]

function assertNever(value: never): never {
  throw new Error(`unhandled instance engine: ${String(value)}`)
}

/// 引擎的展示名。产品名的大小写是产品自己的写法，不是把枚举值转成小写能得到的。
export function instanceEngineLabel(engine: InstanceEngine): string {
  switch (engine) {
    case 'POSTGRESQL':
      return 'PostgreSQL'
    default:
      return assertNever(engine)
  }
}

/// Bootstrap database 在两个表单里的说明文案。它只用来建立连接，
/// **不限定被监控的范围** —— 这条连接下的所有库都归这台实例。
export const bootstrapDatabaseLabel = 'Bootstrap 数据库'
export const bootstrapDatabaseHelperText =
  '建立连接用的库名，不限定监控范围：这条连接下的所有库都归这台实例。留空则连 postgres。'
