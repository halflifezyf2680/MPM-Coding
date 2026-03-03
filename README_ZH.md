# MPM-Coding MCP

> **把 AI 编程从"过程演示"变成"工程交付"**

[English](README.md) | 中文

![License](https://img.shields.io/badge/license-MIT-blue.svg) ![Go](https://img.shields.io/badge/Go-1.21+-00ADD8.svg) ![MCP](https://img.shields.io/badge/MCP-v1.0-FF4F5E.svg)

---

## 行业现状

AI 编程工具的普及降低了编程门槛，但也暴露了一些问题：许多项目在演示阶段表现亮眼，但在生产落地时缺乏对以下工程问题的系统性考虑：

- **复现性**：能否稳定复现执行结果？
- **容错恢复**：失败后能否断点续传？
- **权限边界**：默认权限是否足够小？能否防止越权？
- **验收审计**：有明确的验收标准吗？操作可追溯吗？
- **自迭代**：状态如何推进？方法是什么？

当前 AI 编程实践中常见的现象：
- 依赖自然语言描述的工作流和 agent 模板，约束力不足
- LLM 生成的文档看似专业，但难以形成可验证的体系
- 使用者难以判断 LLM 生成内容的可靠性

**核心观点**：LLM 工程化落地不靠堆 prompt 或多 agent 协同，而是需要**程序化框架**——信息清洗、状态外置、流程状态机、验证前置、工具边界明确、记忆固化与灵活提取。

交付能力的上限是工具框架决定的，LLM 是驱动方式。

## MPM 是什么

MPM 是一个 **MCP 工程化层**，让 AI 编程从"聊天"升级为"可控交付流程"。
你可以在几乎无认知负担的情况下直接开始使用：初始化后将 `_MPM_PROJECT_RULES.md` 应用为系统 `Rules` 即可。

### 🚀 30秒上手（先做这一步）

```text
1) initialize_project
2) 把 _MPM_PROJECT_RULES.md 贴到客户端系统规则
3) 直接提任务："帮我修复 XXX，并按规则执行"
```

这一步做对后，不需要先完整学习所有工具。

**核心差异**：

| 传统方式 | MPM 方式 |
|---------|---------|
| `grep "Login"` → 500 条结果 | `code_search("Login")` → 精确到文件:行号 |
| "我觉得这里改了就行" | `code_impact` → 完整调用链分析 |
| 每次对话从零开始 | `system_recall` → 跨会话记忆召回 |
| AI 自由发挥 | `task_chain` → 结构化任务执行 |

### 实用流程：一次完整闭环（示例）

以下是一个可直接复制粘贴的完整任务示例，复制后粘贴到任意 MCP 客户端即可运行。

#### 常规模式（推荐新手）

```text
先读取 _MPM_PROJECT_RULES.md 并按规则执行。

任务：修复 Login 回调的幂等问题。
要求：
1. 先定位代码
2. 分析影响范围
3. 实现修复
4. 记录变更原因
```

AI 将自动执行：`initialize_project` → `code_search` → `code_impact` → 修改代码 → `memo` 记录。

#### 严格模式（需显式验收）

```text
先读取 _MPM_PROJECT_RULES.md 并按规则执行。

使用 task_chain 完成以下任务：
任务：修复 Login 回调的幂等问题。

阶段要求：
1. 定位阶段：用 code_search 找到目标函数
2. 分析阶段：用 code_impact 评估影响范围
3. 实现阶段：修复并通过测试
4. 收尾阶段：用 memo 记录变更原因

每个阶段完成后报告结果，等待确认再继续。
```

#### 闭环检查清单

- **理解**：`project_map` / `flow_trace` 了解项目结构
- **定位**：`code_search` 精确找到符号
- **评估**：`code_impact` 分析调用链影响
- **修改**：编写代码，修复编译错误
- **验证**：运行测试确认功能正确
- **记录**：`memo` 归档变更原因

> ⚠️ **数据卫生**：`.mpm-data/` 目录存储本地数据，不应提交到版本控制。
>
> **项目绑定**：`initialize_project` 会创建 `.mpm-data/project_config.json` 作为锚点。后续会话自动绑定到此项目根。若在工作区聚合目录下发现多个锚点，MPM 会拒绝猜测并要求显式指定 `project_root`。

---

## 核心卖点

### 1. AST 精确定位，不是文本搜索

```text
你：搜索 Login 函数
AI：### About Login
    最佳匹配: src/auth/login.go L45-67
    ID: func:src/auth/login.go::Login
    签名: func Login(ctx context.Context, cred Credentials) (*Token, error)
```

**背后技术**：Rust AST 引擎 + `canonical_id` 消除同名歧义

### 2. 完整调用链追踪

```text
你：分析修改 SessionManager 的影响
AI：CODE_IMPACT_REPORT
    风险等级: HIGH
    直接影响: 4 个函数
    间接影响: 23 个函数（3层调用链）
    
    修改清单:
    ▶ [core/session.go:100-150] MODIFY_TARGET
    ▶ [api/handler.go:45-80] VERIFY_CALLER
    ▶ [service/auth.go:200-250] VERIFY_CALLER
```

### 3. 跨会话记忆持久化

```text
你：上次为什么把 timeout 改成 30s？
AI：(system_recall) 2024-01-15 的 memo：
    "将 timeout 从 10s 改为 30s，因为阿里云 ECS 冷启动延迟"
```

---

## 快速开始

### 1. 编译

```powershell
# Windows
powershell -ExecutionPolicy Bypass -File scripts\build-windows.ps1

# Linux/macOS
./scripts/build-unix.sh
```

### 2. 配置 MCP

指向编译产物：`mcp-server-go/bin/mpm-go(.exe)`

### 3. 开始使用

```text
初始化项目
帮我分析修复 Login 回调幂等问题的方案
```

初始化后会自动生成 `_MPM_PROJECT_RULES.md`，这是项目的"操作说明书"：

- 告诉 LLM 这个仓库的命名风格、工具使用顺序、硬规则
- 让你不必先完整学习所有工具细节，也能直接进入可用状态
- 新会话时优先让 LLM 先读取该文件，可明显降低误操作

推荐首句：`先读取 _MPM_PROJECT_RULES.md 并按规则执行`

### 4. 发布打包（固定目录）

```powershell
python package_product.py
```

说明：

- 打包目录固定为 `mpm-release/MyProjectManager`
- 每次执行会先清理旧的 `mpm-release` 后再重建

---

## 工具速查表

| 触发词 | 工具 | 用途 |
|--------|------|------|
| `mpm 初始化` | `initialize_project` | 项目绑定与 AST 索引（支持 `force_full_index`） |
| `mpm 索引状态` | `index_status` | 查看后台索引进度/心跳/DB大小 |
| `mpm 搜索` | `code_search` | AST 精确定位符号 |
| `mpm 影响` | `code_impact` | 调用链影响分析 |
| `mpm 地图` | `project_map` | 项目结构 + 热力图 |
| `mpm 流程` | `flow_trace` | 业务流程追踪（入口/上游/下游） |
| `mpm 任务链` | `task_chain` | 协议状态机驱动（linear/develop/debug/refactor），支持门控与子任务 |
| `mpm 记录` | `memo` | 变更备忘录 |
| `mpm 历史` | `system_recall` | 记忆召回 |
| `mpm 人格` | `persona` | 切换 AI 人格 |
| `mpm 时间线` | `open_timeline` | 项目演进可视化 |

---

## 架构

```
┌─────────────────────────────────────────────────────────────┐
│                        MCP Client                           │
│              (Claude Code / Cursor / Windsurf)              │
└─────────────────────────┬───────────────────────────────────┘
                          │ MCP Protocol
┌─────────────────────────▼───────────────────────────────────┐
│                     Go MCP Server                           │
├──────────────┬──────────────┬───────────────┬───────────────┤
│   感知层      │    调度层     │    记忆层      │    增强层     │
│ code_search  │ manager_     │ memo          │ persona       │
│ code_impact  │ hooks        │ system_recall │ open_timeline │
│ project_map  │ task_chain   │ known_facts   │               │
└──────────────┴──────────────┴───────────────┴───────────────┘
                          │
┌─────────────────────────▼───────────────────────────────────┐
│                   Rust AST Indexer                          │
│  • Tree-sitter 多语言解析 (Go/Python/JS/TS/Rust/Java/C++)   │
│  • canonical_id 精确标识 (func:file.go::Name)               │
│  • callee_id 精确调用链                                      │
│  • DICE 复杂度算法                                           │
└─────────────────────────────────────────────────────────────┘
                          │
┌─────────────────────────▼───────────────────────────────────┐
│                SQLite 多库 (.mpm-data/*)                    │
│  • symbols.db: canonical_id/scope_path/callee_id            │
│  • mcp_memory.db: memos/tasks/known_facts                    │
│  • arch-ast.db: revisions/nodes/edges/proposals/events       │
└─────────────────────────────────────────────────────────────┘
```

---

## AST 索引核心字段

MPM 的 AST 引擎维护 **精确调用链**：

| 字段 | 示例 | 价值 |
|------|------|------|
| `canonical_id` | `func:core/session.go::GetSession` | 全局唯一，消除同名歧义 |
| `scope_path` | `SessionManager::GetSession` | 层级作用域 |
| `callee_id` | `func:core/db.go::Query` | 精确调用链（不是猜测） |

**结果**：`code_impact` 支持 **3 层 BFS 遍历**，完整展示影响传播路径。

---

## 效能对比

| 指标 | 无 MPM | 有 MPM |
|------|--------|--------|
| 符号定位 | 10+ 步搜索 | 1 步精确命中 |
| 首步命中率 | 0% | 100% |
| 影响评估 | 基于猜测 | AST 调用链 |
| Token 消耗 | 4000+ | ~800 |
| 认知恢复 | 每次从零 | 记忆召回 |

详见 [MANUAL_ZH.md](./docs/MANUAL_ZH.md#效能对比)。

---

## 文档

- **[MANUAL_ZH.md](./docs/MANUAL_ZH.md)** - 完整手册（工具详解 + 最佳实践 + Case Study）
- **[README.md](./README.md)** - English Version
- **[MANUAL.md](./docs/MANUAL.md)** - English Manual

---

## 常见搜索问题

- `如何在 MCP 中做代码影响分析？` → 用 `code_impact`
- `如何让 LLM 看懂业务流程？` → 用 `flow_trace`
- `大型仓库索引进度怎么看？` → 用 `index_status`
- `如何强制全量索引？` → `initialize_project(force_full_index=true)`

更多示例见 [MANUAL_ZH.md](./docs/MANUAL_ZH.md)。

---

## OpenCode 多 Agent 模式

MPM 提供了 PM / Architect / Coder / Expert / Spider 五角色 Agent 包，可在 OpenCode 中直接使用。详见 [opencode-agents/README.md](./opencode-agents/README.md)。

---

## 联系方式

- 问题反馈：GitHub Issues
- 邮箱：`halflifezyf2680@gmail.com`

---

## 许可证

MIT License
