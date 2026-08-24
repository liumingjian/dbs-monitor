# 决策文档约定

本目录是 **append-only 的决策日志**：推翻一条结论时新开一份记录，不原地改写旧结论。
历史因此完整，代价是目录会持续变长——所以「历史」和「当前真值」必须分开存放。

## 两层，各司其职

| | 当前真值 | 历史日志 |
|---|---|---|
| 在哪 | [`LIVE.md`](LIVE.md)、`CLAUDE.md` 的禁令表、`internal/arch_test.go` | 本目录的 `NN-*.md` 与 [`superseded/`](superseded/) |
| 可变性 | 随时覆盖 | 只增不改 |
| 读取时机 | **每个会话开局必读，且通常只读这个** | 按需 zoom，`LIVE.md` 指到哪读哪 |
| 体量约束 | `LIVE.md` ≤ 16 KB（`check-docs` 强制） | 无上限 |

**不要把决策正文读进上下文。** 本目录全量约 25 万 token，是 smart zone 的一倍半；
调研阶段应读 `LIVE.md` 拿到候选，再展开其中两三份，或把通读派给 subagent 只取结论。

## 一份决策文档长什么样

开头必须是机器可读的 frontmatter：

```yaml
---
status: active            # active | partially-superseded | superseded | historical | generated
superseded_by: ""         # status 为 superseded 时必填，指名推翻它的文档
supersedes: []            # 本文推翻了哪些文档
---
```

| status | 含义 | 位置 |
|---|---|---|
| `active` | 全文是当前真值 | 本目录顶层 |
| `partially-superseded` | 主体有效，部分条目已死；正文须逐条标明哪几条死了 | 本目录顶层 |
| `historical` | 当时有效、未被明文推翻，但只剩考古价值 | 本目录顶层 |
| `superseded` | 整份作废 | **必须移入 `superseded/`** |
| `generated` | 由代码生成，`make gen` 产出，永不手改 | 本目录顶层 |

## 什么该写进这里，什么不该

**该写**：架构形态、带锁定的技术选型、边界与归属、刻意偏离常规路径的做法、代码里看不见的外部约束、非显然的否决理由。判据是三条同时成立——**难以撤回、不看背景会觉得奇怪、是真实取舍的结果**。缺一条就别写。

**不该写**：

- **验收条目、门禁清单、发布留痕、适配进度** → `docs/acceptance/`、`docs/release/`。这些是**项目状态**，交付完即失去价值，不该占用决策语料。
- **产品规格「是什么」** → `docs/spec/`。规格回答是什么，决策回答为什么。
- **术语** → 根目录 `CONTEXT.md`。它是 glossary，不装实现细节。
- **能被 `make check` 验证的约束** → 直接写成测试或 lint。可执行的不变式不需要 agent 读文档才能遵守；`internal/arch_test.go`（依赖方向）和 `01-appendix-implemented.md`（指标字典由 `internal/metric/dictionary.go` 生成）是已有的两个样板。

## 推翻一条结论的正确动作

1. 新开 `NN-<slug>.md`，`supersedes` 列出被推翻的文档。
2. 被推翻的那份：`status: superseded` + `superseded_by`，然后 `git mv` 进 `superseded/`。
   **不删**——它是当时为什么那样决定的唯一证据。
3. 检查还有没有**活文档指向它**。活指针指向死文档比没有指针更危险：读者是被活文档背书地带进废话的。`check-docs` 会拦这一条。
4. 更新 `LIVE.md` 对应行。

## 可执行的守卫

`scripts/check-docs.sh`（`make check` 第一步）强制六条：

1. 每份决策文档都带合法的 `status`。
2. `superseded/` 下的文档必须指名 `superseded_by`，且顶层不得残留 `status: superseded`。
3. 活文档不得链接到 `superseded/`（确需引用时同行加 `<!-- allow-superseded-link -->`）。
4. 顶层决策编号唯一——撞号会让「见 18」这类简写无法解析。
5. 所有相对 markdown 链接解析得到。
6. `LIVE.md` 不超预算。
