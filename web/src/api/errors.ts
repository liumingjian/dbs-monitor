import type { FieldPath, FieldValues, UseFormSetError } from 'react-hook-form'

/// 服务端返回的一条字段级错误，原样保留线上的 `{ field, message }` 形状。
/// 服务端每个字段最多返回一条消息（见 `internal/httpapi/validation.go`），
/// 所以这里是一条消息而不是消息数组。
export type ApiFieldError = {
  field: string
  message: string
}

export function apiErrorMessage(error: unknown, fallback: string): string {
  if (!isRecord(error) || !isRecord(error.error) || typeof error.error.message !== 'string') {
    return fallback
  }
  return error.error.message
}

/// 解析 `{ error: { field_errors: [{ field, message }] } }`。
/// 总函数：形状不认识就返回 `[]`，不抛。
export function apiFieldErrors(error: unknown): ApiFieldError[] {
  if (!isRecord(error) || !isRecord(error.error) || !Array.isArray(error.error.field_errors)) {
    return []
  }
  return error.error.field_errors.flatMap((item) => {
    if (!isRecord(item) || typeof item.field !== 'string' || typeof item.message !== 'string') {
      return []
    }
    return [{ field: item.field, message: item.message }]
  })
}

/// 把服务端的字段级错误写进 react-hook-form，使错误落在对应输入框下方而不是页面顶部。
///
/// `fields` 是本表单认识的字段名清单（写成 `as const satisfies readonly FieldPath<T>[]`，
/// 让类型系统盯住它与表单值类型的一致性）。**清单之外的字段名一律丢弃** —— 与迁移前一致，
/// 也是必须的：`setError` 一个表单里没有的名字会挂出一条永远显示不出来、也永远清不掉的错误。
///
/// 第一个命中的字段会被聚焦（`shouldFocus`），前提是该字段用 `register` / `Controller`
/// 接上了真实控件的 ref。
///
/// 返回真正写进表单的字段名。**空数组 = 这次失败没有任何字段级信息**，调用方据此决定
/// 是否再显示一条整表单的错误消息 —— 不要两边都显示。
export function applyApiFieldErrors<TValues extends FieldValues>(
  error: unknown,
  fields: readonly FieldPath<TValues>[],
  setError: UseFormSetError<TValues>,
): FieldPath<TValues>[] {
  const applied: FieldPath<TValues>[] = []
  for (const item of apiFieldErrors(error)) {
    const field = fields.find((name) => name === item.field)
    if (field === undefined || applied.includes(field)) {
      continue
    }
    setError(field, { type: 'server', message: item.message }, { shouldFocus: applied.length === 0 })
    applied.push(field)
  }
  return applied
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}
