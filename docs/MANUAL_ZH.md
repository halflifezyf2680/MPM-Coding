# MPM 完整手册

> **把"聊天开发"变成"可控交付"**

[English](MANUAL.md) | 中文

---

## 目录

1. [核心概念](#1-核心概念)
2. [工具详解](#2-工具详解)
3. [最佳实践](#3-最佳实践)
4. [效能对比](#4-效能对比)
5. [FAQ](#5-faq)

---

## 1. 核心概念

### 1.1 核心概念总览

当我们实际实用 MPM ，可以留意三件很实在的事：

1. 动手前先把位置和影响看清楚，少靠猜
2. 任务一长就分阶段跑，每段有验收点，中断后还能接上
3. 把"为什么这么改"记下来，后面你自己或队友都能接着干

对应到实际落地，是下面这些核心概念：

| 核心概念 | 一句话说明 | 主要工具 | 落地结果 |
|------|------|---------|---------|
| **项目锚点** | 先把当前会话绑定到正确项目根 | `initialize_project` | 让索引/记忆落在正确目录（部分 MCP 客户端不会保证工作目录就是项目根） |
| **定位优先** | 改代码前先找到真实入口和符号 | `project_map` / `flow_trace` / `code_search` | 减少"到处翻文件" |
| **影响前置** | 修改函数前先看调用链影响 | `code_impact` | 减少改一处炸一片 |
| **任务状态机** | 长任务分阶段执行，每段有验收点 | `task_chain` | 长任务更稳，跑偏更少 |
| **阻塞可挂起** | 缺信息时挂起，补齐后恢复 | `system_hook` | 中断后可续跑，不丢上下文 |
| **记忆即审计** | 记录"为什么这么改"，不是只记"改了什么" | `memo` / `system_recall` / `known_facts` | 可复盘、可迁移、可重建 |

补充说明：这主要是 IDE/MCP 客户端场景的问题。客户端启动 server 时不一定会把工作目录设成"项目根"，如果不做程序级处理，数据目录很容易落在 server 可执行文件所在目录，或者系统用户目录下。

MPM 之所以需要"项目锚点"，核心原因是：**项目级 `.mpm-data/` 是按"真实项目根"来放的**（symbols.db / mcp_memory.db / project_config.json 都在这里）。`initialize_project` 做的就是把这个根目录明确下来，确保索引与记忆不会写错位置。

### 1.2 执行闭环（运行时视角）

核心概念落地时，通常按这个顺序跑：

```
initialize_project
  -> project_map / flow_trace（可选，先摸清结构）
  -> code_search（精确定位）
  -> code_impact（改前看影响）
  -> task_chain（长任务建议，带阶段和验收点）
  -> system_hook（阻塞时挂起/恢复）
  -> 修改 + 测试
  -> memo（记录 why）
  -> system_recall / known_facts（后续复用）
```

- **短任务**：`code_search -> code_impact -> 修改/测试 -> memo`
- **长任务**：在中间加 `task_chain`，需要暂停时用 `system_hook`
- **核心目标**：每一步都可解释、可恢复、可追溯

### 1.3 AST 索引原理

索引不是"扫一遍文件名"，而是一个可复用的数据管线。简化后分 5 步：

1) 解析源码（Tree-sitter）
- 扫描项目源码文件，提取函数/方法/类等符号
- 记录文件位置（文件、起止行、签名）

2) 规范化符号标识
- 为每个符号生成 `canonical_id`（全局唯一）
- 维护 `scope_path`（层级作用域）和 `qualified_name`
- 目的：同名符号不再靠猜，定位是可确定的

3) 建调用边（Call Graph）
- 先记录调用关系（caller -> callee_name）
- 再做一次链接，把可解析的调用补成 `callee_id`
- 结果：调用链从"名字匹配"升级为"ID 级连接"

4) 提供查询能力
- `code_search` 使用 5 层渐进匹配（exact / prefix_suffix / substring / levenshtein / stem）
- 所有候选合并后按 `canonical_id` 去重，返回"最佳匹配 + 其他候选"

5) 支撑影响分析
- `code_impact` 基于调用图做多层 BFS 传播分析
- 输出风险等级、直接/间接影响与修改检查清单

关键字段（落库）：

