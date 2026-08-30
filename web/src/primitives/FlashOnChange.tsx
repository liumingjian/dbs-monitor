import { useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import './FlashOnChange.css'

/**
 * 轮询页面的一个老问题：屏幕每 10–30 秒整块重画一次，而真正变了的往往只有一两个数。
 * 盯着看一小时的人分辨不出「刷新了但没变」和「变了」，于是只能反复重读整块面板。
 *
 * 这一件就是 DESIGN.md「Motion」里唯一被允许的数值动效：**刷新后确实变了的值闪一次**
 * 选中底色，400ms，然后就没了。它承载信息，因此不算装饰。
 *
 * 刻意不做的事（规范明令禁止）：数字不滚动、不做入场、不放大、不描边生长。
 * 变化只由背景色的一次淡出表达，字形从头到尾不动 —— 数值在动的界面读不了数。
 *
 * 判定用的是**渲染出来的那个值**（多半是已经格式化好的字符串），不是原始样本：
 * 42.0001 → 42.0002 在屏幕上还是 “42%”，闪一下只会是噪声。
 *
 * `prefers-reduced-motion: reduce` 时整条动画不发射（媒体查询包着，不是靠 duration 抹掉），
 * 数值照常更新，只是不闪。
 */
export function FlashOnChange({
  value,
  children,
  className,
}: {
  /** 判定「变了没有」的依据。给已经格式化好的展示值。 */
  value: string | number | null | undefined
  /** 要闪的内容；不给就直接渲染 `value`。 */
  children?: ReactNode
  className?: string
}) {
  // 两段式而不是一个布尔：同一个 `animation-name` 从「不匹配」变成「匹配」才会重放，
  // 而连续两次变化之间元素一直匹配着同一条规则 —— 布尔开关下第二次变化不会再闪。
  // 交替换名字，每次变化都是一条「新」动画，不必等 animationend，也不重挂元素
  // （重挂会把焦点与选区甩掉）。
  const [phase, setPhase] = useState<'idle' | 'a' | 'b'>('idle')
  const previous = useRef(value)

  useEffect(() => {
    if (Object.is(previous.current, value)) return
    previous.current = value
    setPhase((current) => (current === 'a' ? 'b' : 'a'))
  }, [value])

  return (
    <span
      className={['dbs-flash-on-change', className].filter(Boolean).join(' ')}
      data-flash={phase === 'idle' ? undefined : phase}
    >
      {children ?? value}
    </span>
  )
}
