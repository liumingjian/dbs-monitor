# 文档索引

> 本仓库的文档分四层，**入口只有一个**：当前真值索引
> [`design/LIVE.md`](design/LIVE.md)。开工前读它 + 根目录 `CONTEXT.md`，通常不必再读别的。

## 1. 四层文档，各司其职

| 目录 | 装什么 | 什么时候读 |
|---|---|---|
| [`design/LIVE.md`](design/LIVE.md) | **当前真值索引**：一行一条决策 + 出处 | **每次开工，且通常只读这个** |
| [`design/`](design/) | 决策日志，append-only，约 25 万 token | 由 `LIVE.md` 指到哪读哪，**不要 glob** |
| [`design/superseded/`](design/superseded/) | 已整体作废的决策，保留作考古 | 只在追问「当时为什么那样定」时；**不得据以行事** | <!-- allow-superseded-link -->
| [`spec/`](spec/) | 产品规格「是什么」 | 做产品语义相关的迭代时 |
| [`acceptance/`](acceptance/) | 验收条目、Go/No-Go 门禁、发布留痕 | 做验收或发版时；条目真值在 `test/acceptance/matrix.yaml` |
| [`research/`](research/) | 调研报告与取证资产 | 需要外部事实依据时 |
| [`validation/`](validation/) | 环境验证记录 | 需要实测数据时 |

约定（frontmatter `status` 规范、推翻一条结论的正确动作、什么不该写进 `design/`）见
[`design/README.md`](design/README.md)。

## 2. 只在这三种情况下越过 `LIVE.md`

1. **要理由，不只要结论**——`LIVE.md` 只给结论，被否决的方案和边界条件在决策文档正文里。
2. **要做产品语义迭代**——先读 `design/01`（指标字典）、`design/02`（告警模型）、`design/03`（信息架构）
   与 [`spec/mvp-master-spec.md`](spec/mvp-master-spec.md)。
3. **要考古**——R1 十项 ADR 的完整理由与否决记录在 [`design/00-decision-index.md`](design/00-decision-index.md)，
   R2 的在 [`design/16-r2-decision-index.md`](design/16-r2-decision-index.md)。两份都已降为**历史路线索引**，
   首屏口径过期，读前先看各自的「当前适用性」块。

需要通读大量决策时，**派 subagent 去读，只把结论带回主上下文**。

## 3. 阿里云调研取证资产

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

---

## 4. 文件维护约定

- 决策文档进 `design/`，必须带 frontmatter `status`；推翻一条须新开记录并把旧的移入 `superseded/`，**不原地改写**。
- 验收条目 / 门禁 / 发布留痕进 `acceptance/`，**不要放进 `design/`**——它们是项目状态，交付完即失效。
- 原始取证资产放 `research/<topic>/evidence/`，调研报告放 `research/<topic>/`。
- 文档之间用相对 Markdown 链接；`scripts/check-docs.sh`（`make check` 第一步）会验证链接可解析、
  编号唯一、活文档不指向 `superseded/`、`LIVE.md` 不超预算。
- **新增或推翻决策后，改 `design/LIVE.md` 对应那一行。** 不改它，它就会像它取代的那四份索引一样过期。