| 字段 | 说明 | 示例 |
|------|------|------|
| `canonical_id` | 全局唯一标识 | `func:core/auth.go::Login` |
| `scope_path` | 层级作用域 | `AuthManager::Login` |
| `callee_id` | 精确调用链目标 | `func:db/query.go::Exec` |

一句话总结：AST 索引把"找代码/看影响"从文本猜测，变成可计算、可复现的结构化查询。

---

## 2. 工具详解

### 2.0 工具清单 (13 个工具)

| # | 工具名称 | 分类 | 描述 |
|---|---------|------|------|
| 1 | `initialize_project` | 系统 | 初始化项目环境和数据库 |
| 2 | `index_status` | 系统 | 查看后台 AST 索引状态 |
| 3 | `project_map` | 感知 | 项目结构导航地图 |
| 4 | `flow_trace` | 感知 | 业务流程追踪（入口/上游/下游） |
| 5 | `code_search` | 感知 | AST 精确符号定位 |
| 6 | `code_impact` | 感知 | 调用链影响分析 |
| 7 | `task_chain` | 调度 | 协议状态机任务链管理 |
| 8 | `system_hook` | 调度 | 待办钩子管理（创建/列表/释放） |
| 9 | `memo` | 记忆 | 变更备忘录（SSOT） |
| 10 | `system_recall` | 记忆 | 检索历史决策和变更 |
| 11 | `known_facts` | 记忆 | 存档经过验证的铁律和避坑经验 |
| 12 | `persona` | 增强 | AI 人格管理 |
| 13 | `open_timeline` | 增强 | 生成并打开项目演进时间线 |

---

### 2.1 系统工具

#### initialize_project

**用途**：初始化项目环境，建立 AST 索引、检测技术栈并生成项目规则。**任何其他 MPM 操作前必须先调用。**

**触发词**：`mpm init`, `mpm 初始化`

**参数**：
| 字段 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `project_root` | string | 否* | 项目根目录的绝对路径。留空时自动探测。 |
| `force_full_index` | boolean | 否 | 强制全量索引（禁用大仓库 bootstrap 策略）。默认: `false` |

*如果留空，通过 `.mpm-data/project_config.json` 锚点自动探测。

**输出**：
- 成功消息及项目路径
- AST 索引状态（后台执行，mode=auto/full）
- 生成的 `_MPM_PROJECT_RULES.md` 路径

**注意事项**：
- 手动指定 `project_root` 时必须使用绝对路径
- 创建 `.mpm-data/project_config.json` 作为项目锚点
- 生成 `_MPM_PROJECT_RULES.md` 供 LLM 参考
- 若在工作区聚合目录下发现多个锚点，拒绝猜测并要求显式指定

---

#### index_status

**用途**：查询 `initialize_project` 启动的后台 AST 索引任务状态。

**触发词**：`mpm index status`, `mpm 索引状态`

**参数**：
| 字段 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `project_root` | string | 否 | 项目根路径。留空时使用会话项目。 |

**输出**：
- `status`: running/success/failed
- `mode`: auto/full
- `started_at` / `finished_at` 时间戳
- `total_files` / `elapsed_ms`
- `heartbeat`: 进度指示器 (processed/total)
- `db_file_sizes`: symbols.db 和 WAL 文件大小

**注意事项**：
- 状态文件: `.mpm-data/index_status.json`
- 心跳文件: `.mpm-data/heartbeat`
- 大型仓库可用此工具监控索引进度

---

### 2.2 感知工具

#### project_map

**用途**：项目导航地图。当你迷路了或不知道该改哪个文件时使用。提供带复杂度热力图的结构化概览。

**触发词**：`mpm map`, `mpm structure`, `mpm 地图`, `mpm 结构`

**参数**：
| 字段 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `scope` | string | 否 | 目录/文件范围（空 = 整个项目） |
| `level` | string | 否 | 层级: "structure"(目录)/"symbols"(函数+类)。默认: "symbols" |
| `core_paths` | string | 否 | JSON 数组格式的高亮核心路径 |

**输出**：
- **项目统计**: 文件数、符号数
- **复杂度热力图**: 高复杂度符号标记 🔴
- **目录结构** (structure 层级): 每个目录的文件数
- **符号结构** (symbols 层级): 每个文件的函数/类及行范围

