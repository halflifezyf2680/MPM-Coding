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

### 1.1 执行闭环

```
  定位            分析            执行            记录
┌──────────┐   ┌──────────┐   ┌──────────┐   ┌──────────┐
│  code_   │──▶│  code_   │──▶│  task_   │──▶│   memo   │
│  search  │   │  impact  │   │  chain   │   │          │
└──────────┘   └──────────┘   └──────────┘   └──────────┘
```

- **短任务**：`code_search → code_impact → 改代码 → memo`
- **长任务**：中间加 `task_chain`，阻塞时用 `system_hook`

### 1.2 AST 索引

`initialize_project` 触发后台索引构建，流程：

1. Tree-sitter 解析源码，提取函数/类/方法等符号
2. 为每个符号生成 `canonical_id`（全局唯一）
3. 建立调用边（caller → callee），形成调用图
4. 写入 `symbols.db`（SQLite）

`code_search` 查这个库，`code_impact` 在调用图上做 BFS 传播分析。

支持的语言：Go、Rust、Python、TS/JS、Java、C/C++、HTML、CSS。

---

## 2. 工具参考

### 2.0 总览

| # | 工具 | 分类 | 一句话 |
|---|------|------|--------|
| 1 | `initialize_project` | 系统 | 初始化项目环境和 AST 索引 |
| 2 | `index_status` | 系统 | 查看索引进度 |
| 3 | `project_map` | 导航 | 项目结构 + 符号清单 |
| 4 | `flow_trace` | 导航 | 函数调用链追踪 |
| 5 | `code_search` | 导航 | AST 级符号定位 |
| 6 | `code_impact` | 安全 | 调用链影响分析 |
| 7 | `task_chain` | 执行 | 分阶段任务链（门控 + 续传） |
| 8 | `system_hook` | 执行 | 阻塞挂起 / 恢复 |
| 9 | `memo` | 记忆 | 变更记录（SSOT） |
| 10 | `system_recall` | 记忆 | 搜索历史记录 |
| 11 | `known_facts` | 记忆 | 存档规则和踩坑经验 |
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

**行为**：
- 创建 `.mpm-data/` 目录（SQLite 数据库 + 索引）
- 创建 `.mpm-data/project_config.json` 作为项目锚点
- 后台启动 AST 索引构建
- 生成 `_MPM_PROJECT_RULES.md`

---

### 2.2 index_status

查看 AST 索引进度。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `project_root` | string | 否 | 留空使用当前会话项目 |

**返回**：`status`(running/success/failed)、`mode`、`heartbeat`(processed/total)、`db_file_sizes`

---

### 2.3 project_map

项目结构导航。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `level` | string | 否 | `structure`(目录) / `symbols`(符号)，默认 symbols |
| `scope` | string | 否 | 限定目录范围，留空 = 整个项目 |

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
| `scope` | string | 否 | 限定目录 |
| `search_type` | string | 否 | `any` / `function` / `class`，默认 any |

**5 层降级匹配**：精确 → 前后缀 → 子串 → 编辑距离 → 词根。无 AST 匹配时回退 ripgrep。

**输出**：最佳匹配的 `canonical_id`、文件位置、签名、调用关系 + 其他候选列表。

---

### 2.5 code_impact

调用链影响分析。改代码前用。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `symbol_name` | string | 是 | 精确符号名 |
| `direction` | string | 否 | `backward`(谁调用我) / `forward`(我调用谁) / `both`，默认 backward |

**输出**：风险等级(low/medium/high)、复杂度评分、直接/间接调用者列表、修改检查清单。

---

### 2.6 flow_trace

追踪函数调用链，建立代码阅读路径。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `symbol_name` | string | 否* | 入口符号名 |
| `file_path` | string | 否* | 入口文件路径 |
| `scope` | string | 否 | 限定目录 |
| `direction` | string | 否 | `backward` / `forward` / `both`，默认 both |
| `mode` | string | 否 | `brief` / `standard` / `deep`，默认 brief |
| `max_nodes` | int | 否 | 最大节点数，默认 40 |

