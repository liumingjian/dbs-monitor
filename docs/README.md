# 文档索引

> 本索引用于后续 `/wayfinder` 或其他调研/设计会话快速定位材料。  
> 当前阶段：**R1 已完成并冻结；R2（系统架构骨架与技术选型）决策层已收口**——17 张子票全部关闭，决策文档 `04`–`15` 均为 v1.0，walking skeleton（T11）已验收（升级/回滚、PG13–17 矩阵、release gates 递延 R3）。正在进行收口合入与 `/to-spec` 前置准备。

---

## 1. 推荐阅读顺序

```text
1. docs/design/00-decision-index.md          ← R2 开工前必读
   ↓
2. docs/research/aliyun-rds/aliyun-rds-pg-monitor-feasibility-report.md
   ↓
3. docs/design/01-pg-mvp-metric-dictionary.md
   ↓
4. docs/design/02-alert-rule-model-draft.md
   ↓
5. docs/design/03-monitor-platform-ia-draft.md
```

---

## 2. 核心文档

| 文档 | 作用 | 状态 |
|---|---|---|
| [`docs/design/00-decision-index.md`](design/00-decision-index.md) | R1 决策索引：十项决策的结论、理由与**被否决的方案** | v1.0，R1 结束后冻结 |
| [`docs/research/aliyun-rds/aliyun-rds-pg-monitor-feasibility-report.md`](research/aliyun-rds/aliyun-rds-pg-monitor-feasibility-report.md) | 阿里云 RDS PostgreSQL 监控与报警页面可行性调研 | 已完成 |
| [`docs/design/01-pg-mvp-metric-dictionary.md`](design/01-pg-mvp-metric-dictionary.md) | PG MVP 指标字典，定义指标口径、来源、采样、告警适用性 | **v1.0** |
| [`docs/design/02-alert-rule-model-draft.md`](design/02-alert-rule-model-draft.md) | 告警规则配置模型，定义规则、告警实例、状态、No Data、通知等 | **v1.0** |
| [`docs/design/03-monitor-platform-ia-draft.md`](design/03-monitor-platform-ia-draft.md) | 监控平台信息架构，定义页面树、页面职责和排障路径 | **v1.0** |
| [`docs/design/04-metric-storage-model.md`](design/04-metric-storage-model.md) | R2 · 指标存储选型与数据模型：选型结论、schema、分区与保留、查询纪律、事务边界 | **v1.0** |
| [`docs/design/05-backend-code-structure.md`](design/05-backend-code-structure.md) | R2 · 后端代码结构与模块边界：目录树、依赖方向、接缝白名单、错误模型、启动形态 | **v1.0** |
| [`docs/design/06-metric-dictionary-and-collection-plan.md`](design/06-metric-dictionary-and-collection-plan.md) | R2 · 指标字典载体与采集计划：载体形态、采集任务模型、能力枚举与三态、PG13–17 矩阵、采集管线分层、可扩展性边界 | **v1.0** |
| [`docs/design/07-api-contract-and-codegen.md`](design/07-api-contract-and-codegen.md) | R2 · API 契约组织与代码生成流水线：spec 拆分与生成流水线、资源与 URL 模型、空状态码表、枚举穷尽性、认证授权、实时性 | **v1.0** |
| [`docs/design/08-frontend-stack-and-ui.md`](design/08-frontend-stack-and-ui.md) | R2 · 前端技术栈与 UI 体系：UI 组件体系、图表库与领域组件、数据获取层、路由、状态归属三桶、目录结构、状态视觉词汇、测试策略 | **v1.0** |
| [`docs/design/09-packaging-and-deployment.md`](design/09-packaging-and-deployment.md) | R2 · 打包、部署与运行形态：交付物形态、自建 PG 与双架构、运行形态与自举、Agent 分发、首次启动、升级与回滚、资源基线与前置检查 | **v1.0** |
| [`docs/design/10-ai-guardrails-and-verification.md`](design/10-ai-guardrails-and-verification.md) | R2 · AI 开发护栏与验证闭环：两层验证闭环、本地开发环境、强制测试清单与准入判据、`CLAUDE.md` 边界与两份草案、不变式的可执行化、强制点与工作方式 | **v1.0** |
| [`docs/design/11-walking-skeleton-slice.md`](design/11-walking-skeleton-slice.md) | R2 · Walking skeleton 切片定义与验收标准：两条采集通路的切法、告警与前端的深度、鉴权与凭据、分区机制、验收标准三层、禁止清单、推翻选型的处理规则 | **v1.0** |
| [`docs/design/12-collection-concurrency-timeouts-and-backpressure.md`](design/12-collection-concurrency-timeouts-and-backpressure.md) | R2 · 采集并发、超时与背压：中央调度、双连接生命周期、超时与退避、能力探测份额、任务状态与完整性水位、自观测边界 | **v1.0** |
| [`docs/design/13-credential-encryption-rotation-and-revocation.md`](design/13-credential-encryption-rotation-and-revocation.md) | R2 · 凭据加密、轮换与吊销：威胁模型、PG 密文、Agent 登记与令牌生命周期、主密钥、备份恢复和回显边界 | **v1.0** |
| [`docs/design/14-platform-observability-and-diagnostics.md`](design/14-platform-observability-and-diagnostics.md) | R2 · 平台自身运行可观测性与诊断出口：journal + 只读诊断 API、四态平台健康快照、非递归告警边界、磁盘分级保护、故障注入验收 | **v1.0** |
| [`docs/design/15-ci-and-release-pipeline.md`](design/15-ci-and-release-pipeline.md) | R2 · CI 与发布流水线：GitHub Actions 唯一规范执行者、PR 门与 `check-full`、tag + 精确提交校验 + 人工审批发布、四组合构建矩阵、留痕规则 | **v1.0** |
| [`docs/design/16-r2-decision-index.md`](design/16-r2-decision-index.md) | R2 收口索引：固化地图 #15 的 `Decisions so far`，以 `make check ≤120 秒` 为当前真值 | **v1.0** |
| [`docs/design/18-v1-macos-support-boundary.md`](design/18-v1-macos-support-boundary.md) | v1 macOS 首发支持边界：macOS 14.0+、仅原生 arm64，以及开发/CI/安装/交付运行的最低验收语义 | **v1.0** |
| [`docs/design/19-v1-macos-runtime-and-postgresql.md`](design/19-v1-macos-runtime-and-postgresql.md) | v1 macOS 运行与 PostgreSQL 交付：随包 PG 17、系统级 launchd、离线安装，以及备份/升级/卸载闭环 | **v1.0** |
| [`docs/validation/t11-windows-amd64-progress.md`](validation/t11-windows-amd64-progress.md) | T11 · Windows amd64 环境验证记录、Docker Desktop 兼容性结论与 Linux amd64 后续验收清单 | 已完成（T11 已验收） |

