# MPM-Coding MCP

> **把 AI 编程从“会演示”拉回“能交付”**

中文 | [English](README_EN.md)

![License](https://img.shields.io/badge/license-MIT-blue.svg) ![Go](https://img.shields.io/badge/Go-1.21+-00ADD8.svg) ![MCP](https://img.shields.io/badge/MCP-v1.0-FF4F5E.svg)

---

## 在真实项目中存活的 AI 编程

AI 写代码一直是一件很好玩的事……直到你把它放进一个真实的业务仓库里：

- 模型经常忘记上下文（“刚刚我要改的代码在哪来着？”）
- 靠猜来“盲改”（“我觉得这样改应该没问题”）
- 长任务很容易跑偏、跳步，或者中途死掉
- 隔天你根本无法回答“当初改了什么，为什么要这么改？”

MPM 并不是试图让 LLM 变得“更聪明”或者“更会聊天”，那是模型自己的工作。
MPM 的目的是让工作**能做完（finishable）**：先精确查代码 (`code_search`)，再查影响范围 (`code_impact`)，对于长任务使用带验收门控的流程链 (`task_chain`)，最后把所有的“为什么”固化下来 (`memo`)。

在 AI 编程的语境里，“聪明”通常意味着稳如老狗：能解决真问题、留痕迹、能随时续传，少一点盲猜，少一点失误。

哪怕你的 Git 历史变成了一坨乱麻（或者你想重建整个仓库），AI 留下的思考路径依然可以保存完好：`memo` 会把“为什么”写进 `.mpm-data/`。
备份好 `.mpm-data/`，你用 AI 重构通常会更快、也更干净。

---

## 30 秒上手

```text
1) initialize_project
2) 把生成的 _MPM_PROJECT_RULES.md 放进客户端系统规则
3) 直接提任务：帮我修复 XXX，并按规则执行
```

如果你一开始就遵循这三步，你就可以立刻开启高效工作流，而无需提前学习每一个工具的用法。

**与众不同的核心机制**：

| 传统方式 | MPM 方式 |
|---------|---------|
| `grep` 一把梭，结果一堆重名 | `code_search` 先定位到精确符号 |
| “我觉得这样改应该行” | `code_impact` 先看调用链风险 |
| 每次新开会话从头开始 | `system_recall` 穿透会话的跨周期记忆 |
| 长任务靠聊天上下文硬撑 | `task_chain` 拆分带有门控验收点的长线任务链 |
| 改完没记录，隔天全忘 | 用 `memo` 记录决策过程，成为代码仓库单一事实来源 |

---

## 快速实战参考

这里提供一个复制即用的例子，贴进 MCP 客户端即可运行：

#### 标准模式（新手推荐）

```text
先调用 initialize_project 获取上下文，读取规则并严格遵守。

核心任务：帮我在 User 服务里加一个获取个人资料的 getProfile 接口。
如果你遇到不确定的地方（或者找不到应该改哪个文件），必须要停下来问我。不要自己乱猜。
完成后，你必须调用 memo 工具记录此次修改的原因和结论，并且语言使用中文。
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

让客户端指向：`mcp-server-go/bin/mpm-go(.exe)`

### 3. 开始使用

```text
初始化项目
帮我定位并修复一个具体问题，并按规则执行
```

初始化后会自动生成 `_MPM_PROJECT_RULES.md`。它相当于这个项目给 LLM 的本地操作规程，建议每次新会话都先读取。

### 4. 本地打包

```powershell
python package_product.py
```

输出目录固定为 `mpm-release/MyProjectManager`。



## 文档入口

- [QUICKSTART.md](./QUICKSTART.md) - 中文快速接入
- [docs/MANUAL.md](./docs/MANUAL.md) - 中文完整手册
- [README_EN.md](./README_EN.md) - English overview
- [QUICKSTART_EN.md](./QUICKSTART_EN.md) - English quickstart
- [docs/MANUAL_EN.md](./docs/MANUAL_EN.md) - English manual

> 发布包会保留主文档与手册，但不会再携带 `opencode-agents`，也不会打包 `docs/wiki/`。

---

## 常见问题

- `如何在改代码前先看影响范围？` -> 用 `code_impact`
- `如何快速看懂一个模块主链？` -> 用 `flow_trace`
- `如何快速找到某个函数/类？` -> 用 `code_search`
- `大型仓库索引进度怎么看？` -> 用 `index_status`
- `如何强制全量索引？` -> `initialize_project(force_full_index=true)`

---

## 联系方式

- 问题反馈：GitHub Issues
- 邮箱：`halflifezyf2680@gmail.com`

---

## 许可证

MIT License
