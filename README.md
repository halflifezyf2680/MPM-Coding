# MPM-Coding

> **让 AI 编程从"能演示"变成"能交付"**

中文 | [English](README_EN.md)

![License](https://img.shields.io/badge/license-MIT-blue.svg) ![Go](https://img.shields.io/badge/Go-1.21+-00ADD8.svg) ![MCP](https://img.shields.io/badge/MCP-v1.0-FF4F5E.svg)

---

## 问题

AI 写代码的快乐，很容易被真实项目剥夺：

```
"那个函数在哪来着？"               → 猜文件路径
"我觉得这样改应该没问题"            → 不做影响分析
12 步的任务跑到第 7 步挂了         → 没有检查点，无法续传
"上周为什么改这个？"               → 谁都说不清
```

MPM 不负责让模型变聪明。MPM 负责**把活干完**。

---

## 原理

```
  定位            分析            执行            记录
┌──────────┐   ┌──────────┐   ┌──────────┐   ┌──────────┐
│          │   │          │   │          │   │          │
│  code_   │──▶│  code_   │──▶│  task_   │──▶│   memo   │
│  search  │   │  impact  │   │  chain   │   │          │
│          │   │          │   │          │   │          │
└──────────┘   └──────────┘   └──────────┘   └──────────┘
  AST 精确        调用链         分阶段          SSOT
  符号定位        风险评估        门控验收        变更日志
```

每次修改必须走：**找定位 → 查影响 → 改代码 → 记原因**。
不猜。不盲改。不留死角。

---

## 工具箱

### 导航

| 工具 | 干什么 |
|------|--------|
| `code_search` | 精确定位符号。不是 grep，是 AST 级精确查找。 |
| `project_map` | 一眼看到目录结构和符号清单。 |
| `flow_trace` | 追踪函数调用链——改代码之前先看懂主链路。 |

### 安全

| 工具 | 干什么 |
|------|--------|
| `code_impact` | "谁调用了它？" 或 "它调用了谁？"——动手前先看爆炸半径。 |

### 执行

| 工具 | 干什么 |
|------|--------|
| `task_chain` | 长任务？拆成阶段，每阶段有门控验收。会话断了也能续。 |
| `system_hook` | 被阻塞？挂个钩子，条件满足后再继续。 |

### 记忆

| 工具 | 干什么 |
|------|--------|
| `memo` | 记录"为什么改"。跨会话持久保留。 |
| `system_recall` | "之前是不是修过类似的？"——搜索历史记录。 |
| `known_facts` | 存储铁律和踩坑经验，不让 AI 重蹈覆辙。 |

### 系统

| 工具 | 干什么 |
|------|--------|
| `initialize_project` | 初始化 AST 索引 + 生成项目规则。一次性操作。 |
| `index_status` | 查看后台索引进度。 |
| `persona` | 切换 AI 人格，适配不同场景。 |

---

## 快速开始

```bash
# 编译
# Windows
powershell -ExecutionPolicy Bypass -File scripts\build-windows.ps1
# Linux/macOS
./scripts/build-unix.sh
```

让 MCP 客户端指向 `mcp-server-go/bin/mpm-go(.exe)`，然后：

```text
1) initialize_project
2) 把生成的 _MPM_PROJECT_RULES.md 放进客户端系统规则
3) 直接提需求——AI 会自动按协议执行
```

就这样。工具编排交给 AI，决策权在你手上。

---

## 使用示例

把这段直接贴进 MCP 客户端：

```text
读取 _MPM_PROJECT_RULES.md 并严格遵守。

任务：修复 UserService.getProfile 的空指针崩溃。
要求：
1. 用 code_search 定位函数
2. 用 code_impact 检查谁在调用它
3. 修复 Bug
4. 用 memo 记录为什么这样改
```

AI 会自动执行：`initialize_project` → `code_search` → `code_impact` → 改代码 → `memo`。

---

## 安装

### 从 Release 安装

从 [Releases](https://github.com/halflifezyf2680/MPM-Coding/releases) 下载：

| 平台 | 文件 |
|------|------|
| Windows x64 | `mpm-windows-amd64.zip` |
| Linux x64 | `mpm-linux-amd64.tar.gz` |
| macOS Universal | `mpm-darwin-universal.tar.gz` |

解压。让 MCP 客户端指向 `mpm-go`。完事。

### 从 MCP Registry 安装

已在 [MCP Registry](https://modelcontextprotocol.io) 发布：`io.github.halflifezyf2680/mpm-coding`

### 从源码编译

```bash
git clone https://github.com/halflifezyf2680/MPM-Coding.git
cd MPM-Coding
powershell -ExecutionPolicy Bypass -File scripts\build-windows.ps1  # 或 ./scripts/build-unix.sh
```

---

## 文档

| 文档 | 说明 |
|------|------|
| [QUICKSTART.md](./QUICKSTART.md) | 5 分钟上手指南 |
| [docs/MANUAL.md](./docs/MANUAL.md) | 完整手册——全部工具、模式、案例 |
| [README_EN.md](./README_EN.md) | English overview |
| [QUICKSTART_EN.md](./QUICKSTART_EN.md) | English quickstart |
| [docs/MANUAL_EN.md](./docs/MANUAL_EN.md) | English manual |

---

## 架构

```
mcp-server-go/
├── cmd/server/main.go              # 入口 (StdIO MCP Server)
├── internal/
│   ├── tools/    (14 files)        # MCP 工具实现
│   ├── core/     (6 files)         # 数据层 — SQLite + MemoryLayer (SSOT)
│   └── services/                    # AST 索引器 (Tree-sitter, 多语言)
└── configs/                         # 默认配置
```

- **Go 1.21+** — 零 CGO，纯 `modernc.org/sqlite`
- **Tree-sitter** — Rust AST 索引器，支持 Go、Rust、Python、TS/JS、Java、C/C++、HTML、CSS
- **SQLite** — 嵌入式存储，数据在 `.mpm-data/`（不提交到 git）

---

## 常见问题

| 问题 | 用什么 |
|------|--------|
| 怎么找函数/类？ | `code_search` |
| 改代码前怎么查影响范围？ | `code_impact` |
| 怎么看懂一个模块的调用链？ | `flow_trace` |
| 长任务怎么可靠执行？ | `task_chain` |
| 大仓库索引进度怎么看？ | `index_status` |
| 怎么强制全量索引？ | `initialize_project(force_full_index=true)` |

完整手册：[docs/MANUAL.md](./docs/MANUAL.md)

---

## 许可证

MIT
