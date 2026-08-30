/// 维护窗口的「日期 + 时刻」组合。
///
/// 组件库没有日期时间范围选择器：日期范围是一个控件（`DatePicker datePickerType="range"`），
/// 时刻是另外两个（`TimePicker`）。所以「一个时刻」在表单里是**两段文本**，
/// 提交前才合成一个 ISO 时刻，读回来时再拆开。合成与拆分是这里的两个纯函数，
/// 页面不要各自再写一遍字符串拼接 —— 时区错一次就是维护窗口在错误的时间生效。
///
/// 约定：日期是 `YYYY-MM-DD`，时刻是 24 小时制的 `HH:mm`，两者都按**浏览器本地时区**
/// 解读（用户看到的就是他所在时区的墙上时间），出参是 UTC 的 ISO 串。

export type DateTimeParts = {
  /** `YYYY-MM-DD`，空串表示还没选。 */
  date: string
  /** `HH:mm`（24 小时制），空串表示还没填。 */
  time: string
}

export const datePattern = /^\d{4}-\d{2}-\d{2}$/
export const timePattern = /^([01]\d|2[0-3]):[0-5]\d$/

function pad(value: number, width = 2): string {
  return String(value).padStart(width, '0')
}

/// 把一个 ISO 时刻拆成本地时区的日期与时刻。认不出来的时刻拆成两个空串（表单显示为未填），
/// 不抛 —— 这个函数的调用方是渲染路径。
export function splitLocalDateTime(iso: string): DateTimeParts {
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) {
    return { date: '', time: '' }
  }
  return {
    date: `${pad(at.getFullYear(), 4)}-${pad(at.getMonth() + 1)}-${pad(at.getDate())}`,
    time: `${pad(at.getHours())}:${pad(at.getMinutes())}`,
  }
}

/// 把本地时区的日期与时刻合成一个 ISO 时刻。任何一段不合格式就返回 undefined ——
/// 「合不成」是校验要报的事实，不能兜底成当下时刻。
export function composeLocalDateTime({ date, time }: DateTimeParts): string | undefined {
  if (!datePattern.test(date) || !timePattern.test(time)) {
    return undefined
  }
  // 不带时区后缀的 `YYYY-MM-DDTHH:mm:ss` 按本地时区解读，这正是要的语义。
  const at = new Date(`${date}T${time}:00`)
  if (Number.isNaN(at.getTime())) {
    return undefined
  }
  return at.toISOString()
}

/// 组合出来的值必须**看得见**：两个日期框加两个时刻框读起来是四个独立的数，
/// 所以控件下方回显一句完整的起止与时长。合不成时返回空串，由调用方决定不显示。
export function formatDateTimeRange(start: DateTimeParts, end: DateTimeParts): string {
  const startISO = composeLocalDateTime(start)
  const endISO = composeLocalDateTime(end)
  if (startISO === undefined || endISO === undefined) {
    return ''
  }
  const duration = durationLabel(Date.parse(endISO) - Date.parse(startISO))
  return `${start.date} ${start.time} → ${end.date} ${end.time}（${duration}）`
}

/// 时长文案。零或负数不是时长，交给校验去报，这里如实说出来。
export function durationLabel(milliseconds: number): string {
  if (milliseconds <= 0) {
    return '结束时间不晚于开始时间'
  }
  const minutes = Math.round(milliseconds / 60_000)
  if (minutes < 60) {
    return `共 ${minutes} 分钟`
  }
  const hours = minutes / 60
  if (minutes % 60 === 0 && hours < 48) {
    return `共 ${hours} 小时`
  }
  if (minutes < 24 * 60) {
    return `共 ${Math.floor(hours)} 小时 ${minutes % 60} 分钟`
  }
  const days = Math.floor(minutes / (24 * 60))
  const remainingHours = Math.floor((minutes - days * 24 * 60) / 60)
  return remainingHours === 0 ? `共 ${days} 天` : `共 ${days} 天 ${remainingHours} 小时`
}

/// 时区标签。日期与时刻按本地时区解读这件事必须写出来，否则「12:00」是谁的 12:00 说不清。
export function localTimeZoneLabel(): string {
  const offsetMinutes = -new Date().getTimezoneOffset()
  const sign = offsetMinutes < 0 ? '-' : '+'
  const absolute = Math.abs(offsetMinutes)
  return `UTC${sign}${pad(Math.floor(absolute / 60))}:${pad(absolute % 60)}`
}
