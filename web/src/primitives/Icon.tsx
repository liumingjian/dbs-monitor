import {
  Add,
  ArrowLeft,
  ArrowRight,
  Calendar,
  ChartColumn,
  ChartLineData,
  Checkmark,
  CheckmarkFilled,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronUp,
  Close,
  Copy,
  Dashboard,
  DataBase,
  Download,
  Edit,
  ErrorFilled,
  Filter,
  FilterRemove,
  Information,
  InformationFilled,
  Launch,
  ListBulleted,
  Locked,
  Misuse,
  Network_3 as Network,
  Notification,
  OverflowMenuVertical,
  Password,
  PauseFilled,
  PlayFilled,
  Plug,
  Power,
  Renew,
  Save,
  Search,
  Send,
  Settings,
  Template,
  Time,
  Tools,
  TrashCan,
  UserAvatar,
  View,
  Warning,
  WarningAlt,
  WarningFilled,
} from '@carbon/icons-react'

// 图标集。整个前端**只**通过这一件取图标 —— 页面不直接 import `@carbon/icons-react`，
// 因为那样一来「用了哪些图标」就散在几十个文件里，再也数不清，也无法保证同一个语义
// 位（新建 / 删除 / 刷新）在两个页面用的是同一个图标。
//
// 名字按「它表示什么动作或事物」取，不按业务概念取：这里没有「实例」「告警」「采集」，
// 只有 database / notification / renew。业务语义由调用方赋予。
//
// 清单覆盖迁移前 AntD 图标普查里的 34 个不同图标位，另加几枚组件外壳自用的骨架图标
// （关闭、折叠箭头等）。新增一枚就在这张表里加一行。
const icons = {
  add: Add,
  arrowLeft: ArrowLeft,
  arrowRight: ArrowRight,
  calendar: Calendar,
  chartColumn: ChartColumn,
  chartLine: ChartLineData,
  checkmark: Checkmark,
  checkmarkFilled: CheckmarkFilled,
  chevronDown: ChevronDown,
  chevronLeft: ChevronLeft,
  chevronRight: ChevronRight,
  chevronUp: ChevronUp,
  close: Close,
  copy: Copy,
  dashboard: Dashboard,
  database: DataBase,
  download: Download,
  edit: Edit,
  errorFilled: ErrorFilled,
  filter: Filter,
  filterRemove: FilterRemove,
  information: Information,
  informationFilled: InformationFilled,
  launch: Launch,
  listBulleted: ListBulleted,
  locked: Locked,
  network: Network,
  notification: Notification,
  overflowMenu: OverflowMenuVertical,
  password: Password,
  pauseFilled: PauseFilled,
  playFilled: PlayFilled,
  plug: Plug,
  power: Power,
  renew: Renew,
  save: Save,
  search: Search,
  send: Send,
  settings: Settings,
  stop: Misuse,
  template: Template,
  time: Time,
  tools: Tools,
  trashCan: TrashCan,
  userAvatar: UserAvatar,
  view: View,
  warning: Warning,
  warningAlt: WarningAlt,
  warningFilled: WarningFilled,
} as const

export type IconName = keyof typeof icons

export type IconProps = {
  name: IconName
  /** Carbon 只出这四档尺寸的字形，16 是行内默认。 */
  size?: 16 | 20 | 24 | 32
  /**
   * 图标独自承担信息时的可访问名。
   * 不给就是纯装饰，渲染成 `aria-hidden` —— 旁边一定有文字说明同一件事。
   */
  label?: string
  className?: string
}

/// 内联 SVG 图标。图标本身随构建产物内联，运行时不请求任何外部域名。
export function Icon({ name, size = 16, label, className }: IconProps) {
  const Glyph = icons[name]
  return label === undefined ? (
    <Glyph size={size} className={className} aria-hidden="true" />
  ) : (
    <Glyph size={size} className={className} role="img" aria-label={label} />
  )
}
