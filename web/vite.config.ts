import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  // Carbon 的 ESM 模块数量极大，不预打包会让开发服务器首次启动慢到不可用。
  optimizeDeps: { include: ['@carbon/react'] },
  css: {
    preprocessorOptions: {
      scss: {
        // `loadPaths` 是现代 Sass JS API 的名字。Carbon 文档给的是 `includePaths`，
        // 那是 legacy API 专有的选项，而 Vite 7 已经彻底移除了 legacy Sass API
        // （连带 `api: 'modern-compiler'` 这个开关也一并去掉了，不要再设）。
        // Vite 自己的 importer 其实已经能解析 `@carbon/react`，这行是显式冗余。
        loadPaths: ['node_modules'],
        // 只静音「依赖」的弃用警告 —— Sass 把通过 loadPaths / importers 加载的文件
        // 算作依赖，Carbon 正好走这条路，而 src/ 下我们自己的样式表不走。
        // 不用 silenceDeprecations 逐条列 ID：那份清单会随 Carbon 升级漂移；
        // 也不用 logger: silent，那会连我们自己的 @warn 一起吞掉。
        quietDeps: true,
      },
    },
  },
  test: {
    environment: 'jsdom',
    exclude: ['e2e/**', 'node_modules/**'],
    env: { TZ: 'UTC' },
  },
})