**示例**：
```
📊 项目统计: 156 文件, 892 符号

🔴 高复杂度热点:
  - SessionManager::Handle (Score: 85)
  - PaymentService::Process (Score: 72)

📁 src/core/ (12 文件)
  ├── session.go
  │   └── func GetSession (L45-80) 🔴
  └── config.go
      └── func LoadConfig (L20-40) 🟢
```

**注意事项**：
- 大输出（>2000 字符）保存到 `.mpm-data/project_map_*.md`
- 使用 "structure" 层级快速查看目录概览
- 使用 "symbols" 层级详细浏览代码导航
- 复杂度分析基于 DICE 算法

---

#### code_search

**用途**：AST 精确符号定位。**当你知道名字（函数名/类名）但不知道文件位置时使用。** 比 grep 更精确。

**触发词**：`mpm search`, `mpm locate`, `mpm 定位`, `mpm 符号`, `mpm find`

**参数**：
| 字段 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `query` | string | 是 | 符号名（不是自然语言描述） |
| `scope` | string | 否 | 目录范围（如 "internal/core"） |
| `search_type` | string | 否 | 过滤: "any"/"function"/"class"。默认: "any" |

**输出**：
- **最佳匹配**: 精确符号信息:
  - `node_type`: function/class/method/struct
  - `name`: 符号名
  - `file_path`: 文件位置
  - `line_start` / `line_end`: 行范围
  - `canonical_id`: 唯一标识符（如 `func:core/auth.go::Login`）
  - `signature`: 函数签名
  - `calls`: 该符号调用的函数（前 5 个）
  - `related_nodes`: 调用者（前 5 个）
- **其他候选**: 相似匹配列表及评分
- **文本搜索 (Ripgrep)**: 无 AST 匹配时的兜底搜索

**示例**：
```
### About Login

最佳匹配: src/auth/login.go L45-67
ID: func:src/auth/login.go::Login
签名: func Login(ctx context.Context, cred Credentials) (*Token, error)

其他候选:
  [func] LoginUser @ src/api/user.go (score: 0.85)
```

**5层降级搜索**：
```
1. 精确匹配 (exact)
2. 前缀/后缀匹配 (prefix/suffix)
3. 子串匹配 (substring)
4. 编辑距离 (levenshtein)
5. 词根匹配 (stem)
```

**注意事项**：
- 查询应为精确符号名，不是自然语言描述
- 无 AST 匹配时，回退到 ripgrep 文本搜索（并附带上下文）
- `scope` 会传给索引引擎；服务端也会对"最佳匹配"做一次 best-effort 的范围校验
- 类型过滤支持 function/method 和 class/struct/interface

---

#### code_impact

**用途**：分析修改函数或类时的影响范围。**修改函数前必须调用。** 展示完整调用链。

**触发词**：`mpm impact`, `mpm dependency`, `mpm 影响`, `mpm 依赖`

**参数**：
| 字段 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `symbol_name` | string | 是 | 精确符号名（函数名或类名） |
| `direction` | string | 否 | 方向: "backward"(谁调用我)/"forward"(我调用谁)/"both"。默认: "backward" |

**输出**：
- **风险等级**: low/medium/high
- **复杂度评分**: 数字复杂度
- **影响节点**: 总数
- **直接调用者**: 前 10 个，含 file:line 位置
- **间接调用者**: 数量及前 20 个名称
- **修改清单**: 目标和验证点
- JSON 摘要供程序化使用

**示例**：
```
CODE_IMPACT_REPORT: GetSession
RISK_LEVEL: high
AFFECTED_NODES: 15

POLLUTION_PROPAGATION_GRAPH
LAYER_1_DIRECT (4):
  - [api/handler.go:45-80] SYMBOL: HandleRequest
  - [service/auth.go:100-130] SYMBOL: Authenticate

LAYER_2_INDIRECT (11):
  - [main.go:50-100] SYMBOL: main
  ... and 9 more

ACTION_REQUIRED_CHECKLIST
- [ ] MODIFY_TARGET: [core/session.go:45-80]
- [ ] VERIFY_CALLER: [api/handler.go:45-80]
- [ ] VERIFY_CALLER: [service/auth.go:100-130]
```

**注意事项**：
- 符号名必须是精确代码符号，不支持字符串搜索
- 使用 AST 分析，不是文本匹配
- 符号不是函数/类定义时返回错误
- 不确定精确名称时先用 `code_search`

---

#### flow_trace

