# 文档索引

> 本索引用于后续 `/wayfinder` 或其他调研/设计会话快速定位材料。  
> 当前阶段：PostgreSQL 监控平台方向探索与 MVP 设计草案。

---

## 1. 推荐阅读顺序

```text
1. docs/research/aliyun-rds/aliyun-rds-pg-monitor-feasibility-report.md
   ↓
2. docs/design/01-pg-mvp-metric-dictionary.md
   ↓
3. docs/design/02-alert-rule-model-draft.md
   ↓
4. docs/design/03-monitor-platform-ia-draft.md
```

---

## 2. 核心文档

| 文档 | 作用 | 状态 |
|---|---|---|
| [`docs/research/aliyun-rds/aliyun-rds-pg-monitor-feasibility-report.md`](research/aliyun-rds/aliyun-rds-pg-monitor-feasibility-report.md) | 阿里云 RDS PostgreSQL 监控与报警页面可行性调研 | 已完成 |
| [`docs/design/01-pg-mvp-metric-dictionary.md`](design/01-pg-mvp-metric-dictionary.md) | PG MVP 指标字典，定义指标口径、来源、采样、告警适用性 | v0.1 草案 |
| [`docs/design/02-alert-rule-model-draft.md`](design/02-alert-rule-model-draft.md) | 告警规则配置模型，定义规则、告警实例、状态、No Data、通知等 | v0.1 草案 |
| [`docs/design/03-monitor-platform-ia-draft.md`](design/03-monitor-platform-ia-draft.md) | 监控平台信息架构，定义页面树、页面职责和排障路径 | v0.1 草案 |

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

---

## 4. 后续 `/wayfinder` 建议

后续如果继续使用 `/wayfinder`，建议优先围绕以下待确认问题建图或开票：

1. MVP 指标字典中“待确认事项”的逐项决策。
2. 告警模型中的 No Data、维护窗口、恢复阈值语义确认。
3. 信息架构中全局告警 / 实例级告警边界确认。
4. 标准监控与增强监控是否拆成独立页面。
5. 慢查询数据源是否进入 MVP。
6. Agent 指标口径：宿主机、容器还是数据库进程。

---

## 5. 文件维护约定

- 原始取证资产放在 `docs/research/**/evidence/` 下，不直接散放在仓库根目录。
- 调研报告放在 `docs/research/<topic>/` 下。
- 派生设计文档放在 `docs/design/` 下。
- 文档之间尽量使用相对 Markdown 链接。
- 若后续新增截图或快照，请同步更新本索引和对应调研报告的“参考资产”列表。
