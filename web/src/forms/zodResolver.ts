import type { FieldError, FieldErrors, FieldValues, Resolver } from 'react-hook-form'
import type { ZodType } from 'zod'

/// 把一个 zod schema 变成 react-hook-form 的 `resolver`。
///
/// 手写而不是装 `@hookform/resolvers`：这是一个 unwrap + 改形状的适配层，
/// 官方包在这件事之外还带一堆本仓库用不到的 schema 库适配。
///
/// `ZodType<TValues, TValues>` 要求 schema 的输入与输出同型 —— 也就是**不要在 schema 里写
/// `transform` / `default`**。表单值就是提交值，避免「校验通过的东西和发出去的东西不是一件事」。
/// 归一化（trim、空串转 undefined）放在提交处，看得见。
///
/// 只保留每个字段的第一条 issue，与 react-hook-form 默认的 `criteriaMode: 'firstError'` 一致。
/// 路径按段展开成嵌套对象；数组下标不支持（本仓库的表单字段都是扁平的，需要时再补）。
export function zodResolver<TValues extends FieldValues>(schema: ZodType<TValues, TValues>): Resolver<TValues> {
  return (values) => {
    const parsed = schema.safeParse(values)
    if (parsed.success) {
      return { values: parsed.data, errors: {} }
    }

    const errors: Record<string, unknown> = {}
    for (const issue of parsed.error.issues) {
      const path = issue.path.map((segment) => String(segment))
      if (path.length === 0) {
        continue
      }
      assignFirst(errors, path, { type: issue.code, message: issue.message })
    }
    return { values: {}, errors: errors as FieldErrors<TValues> }
  }
}

function assignFirst(target: Record<string, unknown>, path: string[], error: FieldError): void {
  const [head, ...rest] = path
  if (head === undefined) {
    return
  }
  if (rest.length === 0) {
    if (!(head in target)) {
      target[head] = error
    }
    return
  }
  const existing = target[head]
  const branch = isRecord(existing) ? existing : {}
  target[head] = branch
  assignFirst(branch, rest, error)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}