**用途**：给 LLM 建立代码阅读主链的导航工具。基于入口符号或文件，输出"入口-上游-下游"的可读流程摘要，帮助 LLM 按关键节点顺序继续阅读，减少直接通读整文件时的遗漏和误判。

**触发词**：`mpm flow`, `mpm 流程`

**参数**：
| 字段 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `symbol_name` | string | 否* | 入口符号（函数/类）。与 file_path 二选一；若同时提供则优先 symbol_name |
| `file_path` | string | 否* | 目标文件路径。与 symbol_name 二选一 |
| `scope` | string | 否 | 限定范围（大仓建议必填） |
| `direction` | string | 否 | 方向: "backward"/"forward"/"both"。默认: "both" |
| `mode` | string | 否 | 输出层级: "brief"/"standard"/"deep"。默认: "brief" |
| `max_nodes` | integer | 否 | 输出节点预算上限。默认: 40 |

*`symbol_name` 或 `file_path` 至少需要一个。不确定符号名时先用 `code_search`。

**输出**：
- **入口点**: 符号名、类型、位置、评分
- **上游/下游**: 关键节点及影响数
- **关键路径**: Top 3 重要路径
- **阶段摘要**: init/validate/execute/query/persist 阶段
- **副作用**: filesystem/database/network/process/state
- **建议**: 下一步阅读建议

**注意事项**：
- `symbol_name` 只填函数/类名；文件名或文件基名不要填 symbol_name，请用 `file_path`
- 不确定符号名时先用 `code_search`
- 拿到 flow 结果后，优先 Read 入口文件和测试锚点，不要直接整文件硬读猜逻辑
- 文件模式: 分析多个候选入口，展示高分的几个
- 使用 "brief" 快速浏览，"standard"/"deep" 获取更多细节
- 大型仓库强烈建议填写 scope
- 输出在 `max_nodes` 处截断并显示省略节点摘要

---

### 2.3 调度工具

#### task_chain

**用途**：协议状态机任务链管理。支持门控、循环、条件分支和跨会话持久化。

**触发词**：`mpm chain`, `mpm taskchain`, `mpm 任务链`, `mpm 续传`

**参数**：
| 字段 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `mode` | string | 是 | 操作模式（见下表） |
| `task_id` | string | 模式依赖 | 任务标识符 |
| `description` | string | init 模式 | 任务描述 |
| `protocol` | string | init 模式 | 协议名: "linear"/"develop"/"debug"/"refactor"。默认: "linear" |
| `phase_id` | string | 多种模式 | 阶段标识符 |
| `result` | string | complete gate | 门控结果: "pass"/"fail" |
| `summary` | string | complete 模式 | 阶段/子任务总结 |
| `sub_id` | string | complete_sub | 子任务 ID |
| `sub_tasks` | array | spawn 模式 | 子任务列表 |
| `phases` | array | init 模式 | 自定义阶段定义 |

**操作模式**：
| 模式 | 描述 |
|------|------|
| `init` | 初始化任务链（指定协议） |
| `start` | 开始一个阶段 |
| `complete` | 完成阶段（gate 类型需要 result） |
| `spawn` | 在 loop 阶段批量分发子任务 |
| `complete_sub` | 完成单个子任务 |
| `status` | 查看任务进度（自动从数据库加载） |
| `resume` | 恢复/续传任务 |
| `finish` | 彻底关闭任务链 |
| `protocol` | 列出可用协议 |

**内置协议**：
| 协议 | 阶段 | 适用场景 |
|------|------|---------|
| `linear` | main (execute) | 确定性极强的一步走任务 |
| `develop` | analyze → plan_gate → implement(loop) → verify_gate → finalize | 跨模块开发 |
| `debug` | reproduce → locate → fix(loop) → verify_gate → finalize | Bug 排查 |
| `refactor` | baseline → analyze → refactor(loop) → verify_gate → finalize | 大范围重构 |

**示例**：
```javascript
// 1. 初始化一个重构任务
task_chain(mode="init", task_id="AUTH_REFACTOR", protocol="refactor", description="重构登录鉴权模块")

// 2. 完成基线检查
task_chain(mode="complete", task_id="AUTH_REFACTOR", phase_id="baseline", summary="当前测试全绿")

// 3. 进入重构循环
task_chain(mode="spawn", task_id="AUTH_REFACTOR", phase_id="refactor", sub_tasks=[
  {"name": "解耦 SessionStore"},
  {"name": "重写 JWT 签名逻辑"}
])

// 4. 完成子任务
task_chain(mode="complete_sub", task_id="AUTH_REFACTOR", phase_id="refactor", sub_id="sub_001", summary="Store 已提取为接口")
```

