# 34 · 平台健康 `TLS` 死源清算:`tls` 子系统由 `TLS_CERTIFICATE` 承载,移除重复登记

- 状态:生效
- 来源:[#158](https://github.com/liumingjian/dbs-monitor/issues/158)(#156 收尾验证时发现,PR #157 正文"产品级发现")
- 输入边界(不重议):[14](14-platform-observability-and-diagnostics.md) D2(平台四态快照、归并序 `FAILED > UNKNOWN > DEGRADED > OK`)、[29](29-production-security-boundary.md) D3(证书过期不拒启动、`tls` 只有 `OK`/`DEGRADED`、刻意无 `FAILED` 档)、[21](21-v1-acceptance-entries-a.md) D6(`AC-09-F6`:任一事实源取不到状态 → 该源 `UNKNOWN` 且总态绝不 `OK`)。

## 1. 现象与事实

`internal/platformhealth/health.go` 登记了两个 TLS 相关事实源:

- `TLS_CERTIFICATE`——**有写入者**。`CertificateSource` 产出 `CERTIFICATE_VALID / CERTIFICATE_EXPIRING / CERTIFICATE_EXPIRED / CERTIFICATE_UNAVAILABLE`,状态只有 `OK`/`DEGRADED`(从不 `FAILED`),`assemble` 每拍按 `expires_at` 重算剩余天数。
- `TLS`——**无写入者**。全仓(除测试)没有任何代码为它写入快照,`NewStore` 播下的 `UNKNOWN`/`FACT_UNAVAILABLE` 即其终态。

后果:归并序中 `UNKNOWN` 压过 `DEGRADED`,只要 `TLS` 在源清单里,**生产环境的平台总态永远是 `UNKNOWN`**——既到不了 `OK` 也到不了 `DEGRADED`,其它源的降级被恒 UNKNOWN 掩盖。`internal/collect/scheduler_test.go` 甚至必须手工为 `TLS` 填一条 `FACT_AVAILABLE` 才能让被测快照走出 UNKNOWN——测试替生产做了生产里不存在的事,这正是死源的实证。

来历:`TLS` 枚举由 #73 十源快照提交(`0f9b82c`)登记,登记时即无写入者。

## 2. D1 · `tls` 子系统的承诺物是 `TLS_CERTIFICATE`,不存在第二个源

[29](29-production-security-boundary.md) D3 定义的 `tls` 子系统语义——证书过期/临期(<30 天)降级、只有 `OK`/`DEGRADED`、刻意无 `FAILED` 档、事件内容可区分——**已由既有 `TLS_CERTIFICATE` 源完整承载**:

- `CertificateSource` 的三码与 29 号 D3 的取值表逐条对应,且从不产出 `FAILED`(刻意缺档成立)。
- 验收 `SEC-3`/`SEC-4`(2026-08-18 全绿,#137 已关)断言的健康变更事件是 `"source":"TLS_CERTIFICATE"`;矩阵条目标题写的"`tls` 子系统 DEGRADED"在被接受的实现里就是这个源。
- 诊断出口 `getCertificateDiagnostics` 返回的也是 `TLS_CERTIFICATE`。

**结论:`tls` 子系统与"证书"事实源是同一个东西,码名 `TLS_CERTIFICATE`。** 29 号 D3"第九个健康子系统"的计数表述按此修正——它没有新增源,它给 14 号 D2 既有的"证书"源钉死了降级语义与缺档。

## 3. D2 · 移除死源 `TLS`,否决另外两个方向

**决定:从源清单与 API 枚举中移除 `TLS`。** 候选三方向的裁定:

- **补写入者(否决)**:`tls` 的全部语义来自证书剩余有效期,给 `TLS` 接线等于让同一注入(一张过期证书)同时降级两个源——证书事实的第二份真相,事件翻倍、语义为零。
- **聚合忽略无写入者的源(否决)**:这会把"死源"从可见事故降级成静默惯例,正面对撞 `AC-09-F6` 的立场——看不见必须以 `UNKNOWN` 可见地存在,而不是被聚合悄悄跳过。
- **移除(采纳)**:登记与写入者同生同灭。源清单里的每一项都必须有真实写入者,这是本文立下的不变式;下次再登记无主源,`AC-09-S1` 的穷尽列举会当场抓住它。

## 4. D3 · 枚举移除与"只许追加"规约的边界

CLAUDE.md"新增枚举码只许追加,禁止修改或复用既有码值"守的是**已承载语义的线上码值**。`TLS` 从未承载过任何事实——它在线上只以 `FACT_UNAVAILABLE` 占位出现;v1 未发布(NO-GO 维持),无任何外部消费方与持久化数据引用该值(健康快照是瞬时态,不落库;journal 历史事件中不存在 `"source":"TLS"` 的记录,因为它从未变更过)。

**决定:允许本次一次性移除 `TLS`,不构成先例。** 发布后枚举一律只增不减;`TLS` 码值**永久废弃,禁止复用**(与"禁止复用既有码值"一致——它曾出现在快照占位里)。

## 5. D4 · 矩阵修订:`AC-09-S1` 十事实源 → 九事实源

只修订 `AC-09-S1` 一条(该条 pending,tracer 未写,无既有绿证据作废):

- 标题与断言中的"十事实源"改"九事实源",穷尽列举去掉 `TLS`,其余九项与顺序不变。
- 本条取代 [32](32-platform-storage-watermark-and-write-protection.md) D8 第 1 条的"十项穷尽列举"计数(该条把 29 号的 `tls` 与 14 号的"证书"错计为两项)。
- `AC-09-F6` 判据不动:它管"取不到状态 → 该源 UNKNOWN 且总态绝不 OK",与源清单成员无关;移除死源后该条约束的是真实源的瞬时不可得,回归本意。

矩阵总数、硬底、`n-a`、`pending` 计数均不变(`AC-09-S1` 本就在 65 条 pending 内)。

## 6. 取代了什么

- [29](29-production-security-boundary.md) D3 中"`tls` 成为 14 的第九个健康子系统"的**计数与命名表述**——子系统语义全部保留,承诺物钉死为 `TLS_CERTIFICATE`,不新增源。
- [32](32-platform-storage-watermark-and-write-protection.md) D8 第 1 条 `AC-09-S1` 的"十项穷尽列举"——改为九项。
- 其余一概不动:归并序、四态、13 码、诊断出口、SEC-3/4 判据与既有绿证据均不受影响。

## 7. 外溢实现硬要求

1. `internal/platformhealth/health.go`:删除 `SourceTLS` 常量与 `sourceOrder` 中的登记。
2. `api/components/enums.yaml`:删除 `TLS` 枚举值与 `HealthSourceTLS` varname,`make gen` 重新生成并入库。
3. 测试同步:`TestStoreIncludesTenHealthSubsystems` 改九源(函数名随语义改);`internal/collect/scheduler_test.go` 删除为死源手工填值的行;`internal/api/enum_test.go` 登记表去掉 `HealthSourceTLS`。
4. `test/acceptance/matrix.yaml`:按 D4 修订 `AC-09-S1`。
5. 完成定义不变:`make check` 全绿(rexec → mac),acceptance 全量重跑确认 SEC/REC 组不受影响。
