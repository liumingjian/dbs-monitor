import type { NotificationTone } from '../../primitives/NotificationBar'

/** 一次操作的结果反馈。四个标签共用同一个形状，语气走通知条的语汇。 */
export type Feedback = { tone: NotificationTone; text: string }

/**
 * 只读用户看到的原因。**每个禁用控件都要说得出自己为什么禁用** ——
 * 点下去才报错是这次迁移要消灭的行为，所以理由挂在控件的 `title` 上，点之前就看得到。
 * 四条文案按标签分开：说「不能改什么」比说「你没权限」有用。
 */
export const readOnlyReason = {
  channels: '需要告警管理员角色才能修改配置或发送测试通知',
  contacts: '需要告警管理员角色才能修改联系人和联系人组',
  policies: '需要告警管理员角色才能修改通知策略',
  maintenance: '需要告警管理员角色才能管理维护窗口',
} as const

/**
 * 邮箱的形状检查。故意松：真正的判定在服务端，这里只拦「明显不是邮箱」，
 * 免得把合法但少见的地址挡在门外。
 */
export const emailPattern = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