**注意事项**：
- 默认协议为 `linear`
- 大工程推荐使用 `develop` 协议，利用 loop 阶段拆解子任务
- gate 阶段需要 `result="pass"` 或 `result="fail"`
- Re-init 保护：同一 task_id 第 2 次 re-init 会被拦截
- V3 的 `linear` 协议通过 `loop` 阶段完美替代旧的 Linear Step 模式

---

#### system_hook

**用途**：统一的待办钩子管理工具（创建/列表/释放）。当任务因缺少信息、等待用户确认或遇到阻塞无法继续时，创建并挂起"钩子"（待办/断点）。

**触发词**：`mpm suspend`, `mpm hook`, `mpm todolist`, `mpm release`, `mpm 挂起`, `mpm 待办`, `mpm 待办列表`, `mpm 释放`, `mpm 完成`

**参数**：
| 字段 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `mode` | string | 是 | 操作模式: "create"/"list"/"release" |
| `description` | string | create 模式 | 待办/阻塞原因描述 |
| `priority` | string | 否 | 优先级: "high"/"medium"/"low"。默认: "medium" |
| `task_id` | string | 否 | 关联的任务 ID |
| `tag` | string | 否 | 分类标签 |
| `expires_in_hours` | integer | 否 | 过期时间（小时）。0 = 永不过期。默认: 0 |
| `status` | string | list 模式 | 过滤: "open"/"closed"。默认: "open" |
| `hook_id` | string | release 模式 | 钩子标识符（如 "#001" 或 UUID） |
| `result_summary` | string | 否 | 完成总结（release 模式） |

**操作模式**：
| 模式 | 描述 |
|------|------|
| `create` | 创建并挂起待办/断点 |
| `list` | 列出按状态过滤的钩子 |
| `release` | 完成并关闭钩子 |

**输出**：
- **create**: Hook ID 及确认详情
- **list**: 钩子列表，含 ID、优先级、描述、过期状态
- **release**: 确认信息，含钩子 ID 和结果摘要

**注意事项**：
- list 模式默认只显示 "open" 状态的钩子
- 过期钩子在列表中标记为 "EXPIRED"
- 用于跨会话任务延续
- 关闭的钩子不再出现在默认（open）列表中
- 使用有意义的结果摘要便于审计追踪

---

### 2.4 记忆工具

#### memo

**用途**：记录变更文档。**任何代码修改后必须调用。** 这是项目演进的唯一真理源 (SSOT)。

**触发词**：`mpm memo`, `mpm record`, `mpm 存档`

**参数**：
| 字段 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `items` | array | 是 | memo 项数组（见下表） |
| `lang` | string | 否 | 语言: "zh"/"en"。默认: "zh" |

**MemoItem 字段**:
| 字段 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `category` | string | 是 | 分类: "修改"/"开发"/"决策"/"重构"/"避坑" |
| `entity` | string | 是 | 改动实体（文件名、函数名、模块名） |
| `act` | string | 是 | 行为: "修复Bug"/"新增功能"/"技术选型" |
| `path` | string | 是 | 文件路径 |
| `content` | string | 是 | 详细说明（为什么，而非是什么） |

**示例**：
```javascript
memo(items=[{
  category: "修复",
  entity: "GetSession",
  act: "添加幂等检查",
  path: "core/session.go",
  content: "防止重复请求创建多个 session"
}])
```

**注意事项**：
- `items` 必须是数组，即使只有一条: `[{...}]`
- 记录"为什么"而非仅仅是"是什么"
- 使用用户对话语言填写内容
- 可通过 `system_recall` 检索

---

#### system_recall

**用途**：从记忆中检索过去的决策、变更和已验证的事实。**修改代码前使用**，避免重复犯错或重复造轮子。

**触发词**：`mpm recall`, `mpm 历史`, `mpm 召回`

**参数**：
| 字段 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `keywords` | string | 是 | 检索关键词（空格分隔，支持模糊匹配） |
| `category` | string | 否 | 类型过滤："开发"/"重构"/"避坑" 等 |
| `limit` | integer | 否 | 最大返回条数。默认: 20 |

