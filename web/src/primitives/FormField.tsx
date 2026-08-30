import { useId } from 'react'
import type { ReactNode } from 'react'
import './FormField.css'

/** 交给控件的三件事：id、描述文本的 id、是否处于错误态。 */
export type FormFieldControl = {
  id: string
  describedBy: string | undefined
  invalid: boolean
}

export type FormFieldProps = {
  /**
   * 字段标签。控件自带标签时（Carbon 的 `TextInput labelText`）就别给这个，
   * 否则读屏会听到两遍；这时 FormField 只承担「消息槽 + id 接线」。
   */
  label?: ReactNode
  /** 常态下的提示文字，出现在控件下方。 */
  helperText?: ReactNode
  /**
   * 错误文案。**给了就是错误态** —— 校验错误与服务端返回的字段错误都落在这里，
   * 显示在对应字段下方，而不是页面顶部。
   */
  errorText?: ReactNode
  /** 是否必填。只画标记，不做校验 —— 展示层不认识校验规则。 */
  required?: boolean
  /** 渲染控件。参数照原样接到控件的 `id` / `aria-describedby` / `invalid` 上。 */
  children: (control: FormFieldControl) => ReactNode
  className?: string
}

/// 表单字段外壳：标签、控件、提示与错误文案的统一版式与无障碍接线。
///
/// 用渲染函数而不是直接包 children，是因为 `id` 与 `aria-describedby` 必须落到
/// 真正的控件元素上；由外壳生成、交给调用方接线，是唯一能保证这条链不断的形状，
/// 而且不绑定任何具体的输入控件实现。
export function FormField({ label, helperText, errorText, required = false, children, className }: FormFieldProps) {
  const baseId = useId()
  const controlId = `${baseId}-control`
  const helperId = `${baseId}-helper`
  const errorId = `${baseId}-error`

  const describedBy =
    [errorText === undefined ? undefined : errorId, helperText === undefined ? undefined : helperId]
      .filter(Boolean)
      .join(' ') || undefined

  return (
    <div className={['dbs-form-field', className].filter(Boolean).join(' ')} data-invalid={errorText === undefined ? undefined : 'true'}>
      {label !== undefined && (
        <label className="dbs-form-field__label dbs-caption" htmlFor={controlId}>
          {label}
          {required && (
            <>
              <span aria-hidden="true">*</span>
              <span className="dbs-form-field__sr">必填</span>
            </>
          )}
        </label>
      )}
      <div className="dbs-form-field__control">
        {children({ id: controlId, describedBy, invalid: errorText !== undefined })}
      </div>
      {helperText !== undefined && (
        <p className="dbs-form-field__helper dbs-caption" id={helperId}>
          {helperText}
        </p>
      )}
      {errorText !== undefined && (
        <p className="dbs-form-field__error dbs-caption" id={errorId}>
          {errorText}
        </p>
      )}
    </div>
  )
}