### 2.1 四条跨文档不变式

后续路线修改任何一份规格前，应先确认不破坏以下四条（详见决策索引 §4）：

1. **告警状态五档**（`OK / PENDING / FIRING / NO_DATA / RECOVERED`），压制是正交轴。
2. **实例健康 = 未恢复告警的最坏归并 + 已暂停 override**，单一来源。
3. **三档全局角色**，可见性不收窄、写能力收窄，凭据永不回显。
4. **三条采集状态内置规则**不可删除、不可停用，严重级别下限 `warning`。

---

## 3. 取证资产目录

阿里云调研取证资产统一放在：

```text
docs/research/aliyun-rds/evidence/
```

目录结构：

```text
docs/research/aliyun-rds/evidence/
├── screenshots/       # 页面截图
├── snapshots/         # Playwright / 页面结构 YAML 快照
├── extracted-text/    # 页面 innerText / tabs 文本提取
└── playwright/        # 原始 Playwright CLI 日志和页面快照
```

### 3.1 截图

| 文件 | 说明 |
|---|---|
| [`screenshots/aliyun-rds-标准监控-viewport.png`](research/aliyun-rds/evidence/screenshots/aliyun-rds-标准监控-viewport.png) | 标准监控 tab viewport 截图 |
| [`screenshots/aliyun-rds-增强监控-viewport.png`](research/aliyun-rds/evidence/screenshots/aliyun-rds-增强监控-viewport.png) | 增强监控 tab viewport 截图 |
| [`screenshots/aliyun-rds-会话管理-viewport.png`](research/aliyun-rds/evidence/screenshots/aliyun-rds-会话管理-viewport.png) | 会话管理 tab viewport 截图 |
| [`screenshots/aliyun-rds-性能事件-viewport.png`](research/aliyun-rds/evidence/screenshots/aliyun-rds-性能事件-viewport.png) | 性能事件 tab viewport 截图 |
| [`screenshots/aliyun-rds-报警-viewport.png`](research/aliyun-rds/evidence/screenshots/aliyun-rds-报警-viewport.png) | 报警 tab viewport 截图 |
| [`screenshots/aliyun-rds-standard-monitor.png`](research/aliyun-rds/evidence/screenshots/aliyun-rds-standard-monitor.png) | 标准监控补充截图 |
| [`screenshots/aliyun-rds-standard-monitor-2.png`](research/aliyun-rds/evidence/screenshots/aliyun-rds-standard-monitor-2.png) | 标准监控补充截图 2 |
| [`screenshots/aliyun-rds-enhanced-monitor.png`](research/aliyun-rds/evidence/screenshots/aliyun-rds-enhanced-monitor.png) | 增强监控补充截图 |
| [`screenshots/aliyun-rds-session-management.png`](research/aliyun-rds/evidence/screenshots/aliyun-rds-session-management.png) | 会话管理补充截图 |
| [`screenshots/aliyun-rds-alarm-tab.png`](research/aliyun-rds/evidence/screenshots/aliyun-rds-alarm-tab.png) | 报警 tab 补充截图 |
| [`screenshots/aliyun-rds-monitor-initial.png`](research/aliyun-rds/evidence/screenshots/aliyun-rds-monitor-initial.png) | 初始监控页面截图 |