**输出**：
- **Known Facts**: 已验证的铁律/避坑经验，含 ID 和日期
- **Memos**: 历史变更记录，含时间戳、分类、行为、内容

**宽进严出策略**：
- **宽进**：在 `Entity` / `Act` / `Content` 多字段中 OR 匹配
- **严出**：通过 `category` 过滤 + `limit` 限制
- **精细输出**：分类展示（Known Facts 优先）+ 时间戳（近→远）

**示例**：
```
## 📌 Known Facts (2)

- **[避坑]** 修改 session 逻辑前必须先检查依赖 _(ID: 1, 2026-01-15)_

## 📝 Memos (3)

- **[42] 2026-02-15 14:30** (修复) 添加幂等检查: 防止重复请求...
- **[41] 2026-02-14 10:00** (开发) 新增 timeout 参数: 适配阿里云...
```

**典型用法**：
```
system_recall(keywords="session timeout")
  → 找到所有涉及 session/timeout 的历史记录

system_recall(keywords="幂等", category="避坑")
  → 只返回避坑类记录
```

**注意事项**：
- 采用"宽进严出"策略：多字段 OR 匹配，再过滤
- Known Facts 优先展示（更高优先级）
- 无匹配时返回"未找到相关记录"

---

#### known_facts

**用途**：存档经过验证的代码规则、铁律或重要的避坑经验，供后续 `system_recall` 检索复用。

**触发词**：`mpm fact`, `mpm 铁律`, `mpm 避坑`

**参数**：
| 字段 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `type` | string | 是 | 事实类型: "铁律"/"避坑"/"规范"/"逻辑" 等 |
| `summarize` | string | 是 | 简洁的事实描述 |

**示例**：
```javascript
known_facts(type="避坑", summarize="修改 session 逻辑前必须先检查依赖")
```

**注意事项**：
- 事实可通过 `system_recall` 检索
- 保持描述简洁且可操作
- 使用一致的类型命名便于过滤

---

### 2.5 增强工具

#### persona

**用途**：切换或管理 AI 人格（角色）。改变语气、回复风格和思维协议，提升特定场景的处理效率。

**触发词**：`mpm persona`, `mpm 人格`, `激活人格`, `切换人格`, `列出人格`, `创建人格`, `删除人格`

**参数**：
| 字段 | 类型 | 必填 | 描述 |
|------|------|------|------|
| `mode` | string | 否 | 模式: "list"/"activate"/"create"/"update"/"delete"。默认: "list" |
| `name` | string | 模式依赖 | 人格名称或别名 |
| `new_name` | string | update 模式 | 新名称 |
| `display_name` | string | create/update | 显示名称 |
| `hard_directive` | string | create/update | 核心指令 |
| `aliases` | array | create/update | 别名列表 |
| `style_must` | array | create/update | 必须遵守的风格 |
| `style_signature` | array | create/update | 标志性表达 |
| `style_taboo` | array | create/update | 禁用表达 |
| `triggers` | array | create/update | 触发词 |

**操作模式**：
| 模式 | 描述 |
|------|------|
| `list` | 列出所有可用人格 |
| `activate` | 按名称或别名激活人格 |
| `create` | 创建新人格（保存到 `.mcp-config/personas.json`） |
| `update` | 更新现有人格 |
| `delete` | 删除人格 |

**内置人格**：
| 名称 | 显示名 | 风格 | 适用场景 |
|------|--------|------|---------|
| `zhuge` | 孔明 | 文言文 | 架构设计、代码审查 |
| `trump` | 特朗普 | 自信、最高级形容词 | 头脑风暴、打破僵局 |
| `doraemon` | 哆啦A梦 | 亲切活泼 | 新手引导 |
| `detective_conan` | 柯南 | 逻辑递进推理 | Bug 排查 |
| `tangseng` | 唐僧 | 江湖话事人语气 | 团队协调 |
| `tsundere_taiwan_girl` | 小智 | 傲娇、台湾腔 | 代码审查 |
| `lich_king_arthas` | 阿尔萨斯 | 冷漠威严 | 严肃调试 |

**上下文稀释判断**：

人格表现强度可作为上下文健康的**信号**：

| 人格表现 | 含义 | 建议 |
|---------|------|------|
| 风格鲜明 | 上下文健康 | 继续当前对话 |
| 表现模糊 | context 已稀释 | 新开对话 / compact / 输入提示词收敛注意力 |

