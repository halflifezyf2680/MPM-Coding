# MPM-Coding MCP

> **把 AI 编程从"过程演示"变成"工程交付"**

[English](README.md) | 中文

![License](https://img.shields.io/badge/license-MIT-blue.svg) ![Go](https://img.shields.io/badge/Go-1.21+-00ADD8.svg) ![MCP](https://img.shields.io/badge/MCP-v1.0-FF4F5E.svg)

---

## AI 编程的快乐，通常死在真实项目里

你用 AI 写过真实项目就会遇到同一组问题：AI 看不懂真正的业务逻辑、读代码和文档老是遗漏细节、上下文一长就容易出错、改动影响范围靠猜、任务一长就跑偏/遗漏、中断一次就很难续上、甚至根本无法做长期任务，最后还记不得“到底改了啥，为啥这么改”。

MPM 不负责让 LLM 更聪明、更会聊，那是模型本身的工作。MPM 做的是让它按工程化的方式把事做完：该先找代码就先找（`code_search`），该先看影响就先看（`code_impact`），任务一长就用 `task_chain` 分阶段 + 验收点跑稳，最后用 `memo` 把“为什么这么改”留下来。

在 AI coding 里，聪明从来不是秀操作，而是稳：能把实际问题解决掉，能复盘，能续跑，少猜、少漏、少返工。

甚至你把 git 历史搞乱了（或者仓库重建了），只要 `.mpm-data/` 还在，`memo` 里的思路和历程也还在。后面用 AI 复原实现会省很多时间，顺手还能把实现整理得更干净。

### 先试一下（1分钟）

如果你第一次看到这个项目，不想先读完整文档，直接按下面做就能跑起来：

1) 按 `QUICKSTART.md` 把 `mpm-go(.exe)` 接入你的 MCP 客户端
2) 在聊天里调用 `initialize_project`
3) 把生成的 `_MPM_PROJECT_RULES.md` 粘贴到客户端系统规则
4) 直接复制粘贴下面的“完整闭环示例”

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
| `grep "某函数名"` → 50 条结果 | `code_search("某函数名")` → 精确到文件:行号 |
| "我觉得这里改了就行" | `code_impact` → 完整调用链分析 |
| 新开对话从零开始 | `system_recall` → 把历史决策和改动原因拉回来 |
| 不知道功能为何改了 | `memo` → 记录为什么这么改（`dev-log.md` 会自动刷新） |
| 长任务容易跑偏/遗漏/不可恢复 | `task_chain` → 稳定的长任务链 + 门控验收 |

### 实用流程：一次完整闭环（示例）

以下是一个可直接复制粘贴的完整任务示例，复制后粘贴到任意 MCP 客户端即可运行。

#### 常规模式（推荐新手）

```text
先读取 _MPM_PROJECT_RULES.md 并按规则执行。

任务：修复 <你的问题>。
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
任务：修复 <你的问题>。

阶段要求：
1. 定位阶段：用 code_search 找到目标函数
2. 分析阶段：用 code_impact 评估影响范围
3. 实现阶段：修复并通过测试
4. 收尾阶段：用 memo 记录变更原因

每个阶段完成后报告结果，等待确认再继续。
```

#### 闭环检查清单

- **理解**：`project_map`（结构）/ `flow_trace`（主链阅读）
- **定位**：`code_search` 精确找到符号
- **评估**：`code_impact` 分析调用链影响
- **修改**：编写代码，修复编译错误
- **验证**：运行测试确认功能正确
- **记录**：`memo` 归档变更原因

> ⚠️ **数据卫生**：`.mpm-data/` 目录存储本地数据，不应提交到版本控制。
>
> **项目绑定**：`initialize_project` 会创建 `.mpm-data/project_config.json` 作为锚点。后续会话自动绑定到此项目根。若在工作区聚合目录下发现多个锚点，MPM 会拒绝猜测并要求显式指定 `project_root`。

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
帮我定位并修复一个具体问题，并按规则执行
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

## 文档

- **[MANUAL_ZH.md](./docs/MANUAL_ZH.md)** - 完整手册（所有 13 个工具详解 + 最佳实践 + Case Study）
- **[MANUAL.md](./docs/MANUAL.md)** - English Manual
- **[README.md](./README.md)** - English Version

---

## 常见搜索问题

- `如何在 MCP 中做代码影响分析？` → 用 `code_impact`
- `如何让 LLM 看懂业务流程？` → 用 `flow_trace`
- `如何让 LLM 快速看懂一个模块/目录在系统里是怎么运作的？` → 用 `flow_trace` 查看入口调用链
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
