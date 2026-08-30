import { Button, InlineNotification, Tag } from '@carbon/react'
import { createRoute } from '@tanstack/react-router'
import { rootRoute } from './root'
import { vizColorTokens } from '../styles/tokens'
import './design-scaffold.css'

// 令牌层的脚手架页。**临时件**：它只证明构建与令牌链路通了 ——
// 颜色、字体、圆角、间距全部来自令牌层，页面里没有任何字面量色值。
// 真正的基线组件另有单独的工单；那批组件落地后，这个路由和它的样式表一起删掉。

const semanticTokens = [
  { token: 'interactive', label: '可交互蓝', note: '链接、主按钮、焦点环。永不表示状态' },
  { token: 'support-error', label: '严重', note: '状态点、越界数值、破坏性按钮' },
  { token: 'support-warning', label: '警告', note: '规范覆盖值，白底 5.19:1' },
  { token: 'support-success', label: '正常', note: '状态点、恢复提示' },
  { token: 'text-secondary', label: '次级文字', note: '规范覆盖值，白底 5.02:1' },
] as const

const sampleRows = [
  { label: '连接数', value: '1 284' },
  { label: '缓存命中率', value: '99.42 %' },
  { label: '复制延迟', value: '0.31 s' },
] as const

function DesignScaffold() {
  return (
    <div className="scaffold dbs-body">
      <InlineNotification
        kind="info"
        lowContrast
        hideCloseButton
        title="脚手架页"
        subtitle="用于验证 Carbon 构建与设计令牌层，不是产品页面。基线组件落地后删除。"
      />

      <section className="scaffold-section">
        <h1 className="dbs-page-title">设计令牌自检</h1>
        <p className="scaffold-caption">
          这一页上的每一个色值都来自令牌层：Carbon 自带的走 <code>--cds-*</code>，
          规范新增的走 <code>--dbs-*</code>。
        </p>
      </section>

      <section className="scaffold-section">
        <h2 className="dbs-panel-title">语义色</h2>
        <div className="scaffold-swatches">
          {semanticTokens.map((entry) => (
            <div className="scaffold-swatch" key={entry.token}>
              <span className="scaffold-chip" data-token={entry.token} />
              <span className="dbs-body">{entry.label}</span>
              <span className="scaffold-caption dbs-body">{entry.note}</span>
            </div>
          ))}
        </div>
      </section>

      <section className="scaffold-section">
        <h2 className="dbs-panel-title">数据可视化色板</h2>
        <p className="scaffold-caption">六色，刻意不含红色系，避免数据系列与严重度混淆。</p>
        <div className="scaffold-swatches">
          {vizColorTokens.map((token, index) => (
            <div className="scaffold-swatch" key={token}>
              <span className="scaffold-chip" data-token={`viz-${index + 1}`} />
              <span className="dbs-numeric">{token}</span>
            </div>
          ))}
        </div>
      </section>

      <section className="scaffold-section">
        <h2 className="dbs-panel-title">字体档位</h2>
        <p className="dbs-page-title">页标题 28px 细体</p>
        <p className="dbs-body">正文 14px 常规字重，中文用常规，细体只留给 28px 以上的标题。</p>
        <p className="dbs-numeric-display">99.42</p>
        <p className="scaffold-caption dbs-body">
          等宽数值不带字距：Carbon 的等宽档位自带 0.32px，会破坏列对齐。
        </p>
      </section>

      <section className="scaffold-section">
        <h2 className="dbs-panel-title">等宽列对齐</h2>
        <div className="scaffold-rows">
          {sampleRows.map((row) => (
            <div className="scaffold-row-pair" key={row.label}>
              <span className="scaffold-cell dbs-body">{row.label}</span>
              <span className="scaffold-cell dbs-numeric" data-align="end">{row.value}</span>
            </div>
          ))}
        </div>
      </section>

      <section className="scaffold-section">
        <h2 className="dbs-panel-title">直角与间距</h2>
        <div className="scaffold-row">
          <Button kind="primary">主操作</Button>
          <Button kind="secondary">次操作</Button>
          <Button kind="ghost">幽灵</Button>
          <Button kind="danger">销毁</Button>
          <Tag type="red">严重</Tag>
          <Tag type="gray">未知</Tag>
        </div>
        <p className="scaffold-caption dbs-body">
          按钮与卡片一律 0px 圆角、1px 细线、无投影；间距全部落在 4px 网格上。
        </p>
      </section>
    </div>
  )
}

export const designScaffoldRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/design-scaffold',
  component: DesignScaffold,
})