**注意事项**：
- 人格是 BUFF 机制，不是持久配置
- 人格表现模糊表示上下文已稀释
- 自定义人格保存到 `.mcp-config/personas.json`
- 激活时包含给 LLM 的隐藏系统指令

---

#### open_timeline

**用途**：基于 `memo` 记录生成并打开项目演进历史的交互式 HTML 可视化。

**触发词**：`mpm timeline`, `mpm 时间线`

**参数**：无

**输出**：
- 生成的 `project_timeline.html` 路径
- 尝试在默认浏览器中打开

**注意事项**：
- 需要在项目根目录或 `scripts/` 目录存在 `visualize_history.py` 脚本
- 需要安装 Python
- Windows 默认用 Edge 打开（失败时回退到默认浏览器）

---

## 3. 最佳实践

### 3.0 先接管规则（必须）

初始化后，优先将 `_MPM_PROJECT_RULES.md` 注入你使用的客户端系统规则，再开始任何开发任务。

**最小步骤**：

1. 执行 `initialize_project`
2. 打开项目根目录 `_MPM_PROJECT_RULES.md`
3. 将全文粘贴到客户端的系统规则区域

**常见客户端放置位置**（不同版本命名可能略有差异）：

| 客户端 | 建议放置位置 |
|------|-------------|
| Claude Code | System Prompt / Project Instructions |
| OpenCode | System Rules / Workspace Rules |
| Cursor | Rules for AI / Project Rules |

**推荐首句**：

`先读取并严格遵守 _MPM_PROJECT_RULES.md，再执行任务。`

### 3.1 标准工作流

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│    定位     │ ─▶ │    执行     │ ─▶ │    记录     │
│ code_search │     │ 代码修改    │     │   memo      │
│ code_impact │     │             │     │             │
└─────────────┘     └─────────────┘     └─────────────┘
       │                   ▲                   │
       └───────────────────┴───────────────────┘

[建议] 项目文件数 > 100 且用户未提及任何符号时：
  - 先用 project_map 了解整体结构
  - 需要追踪调用链时用 flow_trace 找上下游
  - 最后用 flow_trace 收敛主链阅读路径