### 3.2 YAML 快照

| 文件 | 说明 |
|---|---|
| [`snapshots/aliyun-rds-standard-monitor.yml`](research/aliyun-rds/evidence/snapshots/aliyun-rds-standard-monitor.yml) | 标准监控页面结构快照 |
| [`snapshots/aliyun-rds-standard-monitor-2.yml`](research/aliyun-rds/evidence/snapshots/aliyun-rds-standard-monitor-2.yml) | 标准监控页面结构快照 2 |
| [`snapshots/aliyun-rds-enhanced-monitor.yml`](research/aliyun-rds/evidence/snapshots/aliyun-rds-enhanced-monitor.yml) | 增强监控页面结构快照 |
| [`snapshots/aliyun-rds-session-management.yml`](research/aliyun-rds/evidence/snapshots/aliyun-rds-session-management.yml) | 会话管理页面结构快照 |
| [`snapshots/aliyun-rds-alarm-tab.yml`](research/aliyun-rds/evidence/snapshots/aliyun-rds-alarm-tab.yml) | 报警页面结构快照 |
| [`snapshots/aliyun-rds-monitor-main.yml`](research/aliyun-rds/evidence/snapshots/aliyun-rds-monitor-main.yml) | 监控主页面结构快照 |
| [`snapshots/aliyun-rds-current.yml`](research/aliyun-rds/evidence/snapshots/aliyun-rds-current.yml) | 当前页面结构快照 |
| [`snapshots/aliyun-rds-monitor-initial.yml`](research/aliyun-rds/evidence/snapshots/aliyun-rds-monitor-initial.yml) | 初始监控页面结构快照 |

### 3.3 文本提取

| 文件 | 说明 |
|---|---|
| [`extracted-text/aliyun-rds-tabs-innertext.json`](research/aliyun-rds/evidence/extracted-text/aliyun-rds-tabs-innertext.json) | 各 tab 页面 innerText 提取结果 |
| [`extracted-text/aliyun-rds-current-innertext.txt`](research/aliyun-rds/evidence/extracted-text/aliyun-rds-current-innertext.txt) | 当前页面 innerText 提取结果 |

### 3.4 T11 validation

| 文档 | 说明 |
|---|---|
| [`docs/validation/t11-linux-amd64-progress.md`](validation/t11-linux-amd64-progress.md) | Native Linux amd64 validation evidence; T11 acceptance complete, with upgrade/rollback and PG13–17 matrix deferred to R3 |

---

## 4. 路线进度

总目标：建成可运行的 PG MVP 监控系统。

| 路线 | 内容 | 状态 |
|---|---|---|
| **R1** | 产品 / 设计 MVP 规格 | **已完成** —— 见 [R1 地图](https://github.com/liumingjian/dbs-monitor/issues/1) |
| **R2** | 系统架构骨架与技术选型 | **进行中** —— 见 [R2 地图](https://github.com/liumingjian/dbs-monitor/issues/15) |
| R3 | 采集与数据模型 | 待开始 |
| R4 | 告警评估引擎 | 待开始 |
| R5 | 前端与交互实现 | 待开始 |
| R6 | 接入、部署与集成运维 | 待开始 |

R2 开工前请先读 [`docs/design/00-decision-index.md`](design/00-decision-index.md)——它记录了 R1 十项决策的理由与**被否决的方案**，避免重新引入已经排除掉的设计（加权健康评分、静默对象、根因抑制、实例级授权等）。

---

## 5. 文件维护约定

- 原始取证资产放在 `docs/research/**/evidence/` 下，不直接散放在仓库根目录。
- 调研报告放在 `docs/research/<topic>/` 下。
- 派生设计文档放在 `docs/design/` 下。
- 文档之间尽量使用相对 Markdown 链接。
- 若后续新增截图或快照，请同步更新本索引和对应调研报告的“参考资产”列表。
