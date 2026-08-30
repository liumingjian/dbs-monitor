import { Button } from '@carbon/react'
import { Link } from '@tanstack/react-router'
import type { ErrorComponentProps } from '@tanstack/react-router'
import { Component } from 'react'
import type { ReactNode } from 'react'
import { Icon } from '../../primitives/Icon'
import { Panel } from '../../primitives/Panel'
import './errorBoundary.css'

/// 兜底页面（未匹配路由 / 崩溃）这三件，和它们共用的那块版式。
///
/// **这一层是别的东西全都失败之后才跑的，所以它自己不许依赖多少东西**：不取数、
/// 不读路由参数、不碰 localStorage，只有 props 进、标记出。用的展示件只有 `Panel` 与
/// `Icon`（都不认识业务概念、不取数），版式和登录页一样是「居中的一块窄面板」——
/// 没有为这几页新发明任何视觉模式。

/// 从任意抛出物里取一句可读的说明。总函数：认不出来就返回 `undefined`，不抛。
/// **它必须是总函数** —— 在错误边界里抛出的异常没有第二层边界接得住，
/// 整页会白屏，而白屏正是这一层要消灭的东西。
export function failureDetail(error: unknown): string | undefined {
  if (typeof error === 'string') {
    return error === '' ? undefined : error
  }
  if (error instanceof Error) {
    return error.message === '' ? undefined : error.message
  }
  return undefined
}

type StatusPageProps = {
  title: string
  description: string
  /** 技术细节（异常消息）。只在有的时候渲染，永远不渲染调用栈。 */
  detail?: string
  actions: ReactNode
}

/// 兜底页的版式：一块居中的窄面板，标题 + 一句人话 + 可选的技术细节 + 去处。
/// 结构照 `DataGrid` 的空态（标题 / 说明 / 去处）与登录页的居中窄面板，没有新东西。
function StatusPage({ title, description, detail, actions }: StatusPageProps) {
  return (
    <div className="dbs-status-page">
      <Panel className="dbs-status-page__panel" title={title}>
        <p className="dbs-status-page__description dbs-body">{description}</p>
        {detail !== undefined && <p className="dbs-status-page__detail dbs-caption">{detail}</p>}
        <div className="dbs-status-page__actions">{actions}</div>
      </Panel>
    </div>
  )
}

/// `as` 槽只收组件，路由属性不能跟着一起传（D19）。模块级定义 = 跨渲染同一个身份。
function InstancesLink(props: object) {
  return <Link {...props} to="/instances" />
}

function RetryIcon() {
  return <Icon name="renew" />
}

/// 未匹配路由。地址打错、链接过期，或者收藏的是一个已经不存在的页面。
export function NotFoundPage() {
  return (
    <StatusPage
      title="页面不存在"
      description="该地址没有对应的页面，可能是链接过期或输入有误。"
      actions={<Button as={InstancesLink} size="md">返回实例列表</Button>}
    />
  )
}

/// 路由级错误边界（`defaultErrorComponent`）。渲染在应用外框**之内**，所以侧栏与页头都还在，
/// 用户可以直接走去别的页面；`reset` 让这一段路由重挂一次，取数失败之类的瞬时故障
/// 不需要整页刷新就能再来一次。
export function RouteErrorPage({ error, reset }: ErrorComponentProps) {
  return (
    <StatusPage
      title="这个页面出错了"
      description="页面在渲染或取数时抛出了异常。可以重试；一直失败就先回实例列表，并把下面这句话告诉管理员。"
      detail={failureDetail(error)}
      actions={
        <>
          <Button size="md" renderIcon={RetryIcon} onClick={reset}>重试</Button>
          <Button kind="tertiary" size="md" as={InstancesLink}>返回实例列表</Button>
        </>
      }
    />
  )
}

type AppErrorBoundaryState = { failed: boolean; detail: string | undefined }

/// 最后一道边界：包住整个 `RouterProvider`。
///
/// 路由自己的 `defaultErrorComponent` 接不住两类失败 —— 应用外框（页头 / 侧栏）自己抛的，
/// 和路由器初始化时抛的：那两类发生在路由匹配之外，没有边界就是整页白屏。所以这里是
/// 一个真正的 React 错误边界类组件，且**不用路由**：去处是一个原生 `<a href>`
/// 加一个整页重载，路由器本身坏掉时它们照样有效。
export class AppErrorBoundary extends Component<{ children: ReactNode }, AppErrorBoundaryState> {
  state: AppErrorBoundaryState = { failed: false, detail: undefined }

  static getDerivedStateFromError(error: unknown): AppErrorBoundaryState {
    return { failed: true, detail: failureDetail(error) }
  }

  componentDidCatch(error: unknown) {
    // 控制台是这里唯一的上报口：这个产品不外发遥测，运行时也不请求外部域名。
    console.error('unhandled application error', error)
  }

  render() {
    if (!this.state.failed) {
      return this.props.children
    }
    return (
      <StatusPage
        title="应用无法继续运行"
        description="控制台遇到了一个没能恢复的错误。刷新页面通常就能回到正常状态；如果反复出现，请把下面这句话告诉管理员。"
        detail={this.state.detail}
        actions={
          <>
            <Button size="md" renderIcon={RetryIcon} onClick={() => window.location.reload()}>刷新页面</Button>
            {/* 原生 `<a>` 而不是路由链接：这一层要在路由器本身坏掉时也能把人送出去。 */}
            <Button kind="tertiary" size="md" href="/instances">返回实例列表</Button>
          </>
        }
      />
    )
  }
}