```

### 3.2 黄金法则

| 法则 | 说明 |
|------|------|
| **修改前必定位** | 先 `code_search` 再改代码 |
| **大改动必评估** | 先 `code_impact` 看影响 |
| **变更即记录** | 修改后必调用 `memo` |
| **新对话读日志** | 先读 `dev-log.md` 恢复上下文 |

### 3.3 修改代码标准流程

```
1. code_search(query="目标函数")      # 定位
2. code_impact(symbol_name="目标函数") # 评估影响
3. (阅读代码)
4. (执行修改)
5. memo(items=[{...}])                # 记录
```

### 3.4 命名规范（Vibe Coding）

**三大法则**：

1. **符号锚定**：拒绝通用词
   - ❌ `data = get_data()`
   - ✅ `verified_payload = auth_service.fetch_verified_payload()`

2. **前缀即领域**：使用 `domain_action_target`
   - `ui_btn_submit`、`api_req_login`、`db_conn_main`

3. **可检索性优先**：名字越长，冲突越少
   - `transaction_unique_trace_id` 比 `id` 更易搜索

---

## 4. 效能对比

### 4.1 Case 1：符号定位

**任务**：分析 `<symbol>` 工具的实现逻辑

| 指标 | 无 MPM | 有 MPM | 提升 |
|------|--------|--------|------|
| 步骤数 | 12+ 步 | 3 步 | **300%** |
| 工具调用 | 10+ 次 | 2 次 | **400%** |
| 首步命中率 | 0% | 100% | **∞** |

**原因**：`code_search` 直接返回精确坐标（文件:行号），无需反复试错。

---

### 4.2 Case 2：影响评估

**任务**：评估修改 `session.go` 的风险

| 维度 | 无 MPM | 有 MPM |
|------|--------|--------|
| 风险感知 | 基于局部猜测 | **AST 调用链分析** |
| Token 消耗 | 通读文件 (4000+) | 地图摘要 (~800) |
| 输出 | 模糊反问 | **精确修改清单** |

---

### 4.3 Case 3：项目认知

**任务**：冷启动理解 300+ 文件的新项目

| 指标 | 无 MPM | 有 MPM |
|------|--------|--------|
| 总耗时 | 40 秒 | **15 秒** |
| 工具调用 | 4+ 次 | 1 次 |
| 认知路径 | 配置→源码→拼装 | **直达结构化地图** |

---

### 4.4 Case 4：灾难恢复

**场景**：误执行 `git reset --hard`，丢失一天未提交的代码

| 维度 | Git | MPM 数据库 |
|------|-----|-----------|
| 记录触发 | 显式 commit | **修改即 memo** |
| 覆盖范围 | 物理文本 | **意图 + 语义** |
| 恢复成本 | 手动重写 | **指导性恢复** |

**结论**：MPM 保护的是开发过程，Git 保护的是代码。

---

## 5. FAQ

### Q1: `initialize_project` 什么时候需要调用？

**只在以下情况**：
- 重启了 MCP Server / IDE
- 首次使用 MPM

**高级选项**：
- `force_full_index=true`：强制全量索引（禁用大仓 bootstrap 策略）
- `index_status`：查看后台索引进度/心跳/数据库体积

**如果只是新开对话**：直接读 `dev-log.md` 即可，无需重新初始化。

---

### Q2: `code_search` 和 IDE 自带搜索有什么区别？

| 维度 | IDE 搜索 | `code_search` |
|------|---------|---------------|
| 匹配方式 | 文本级 | **AST 符号级** |
| 同名歧义 | 无法区分 | **canonical_id 精确** |
| 上下文 | 需手动查看 | **自动返回签名** |

**建议**：先用 `code_search` 定位，再用 IDE 精读。

---

### Q3: DICE 复杂度分数有什么意义？

说明：DICE 复杂度分数由 AST 引擎计算，属于内部启发式指标。

用途：

1) **标记热点（排优先级）**
- 在 `project_map` / `code_impact` 的输出中用于提示"哪里更值得优先看/优先验证"。

2) **辅助选择执行策略**
- 分数低：通常可以直接修改
- 分数高：先做 `code_impact` 再改
- 分数很高：建议用 `task_chain` 分阶段 + 验收点

3) **降低漏改风险**
- 分数越高往往依赖越密集，越应该先看调用链，再动手改。

备注：具体计算公式属于实现细节，可能随版本调整；建议把它当"提示信号"，不要当成稳定的业务规则。

---

### Q4: 数据存储在哪里？

| 数据 | 位置 |
|------|------|
| AST 索引 | `.mpm-data/symbols.db` (SQLite) |
| Memos | `.mpm-data/mcp_memory.db` |
| 人类可读日志 | `dev-log.md` |
| 项目规则 | `_MPM_PROJECT_RULES.md` |

**建议**：`.mpm-data/` 加入 `.gitignore`。`dev-log.md` 为自动生成，建议也忽略。

**项目绑定**：`initialize_project` 会创建 `.mpm-data/project_config.json` 作为锚点。后续会话自动绑定到此项目根。若在工作区聚合目录下发现多个锚点，MPM 会拒绝猜测并要求显式指定 `project_root`。

---

### Q5: 支持哪些语言？

| 语言 | 扩展名 |
|------|--------|
| Python | .py |
| Go | .go |
| JavaScript/TypeScript | .js, .ts, .tsx, .mjs |
| Rust | .rs |
| Java | .java |
| C/C++ | .c, .cpp, .h, .hpp |

---

## 触发词速查表

| 分类 | 触发词 | 工具 |
|------|--------|------|
| 系统 | `mpm 初始化` | `initialize_project` |
| 系统 | `mpm 索引状态` `mpm index status` | `index_status` |
| 定位 | `mpm 搜索` `mpm 定位` | `code_search` |
| 地图 | `mpm 地图` `mpm 结构` | `project_map` |
| 流程 | `mpm 流程` `mpm flow` | `flow_trace` |
| 链式 | `mpm 任务链` `mpm chain` | `task_chain` |
| 待办 | `mpm 挂起` `mpm 待办列表` `mpm 释放` | `system_hook` |
| 记忆 | `mpm 记录` `mpm 历史` `mpm 铁律` | 记忆系列 |
| 人格 | `mpm 人格` | `persona` |
| 可视 | `mpm 时间线` | `open_timeline` |

---

*MPM Manual v2.2 - 2026-03*
