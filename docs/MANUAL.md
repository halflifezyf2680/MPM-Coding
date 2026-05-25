# MPM 完整手册

中文 | [English](MANUAL_EN.md)

---

## 目录

1. [核心概念](#1-核心概念)
2. [工具参考](#2-工具参考)
3. [工作流](#3-工作流)
4. [FAQ](#4-faq)

---

## 1. 核心概念

```
定位 ──▶ 分析 ──▶ 执行 ──▶ 记录
code_search  code_impact  task_chain  memo
```
- **短任务**：`code_search → code_impact → 改代码 → memo`
- **长任务**：中间加 `task_chain`，阻塞时用 `system_hook`

### 1.1 AST 索引

`initialize_project` 触发后台索引构建，流程：

1. Tree-sitter（Rust 端，rayon 并行）解析源码，提取函数/类/方法等符号
2. 为每个符号生成 `canonical_id`（全局唯一）
3. 建立调用边（caller → callee），形成调用图
4. 写入 `symbols.db`（SQLite）

索引策略采用两阶段设计：第一阶段不限制文件类型，Rust 端按实际扩展名自适应解析所有 tree-sitter 支持的语言；仅在第一阶段失败时，才降级到第二阶段（仅解析 Go 端检测到的技术栈对应文件）。

文件变更后不需要手动重建索引。MPM 启动文件监控（2000ms debounce），变更的文件被标记为 stale，下次工具调用时自动触发增量索引——只重解析改过的文件，不是全量扫描。

`code_search` 查这个库，`code_impact` 在调用图上做 BFS 链路发现 + Dice Random Walk 复杂度评分。

支持的语言：Go、Rust、Python、TS/JS、Java、C/C++、HTML、CSS。

### 1.2 上下文清洗与注意力收敛

AI 编程的核心瓶颈不是模型能力，是**上下文窗口里的垃圾太多**。

一个 5000 文件的项目，AI 如果靠读文件来理解代码，它要么全读（token 爆炸），要么猜着读（遗漏关键依赖）。两种都是灾难。

MPM 的工具不是给 AI 做 IDE 的事——补全、跳转、重命名，AI 客户端自己就能做。MPM 解决的是另一个问题：**如何用最少的 token 让 AI 精确理解代码结构**。

- `code_search` 返回的是符号定义的精确位置，不是一堆 grep 结果
- `code_impact` 返回的是调用链全景，不是让 AI 一个文件一个文件地猜谁调了它
- `flow_trace` 返回的是业务逻辑主链路，不是目录列表
- `project_map` 返回的是结构化符号清单，不是 `ls` 输出

这些工具的 **output** 本身就构成了对上下文的清洗——只注入确定性的结构信息，把噪声过滤掉。AI 不再需要在大片代码中盲目搜索，工具的输出已经把它的注意力聚焦到必须关注的那几个符号和关系上。

这就是**注意力收敛**：从 "猜文件" 变成 "查符号"，从 "读全文件" 变成 "看调用链"，真正有价值的是这些结果被注入上下文后产生的作用。

[查看交互式架构图](https://halflifezyf2680.github.io/MPM-Coding/architecture.html)

---

## 2. 工具参考

### 2.0 总览

| # | 工具 | 分类 | 一句话 |
|---|------|------|--------|
| 1 | `initialize_project` | 系统 | 初始化项目环境、AST 索引和文件监控 |
| 2 | `index_status` | 系统 | 查看索引进度 |
| 3 | `project_map` | 导航 | 自适应项目结构 + 符号清单 + 复杂度标记 |
| 4 | `flow_trace` | 导航 | 调用链追踪 + 副作用检测 + 阶段分析 |
| 5 | `code_search` | 导航 | 5 层降级符号定位 + ripgrep 深度上下文反查 |
| 6 | `code_impact` | 安全 | BFS 链路发现 + Dice Random Walk 复杂度评分 |
| 7 | `task_chain` | 执行 | 状态机任务链（门控 + 自动推进 + 续传） |
| 8 | `system_hook` | 执行 | 阻塞挂起 / 恢复 |
| 9 | `memo` | 记忆 | 变更记录（SSOT + 灾难恢复） |
| 10 | `system_recall` | 记忆 | 搜索历史记录（Memo + Known Facts） |
| 11 | `known_facts` | 记忆 | 经验策略引擎（多维评分 + 置信度演化 + 事件审计） |
| 12 | `persona` | 增强 | AI 人格切换 |
| 13 | `open_timeline` | 增强 | 项目演进时间线可视化 |

---

### 2.1 initialize_project

初始化项目环境。**所有其他操作的前提。**

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `project_root` | string | 否 | 项目根目录绝对路径，留空自动探测 |
| `force_full_index` | boolean | 否 | 强制全量索引，默认 false |

**完整行为**：

1. **路径安全校验**：过滤危险路径（`.`, `..`, `/`, 卷根等），阻止系统目录、IDE 运行时目录、敏感目录初始化。唯一豁免：目录下存在 `.git`
2. **旧版迁移**：检测到 `.mcp-data` 目录时自动迁移到 `.mpm-data`（rename 或 copy+fallback），迁移后留 `MIGRATED.txt`
3. **技术栈检测**：递归扫描文件扩展名（最大深度 8 层），检测 Python / Frontend / Go / Rust / C++ / Java 六种技术栈，集成 `.gitignore` 解析忽略目录
4. **创建数据目录**：`.mpm-data/`（SQLite 数据库 + 索引文件）
5. **初始化记忆层**：`mcp_memory.db`（Memo / Known Facts / Task / Hook）
6. **后台启动 AST 索引**：两阶段策略（全量优先 → 白名单降级），异步执行不阻塞
7. **启动文件监控**：2000ms debounce，变更标记 stale，工具调用时触发增量索引
8. **生成项目规则**：`_MPM_PROJECT_RULES.md`（MPM 协议规则模板），索引完成后后台刷新

**项目根探测**（留空时自动执行，4 级策略）：

| 优先级 | 策略 |
|--------|------|
| 1 | 向上搜索 `.mpm-data/project_config.json` 锚点 |
| 2 | 有界 BFS 向下搜索（深度 ≤ 3，最多 200 目录） |
| 3 | 环境变量 `MPM_PROJECT_ROOT`、`WORKSPACE_FOLDER` 等 |
| 4 | CWD 兜底 + 项目标记文件检查 |

---

### 2.2 index_status

查看 AST 索引进度。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `project_root` | string | 否 | 留空使用当前会话项目 |

**返回**：
- `index_status`：status (running/success/failed)、mode (auto/full)、started_at、finished_at、total_files、elapsed_ms
- `heartbeat`：processed / total 进度
- `db_file_sizes`：symbols.db / symbols.db-wal / symbols.db-shm 文件大小

---

### 2.3 project_map

项目结构导航。两种视图模式，自适应输出。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `level` | string | 否 | `structure`(目录树) / `symbols`(符号清单+复杂度)，默认 symbols |
| `scope` | string | 否 | 限定目录范围（相对路径），留空 = 整个项目 |

**structure 视图**：目录按文件数排序，自适应展开深度：

| 目录数 | 展开策略 |
|--------|----------|
| ≤ 20 | 全部展开到 L3 |
| 20-40 | 前 8 个展开到 L3，前 18 个展开到 L2 |
| > 40 | 前 6 个展开到 L3，前 12 个展开到 L2 |

**symbols 视图**：符号按复杂度标记分级显示，三级折叠：

| 文件序号 | 展示方式 |
|----------|----------|
| 1-10 | 每个符号详情（名称、行号、复杂度 `[HIGH:xx.x]`/`[MED:xx.x]`/`[LOW:xx.x]`） |
| 11-30 | 仅文件名 + 符号数 |
| > 30 | 折叠为省略提示 |

复杂度评分基于 Fan-in/Fan-out 模型：`score = maxFanOut * 1.0 + maxFanIn * 0.5`，FanOut > 10 标记 "High Coupling"，FanIn > 20 标记 "Core Module"。

**大输出自动保存**：输出超过 2000 字符时，自动保存到 `.mpm-data/project_map_symbols.md` 或 `.mpm-data/project_map_structure.md`，返回文件路径。避免大量文本注入上下文窗口。

**输出示例**：
```
📊 66 文件 | 168 符号
🔥 复杂度: High: 6 | Med: 18 | Low: 144

📂 internal/tools/ (14 files)
  📄 search_tools.go
    ƒ wrapSearch L47 [HIGH:83.5]
    🔷 SearchArgs L15
```

---

### 2.4 code_search

AST 级符号定位。知道名字找不到位置时用这个。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `query` | string | 是 | 符号名（不是自然语言） |
| `scope` | string | 否 | 限定目录（相对路径） |
| `search_type` | string | 否 | `any` / `function` / `class`，默认 any |

**搜索策略——5 层降级 + ripgrep 深度上下文反查**：

```
AST 精确匹配 → 前后缀匹配 → 子串匹配 → 编辑距离匹配 → 词根匹配
     ↓ 全部失败
Ripgrep 文本搜索 + GetSymbolAtLine 反查
```

关键设计：ripgrep 降级不是简单的文本搜索。每个匹配行都会调用 `GetSymbolAtLine()` 反查 AST 数据库，找到该行所属的符号。输出格式为 `L42: \`someFunction(args)\` in \`handleRequest\` (function)`。即使走文本搜索路径，LLM 拿到的也是带语义上下文的结果，不是裸行号。

`search_type` 过滤在 AST 层和文本层都会生效：`function` 匹配 function/method，`class` 匹配 class/struct/interface/component/template 等。

**候选列表**：即使找到精确匹配，仍输出最多 5 个候选（去重后），包含符号类型、文件路径、匹配分数。LLM 可以发现"第二个可能才是我要的"。

**输出**：最佳匹配的 canonical_id、文件位置、签名、调用关系（该符号调用的函数 + 调用该符号的位置）+ 候选列表 + ripgrep 文本搜索结果（含语义上下文标注）。

---

### 2.5 code_impact

调用链影响分析。改代码前用。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `symbol_name` | string | 是 | 精确符号名（不是自然语言搜索） |
| `direction` | string | 否 | `backward`(谁调用我) / `forward`(我调用谁) / `both`，默认 backward |

**风险评分**：两阶段分析。首先 BFS（深度 3）沿调用图发现直接和间接调用者，确定风险等级（影响节点数）。然后 Dice 算法（Random Walk，1000 次游走 × 10 步 × 0.85 damping）计算复杂度评分，公式 `coverage × 0.5 + fanOut × 2.0 + fanIn × 1.0`，归一化到 0-100。复杂度分级：Simple(<20) / Medium(<50) / High(<80) / Extreme。无直接调用者时输出 "可安全修改"。

**输出**：风险等级、复杂度评分、影响节点数、直接调用者列表（前 10 个，含文件路径和行号）、间接调用者列表（前 20 个，含文件路径和行号，BFS 按距离排序）。无直接调用者时输出 "可安全修改"。

---

### 2.6 flow_trace

调用链追踪引擎。建立代码阅读路径。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `symbol_name` | string | 是 | 函数名、类名或文件路径，系统自动识别。支持：`handleRequest`、`internal/tools/a.go`、`internal/tools/a.go:handleRequest` |
| `scope` | string | 否 | 限定目录（相对路径） |
| `direction` | string | 否 | `backward` / `forward` / `both`，默认 both |
| `mode` | string | 否 | `brief` / `standard` / `deep`，默认 brief |
| `max_nodes` | int | 否 | 最大节点数，默认 40，上限 120 |

**双模式入口识别**：

| 输入 | 模式 | 行为 |
|------|------|------|
| `handleRequest` | symbol 模式 | 追踪单个符号的上下游 |
| `internal/tools/a.go` | file 模式 | 提取文件内所有符号，按评分排序，展示高入口 |
| `internal/tools/a.go:handle` | 混合模式 | 从指定文件中查找指定符号 |

**文件模式智能筛选**：提取文件内所有 callable 和 type 符号，为每个构建调用链快照，按综合评分排序。评分公式中跨文件入度权重最高（×50），意味着对外暴露的核心接口排在前面。按 mode 截断：brief=1, standard=2, deep=4。

**副作用检测**：基于证据的两阶段分析。从实际调用链收集被调用函数名，对 5 类副作用打分判定：

| 副作用类型 | 证据示例 |
|------------|----------|
| filesystem | readFile, writeFile, os.Open, mkdir, remove |
| database | query, begin, commit, db.Exec, stmt.Query |
| network | listen, dial, http.Get, grpc.Dial |
| process | exec.Command, StartProcess, spawn |
| state | Lock, Mutex, cache.Set, session.Save |

奥卡姆剃刀原则：无证据则不报告，避免泛化。

**阶段检测**：自动识别 init → validate → execute → query → persist 业务阶段。

**输出边界声明**：标题下方标注 `> 以下为调用链索引（上下游符号关系），不含实现细节。提出方案前应阅读目标文件完整上下文。` 防止 LLM 将调用链误认为完整理解。

**mode 差异**：

| 信息 | brief | standard | deep |
|------|-------|----------|------|
| 入口位置/类型 | ✅ | ✅ | ✅ |
| 跨文件连接数 | ✅ | ✅ | ✅ |
| 上游/下游影响数 | ✅ | ✅ | ✅ |
| 上游关键节点 | ❌ | ✅ | ✅ |
| 关键路径 Top3 | ❌ | ✅ | ✅ |
| 阶段摘要 | ❌ | ✅ | ✅ |
| 副作用标注 | ❌ | ✅ | ✅ |
| 候选入口数 | 1 | 2 | 4 |

---

### 2.7 task_chain

**状态机驱动的自迭代任务体系。** 不是简单的步骤列表——init 定义阶段和门控，AI 自主推进，gate 失败自动回退重试，人只在关键节点做检查。

**核心机制**：

- **声明式阶段** — init 时定义 phase 目标和验收标准，AI 在阶段内享有执行自由
- **gate 门控** — gate 阶段必须 result="pass" 才推进；fail 时检查 on_fail 路由：有配置则跳转到指定阶段并重置其状态（不是简单重试当前 gate，而是回到前阶段重新执行）；无配置则重试当前 gate。超过 max_retries（默认 3）标记任务失败
- **loop 循环** — spawn 子任务批量分发，逐个 complete_sub，全部完成后自动 passed
- **自动推进** — init 自动 start 首阶段，spawn 自动 start 首子任务，complete_sub 自动 start 下一个 pending 子任务。AI 只需调 complete，系统自动推进下一步
- **update 运行时调整** — 目标变了？update 修改 description 或替换未完成阶段，已完成的永远保留
- **跨会话续传** — resume 从 SQLite 恢复断点（阶段状态、子任务进度、已完成 summary），会话断了进度不丢
- **re-init 守卫** — 同一 task_id 被 init 两次以上时直接报错，强制 AI 停下来向用户说明。防止死循环

**mode 参数**：

| mode | 说明 | 必要参数 |
|------|------|----------|
| `init` | 初始化 | `task_id`, `description`, `protocol` 或 `phases` |
| `update` | 修改目标 | `task_id`, `description`/`phases` |
| `start` | 开始阶段 | `task_id`, `phase_id` |
| `complete` | 完成阶段 | `task_id`, `phase_id`, `summary` |
| `spawn` | 分发子任务 | `task_id`, `phase_id`, `sub_tasks` |
| `complete_sub` | 完成子任务 | `task_id`, `phase_id`, `sub_id` |
| `status` | 查看进度 | `task_id` |
| `resume` | 恢复任务 | `task_id` |
| `finish` | 关闭任务链 | `task_id` |

**内置协议**：

| 协议 | 阶段 | 场景 |
|------|------|------|
| `linear` | main | 一步到位的确定性任务 |
| `develop` | implement → verify_gate → finalize | 功能开发 |
| `debug` | fix → verify_gate → finalize | Bug 修复 |
| `refactor` | refactor → verify_gate → finalize | 重构 |

gate 阶段需要 `result="pass"` 或 `result="fail"`。fail 会触发 on_fail 路由或重试。

**示例**：
```javascript
// 初始化（自动 start 首阶段 implement）
task_chain(mode="init", task_id="AUTH", protocol="develop", description="新增 OAuth2")

// 完成实现
task_chain(mode="complete", task_id="AUTH", phase_id="implement", summary="流程已实现")

// 通过验收
task_chain(mode="complete", task_id="AUTH", phase_id="verify_gate", result="pass", summary="测试通过")

// 收尾
task_chain(mode="complete", task_id="AUTH", phase_id="finalize", summary="完成")
```

**实战场景**：规划好完整的任务体系后，让能力强的模型自己跑。睡前启动一个 `develop` 协议链——从实现、验证到收尾，每个阶段有 gate 门控把关，失败自动重试或回退，跑偏自己拉回来。第二天醒来 `task_chain(mode="status")` 看结果。网络问题导致会话断了？没事 `resume` 从断点续传，不丢失进度。这就是 task_chain 的真正价值：**让 AI 能独立完成多步长任务，人只做检查**。

---

### 2.8 system_hook

任务阻塞时的挂起/恢复机制。

**mode 参数**：

| mode | 说明 | 必要参数 |
|------|------|----------|
| `create` | 创建钩子 | `description`, `priority` |
| `list` | 列出钩子 | `status`(open/closed) |
| `release` | 释放钩子 | `hook_id`, `result_summary` |

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `description` | string | create 时必填 | 挂起原因描述 |
| `priority` | string | 否 | `high` / `medium` / `low`，默认 medium |
| `tag` | string | 否 | 分类标签 |
| `expires_in_hours` | int | 否 | 过期时间（小时），过期后 list 时显示 EXPIRED 标记 |
| `hook_id` | string | release 时必填 | 钩子编号（如 #001） |
| `result_summary` | string | release 时可选 | 完成总结 |

**实战场景**：和 AI 讨论问题的过程中，发现 5 个需要处理但当下不能立刻动手的点。逐个 `create` 挂起来，继续讨论下一个话题。继续发现新问题，继续 `create` 挂起来。讨论完了，批量 `release` 逐个闭合。不用记在脑子里，不用怕遗漏。**system_hook 是决策神器**——它让你和 AI 的对话从"边聊边忘"变成"边聊边记，集中处理"。

---

### 2.9 memo

变更记录（SSOT）。改完代码必调。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `items` | array | 是 | 记录数组，每项含 `category`, `entity`, `act`, `path`, `content` |
| `lang` | string | 否 | `zh` / `en`，默认 zh |

**category**：`修改` / `开发` / `决策` / `重构` / `避坑`

**示例**：
```javascript
memo(items=[{
  category: "修改",
  entity: "GetSession",
  act: "添加幂等检查",
  path: "core/session.go",
  content: "防止重复请求创建多个 session"
}])
```

**存储机制**：
- 写入 SQLite (`mcp_memory.db`)
- 异步刷新 Markdown (`dev-log.md`，top 100 条，时间倒序)
- 异步追加 JSONL 归档 (`dev-log-archive/memo_archive.jsonl`)

**灾难恢复**：初始化时检查 DB memos 表是否为空。空则尝试从 JSONL 归档恢复；JSONL 也没有则从 `dev-log.md` 用正则解析恢复。三层策略确保即使 SQLite 文件损坏，历史记录也不会完全丢失。

---

### 2.10 system_recall

搜索历史记录。改代码前查一下有没有踩过类似的坑。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `keywords` | string | 是 | 关键词，空格分隔，支持模糊匹配 |
| `category` | string | 否 | 类型过滤（如 "避坑"、"开发"、"决策"） |
| `limit` | int | 否 | 返回条数，默认 20 |

**搜索范围**：Memo（变更记录）+ Known Facts（经验策略）。Known Facts 优先展示。

---

### 2.11 known_facts

经验策略引擎。行动前召回经验、行动后回写结果并进化置信度。

旧式 `type + summarize` 保存仍兼容；新主路径是 `before_action → 执行 → after_action` 闭环。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `mode` | string | 否 | `before_action` / `after_action` / `maintain` / `status`；为空时兼容旧式保存 |
| `type` | string | 否 | 事实类型：`pitfall` / `success_pattern` / `rule` / `constraint`，旧式保存时使用 |
| `summarize` | string | 否 | 事实描述，旧式保存或 `mode=add` 时使用 |
| `context` | object | 否 | 当前任务上下文：task、task_id、intent、phase、risk、files、symbols、tools |
| `outcome` | object | 否 | `after_action` 使用：result、signal、summary、adopted_facts、rejected_facts、new_observations |
| `limit` | int | 否 | 返回数量，默认 3 |

**before_action（行动前召回）**：

根据当前上下文（task/intent/phase/files/symbols/tools），从事实库召回相关 Known Facts，按多维加权评分排序：

```
score = contextMatch × 0.45    // 上下文关键词匹配度（最高权重）
      + confidence × 0.25      // 事实置信度
      + successRate × 0.20     // 历史采纳成功率
      + exploration × 0.10     // 冷门探索奖励（曝光少但全局命中多的事实优先浮出）
      - failurePenalty         // 失败惩罚
```

同时返回策略建议（基于 intent 和 risk 等级动态生成）。

**after_action（行动后回写）**：

根据执行结果更新事实置信度——贝叶斯式演化：

| 场景 | 公式 | 效果 |
|------|------|------|
| 采纳 + 成功 | `confidence += 0.10 × (1 - confidence)` | 指数趋近 1.0，越高越难涨 |
| 采纳 + 失败 | `confidence -= 0.20 × confidence` | 快速惩罚，比例收缩 |
| 未采纳 | `confidence -= 0.02 × confidence` | 微弱衰减，不误杀 |

自动状态晋升：`confidence >= 0.75 && support >= 3` → active；`confidence < 0.25 && reject >= 2` → rejected。

**自动持久化**：after_action 将新观察写入 `.claude/CLAUDE.md` 和项目级 `AGENTS.md`：
- 使用标记区间定位（`## MPM Known Facts` ... `<!-- MPM_KNOWN_FACTS_END -->`）
- 写入前检查内容是否已存在（去重）
- `.tmp` + `os.Rename` 原子写入
- 阈值治理：>50 条建议去重，>70 条建议压缩（死区设计，只向上跨越时触发）

**事件审计**：每次事实交互（exposure/adopt/reject/add）都记录 `FactEvent`，包含 eventType、task_id、phase、context_signature 和 payload_json。完整的事实生命周期审计链。

**`new_observations` 写入指引**：只写可复用经验（格式："在XX条件下应该/不应该YY"），不写任务完成确认（如"Successfully fixed..."）或操作流水账。

**常用调用**：

- `mode=before_action`：根据当前上下文返回 Relevant Known Facts 和 Strategy。
- `mode=after_action`：根据结果强化/削弱事实，并把新观察写入 candidate。
- `mode=status`：查看事实状态（置信度、采纳/拒绝次数、命中次数）。
- `mode=maintain`：查看收敛维护建议（各状态数量统计）。

`system_recall` 仍用于历史搜索，不承担 KnownFact 策略进化。

---

### 2.12 persona

AI 人格管理。

**mode**：`list` / `activate` / `create` / `update` / `delete`

创建/更新时可选字段：`name`, `display_name`, `hard_directive`, `aliases`, `style_must`, `style_signature`, `style_taboo`, `triggers`

人格数据保存在 `.mcp-config/personas.json`。

---

### 2.13 open_timeline

基于 memo 记录生成项目演进时间线（HTML），自动在浏览器打开。

无参数。

---

## 3. 工作流

### 3.1 初始化

```
1. initialize_project
2. 把 _MPM_PROJECT_RULES.md 放进客户端系统规则
3. 开始干活
```

### 3.2 修改代码

```
code_search(query="目标符号")         → 定位（AST 5层降级 + ripgrep反查）
code_impact(symbol_name="目标符号")    → 评估影响（调用链BFS + 风险评分）
(阅读代码，执行修改)
memo(items=[{...}])                   → 记录原因（SSOT + 灾难恢复）
```

### 3.3 理解业务逻辑

```
flow_trace(symbol_name="入口函数", mode="standard")  → 调用链 + 副作用 + 阶段
或
flow_trace(symbol_name="internal/service/user.go")   → 文件模式，自动筛选高入口
```

### 3.4 长任务

```
task_chain(mode="init", protocol="develop")     → 初始化（自动start首阶段）
(执行阶段工作)
task_chain(mode="complete", phase_id="1", ...)  → 完成阶段（自动推进下一步）
(重复直到所有阶段完成)
task_chain(mode="finish")                       → 关闭
```

### 3.5 经验积累

```
// 行动前：召回相关经验
known_facts(mode="before_action", context={task="...", intent="DEVELOP", ...})

// 行动后：回写结果
known_facts(mode="after_action", outcome={
  result: "success",
  adopted_facts: [12],
  new_observations: ["在XX条件下应该YY"]
})
```

### 3.6 被阻塞

```
system_hook(mode="create", description="等 API 密钥", priority="high")  → 挂起
(用户提供密钥后)
system_hook(mode="release", hook_id="#001", result_summary="已配置")     → 恢复
```

---

## 4. FAQ

### 支持哪些语言？

Go、Rust、Python、TypeScript/JavaScript、Java、C/C++、HTML（结构符号）、CSS（结构符号）

共 11 种 tree-sitter 绑定。索引第一阶段不限制文件类型，所有 tree-sitter 能解析的语言都会被索引。

### 数据存在哪？

| 数据 | 位置 |
|------|------|
| AST 索引 | `.mpm-data/symbols.db` |
| Memo/Task/Hook/KnownFacts | `.mpm-data/mcp_memory.db` |
| 人类可读日志 | `.mpm-data/dev-log.md` |
| JSONL 归档 | `.mpm-data/dev-log-archive/memo_archive.jsonl` |
| 项目锚点 | `.mpm-data/project_config.json` |
| 项目规则 | `_MPM_PROJECT_RULES.md` |
| 大输出缓存 | `.mpm-data/project_map_*.md` |

`.mpm-data/` 不提交到 git。

### 多个项目怎么切换？

每个项目有独立的 `.mpm-data/`。工作区下有多个项目时，`initialize_project` 需要显式指定 `project_root`。

### 索引没完成能用吗？

`code_search` 等工具依赖索引。大仓库用 `index_status` 查进度，完成前工具返回的结果可能不完整。

索引采用三层新鲜度缓存（内存 5 分钟阈值 → 文件修改时间 → 数据库行数检查），工具调用前自动检查，过期才触发增量索引。

### 新开会话需要重新初始化吗？

不需要。只要 MCP Server 没重启，且 `.mpm-data/` 存在，直接用就行。新会话建议先 `system_recall` 恢复上下文。

### 索引会自动更新吗？

会。初始化后启动文件监控（2000ms debounce），文件变更自动标记 stale，下次工具调用时触发增量索引（只重解析变更文件）。

---

*MPM Manual v3.0 — 2026-05*