\* 二选一。同时提供优先 `symbol_name`。

---

### 2.7 task_chain

分阶段任务链。支持门控验收和跨会话续传。

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

gate 阶段需要 `result="pass"` 或 `result="fail"`。fail 会阻塞，需要调整后重试。

**示例**：
```javascript
// 初始化
task_chain(mode="init", task_id="AUTH", protocol="develop", description="新增 OAuth2")

// 完成实现
task_chain(mode="complete", task_id="AUTH", phase_id="implement", summary="流程已实现")

// 通过验收
task_chain(mode="complete", task_id="AUTH", phase_id="verify_gate", result="pass", summary="测试通过")

// 收尾
task_chain(mode="complete", task_id="AUTH", phase_id="finalize", summary="完成")
```

---

### 2.8 system_hook

任务阻塞时的挂起/恢复机制。

**mode 参数**：

| mode | 说明 | 必要参数 |
|------|------|----------|
| `create` | 创建钩子 | `description`, `priority` |
| `list` | 列出钩子 | `status`(open/closed) |
| `release` | 释放钩子 | `hook_id`, `result_summary` |

**priority**：`high` / `medium` / `low`

---

### 2.9 memo

变更记录。改完代码必调。

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

数据同时写入 SQLite (`mcp_memory.db`) 和 Markdown (`dev-log.md`)。

---

### 2.10 system_recall

搜索历史记录。改代码前查一下有没有踩过类似的坑。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `keywords` | string | 是 | 关键词，空格分隔 |
| `category` | string | 否 | 类型过滤 |
| `limit` | int | 否 | 返回条数，默认 20 |

搜索范围：Memo + Known Facts + Task 记录。Known Facts 优先展示。

---

### 2.11 known_facts

存档规则和踩坑经验。

**参数**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `type` | string | 是 | `铁律` / `避坑` / `规范` / `逻辑` |
| `summarize` | string | 是 | 描述 |

通过 `system_recall` 检索。

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
code_search(query="目标符号")         → 定位
code_impact(symbol_name="目标符号")    → 评估影响
(阅读代码，执行修改)
memo(items=[{...}])                   → 记录原因
```

### 3.3 长任务

```
task_chain(mode="init", protocol="develop")     → 初始化
task_chain(mode="start", phase_id="1")          → 开始阶段
(执行阶段工作)
task_chain(mode="complete", phase_id="1", ...)  → 完成阶段
(重复直到所有阶段完成)
task_chain(mode="finish")                       → 关闭
```

### 3.4 被阻塞

```
system_hook(mode="create", description="等 API 密钥")  → 挂起
(用户提供密钥后)
system_hook(mode="release", hook_id="#001", result_summary="已配置")  → 恢复
```

---

## 4. FAQ

### 支持哪些语言？

Go、Rust、Python、TypeScript/JavaScript、Java、C/C++、HTML（结构符号）、CSS（结构符号）

### 数据存在哪？

| 数据 | 位置 |
|------|------|
| AST 索引 | `.mpm-data/symbols.db` |
| Memo/Task/Hook | `.mpm-data/mcp_memory.db` |
| 人类可读日志 | `.mpm-data/dev-log.md` |
| 项目锚点 | `.mpm-data/project_config.json` |
| 项目规则 | `_MPM_PROJECT_RULES.md` |

`.mpm-data/` 不提交到 git。

### 多个项目怎么切换？

每个项目有独立的 `.mpm-data/`。工作区下有多个项目时，`initialize_project` 需要显式指定 `project_root`。

### 索引没完成能用吗？

`code_search` 等工具依赖索引。大仓库用 `index_status` 查进度，完成前工具返回的结果可能不完整。

### 新开会话需要重新初始化吗？

不需要。只要 MCP Server 没重启，且 `.mpm-data/` 存在，直接用就行。新会话建议先 `system_recall` 恢复上下文。

---

*MPM Manual v2.3 — 2026-04*
