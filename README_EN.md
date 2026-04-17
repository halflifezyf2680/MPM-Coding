# MPM-Coding

> **MCP tools for AI coding that actually ships.**

English | [中文](README.md)

![License](https://img.shields.io/badge/license-MIT-blue.svg) ![Go](https://img.shields.io/badge/Go-1.21+-00ADD8.svg) ![MCP](https://img.shields.io/badge/MCP-v1.0-FF4F5E.svg)

---

## The Problem

The fun of AI coding can be quickly destroyed by real projects:

```
"where is that function again?"        → guesses file paths
"this change should be fine"           → no impact analysis
12-step task dies at step 7            → no checkpoint, no resume
"why did we change this last week?"    → nobody remembers
```

MPM doesn't make the model smarter. MPM makes the work **finishable**.

---

## How It Works

```
 locate          analyze          execute          record
┌──────────┐   ┌──────────┐   ┌──────────┐   ┌──────────┐
│          │   │          │   │          │   │          │
│  code_   │──▶│  code_   │──▶│  task_   │──▶│   memo   │
│  search  │   │  impact  │   │  chain   │   │          │
│          │   │          │   │          │   │          │
└──────────┘   └──────────┘   └──────────┘   └──────────┘
  AST-powered     call graph      phased         SSOT
  symbol loc.     risk check      w/ gates       change log
```

Every modification follows: **find → assess → change → record**.
No guessing. No blind edits. No missing trail.

### Why AST Indexing Instead of LSP

The core bottleneck in AI coding isn't model capability — it's **too much garbage in the context window**.

In a 50-file project, if the AI relies on reading files to understand code, it either reads everything (token explosion) or guesses which files matter (misses critical dependencies). Both are disasters. LSP solves IDE human-computer interaction — completions, go-to-definition, rename. AI clients already do these themselves.

MPM solves a different problem: **how to make the AI understand code structure with minimal tokens**.

`code_search` returns the exact location of a symbol definition — not a pile of grep results. `code_impact` returns the full call chain panorama — not making the AI guess file by file who calls what. `flow_trace` returns the main business logic chain — not a directory listing. The **output** of these tools constitutes a context cleaning — injecting only deterministic structural information, filtering out the noise.

This is **attention convergence**: the AI no longer needs to blindly search through oceans of code. Tool outputs have already focused its attention on the few symbols and relationships that matter. What's truly valuable isn't which parser is used under the hood — it's the effect these results produce once injected into the AI's context.

---

## Toolkit

### Navigation

| Tool | What it does |
|------|-------------|
| `code_search` | Find a symbol's exact location. Not grep — AST-precise. |
| `project_map` | See directory structure + symbol inventory at a glance. |
| `flow_trace` | Trace a function's call chain — understand the main path before touching code. |

### Safety

| Tool | What it does |
|------|-------------|
| `code_impact` | "Who calls this?" or "What does this call?" — know the blast radius before editing. |

### Execution

| Tool | What it does |
|------|-------------|
| `task_chain` | Long task? Split into phases with gate checks. Survives session restarts. |
| `system_hook` | Blocked? Hang a hook, come back later when conditions are met. |

### Memory

| Tool | What it does |
|------|-------------|
| `memo` | Record *why* you changed something. Persists across sessions. |
| `system_recall` | "Did we fix something like this before?" — search history. |
| `known_facts` | Store hard-won rules so the AI doesn't repeat mistakes. |

### System

| Tool | What it does |
|------|-------------|
| `initialize_project` | Bootstrap AST index + generate project rules. One-time setup. |
| `index_status` | Monitor background index build progress. |
| `persona` | Switch AI personality for different contexts. |

---

## Quick Start

```bash
# Build
# Windows
powershell -ExecutionPolicy Bypass -File scripts\build-windows.ps1
# Linux/macOS
./scripts/build-unix.sh
```

Point your MCP client at `mcp-server-go/bin/mpm-go(.exe)`, then:

```text
1) initialize_project
2) Paste _MPM_PROJECT_RULES.md into your client's system rules
3) Tell it what to do — the AI will follow the protocol automatically
```

That's it. The AI handles the tool orchestration. You make the decisions.

---

## Usage Example

Paste this into your MCP client:

```text
Read _MPM_PROJECT_RULES.md and follow it strictly.

Task: Fix the null pointer crash in UserService.getProfile.
Requirements:
1. Use code_search to locate the function
2. Use code_impact to check who calls it
3. Fix the bug
4. Use memo to record why this change was needed
```

The AI will execute: `initialize_project` → `code_search` → `code_impact` → edit → `memo`.

---

## Installation

### From Release

Download from [Releases](https://github.com/halflifezyf2680/MPM-Coding/releases):

| Platform | File |
|----------|------|
| Windows x64 | `mpm-windows-amd64.zip` |
| Linux x64 | `mpm-linux-amd64.tar.gz` |
| macOS Universal | `mpm-darwin-universal.tar.gz` |

Unzip. Point your MCP client at `mpm-go`. Done.

### From MCP Registry

Available on the [MCP Registry](https://modelcontextprotocol.io) as `io.github.halflifezyf2680/mpm-coding`.

### From Source

```bash
git clone https://github.com/halflifezyf2680/MPM-Coding.git
cd MPM-Coding
powershell -ExecutionPolicy Bypass -File scripts\build-windows.ps1  # or ./scripts/build-unix.sh
```

---

## Documentation

| Doc | Description |
|-----|-------------|
| [QUICKSTART_EN.md](./QUICKSTART_EN.md) | 5-minute setup guide |
| [docs/MANUAL_EN.md](./docs/MANUAL_EN.md) | Full manual — all tools, patterns, and case studies |
| [README.md](./README.md) | 中文版 |
| [QUICKSTART.md](./QUICKSTART.md) | 中文快速上手 |
| [docs/MANUAL.md](./docs/MANUAL.md) | 中文完整手册 |

---

## Architecture

[View interactive architecture diagram](https://halflifezyf2680.github.io/MPM-Coding/architecture.html)

```
mcp-server-go/
├── cmd/server/main.go              # Entry point (StdIO MCP Server)
├── internal/
│   ├── tools/    (14 files)        # MCP tool implementations
│   ├── core/     (6 files)         # Data layer — SQLite + MemoryLayer (SSOT)
│   └── services/                    # AST indexer (Tree-sitter, multi-language)
└── configs/                         # Default configurations
```

- **Go 1.21+** — zero CGO, pure `modernc.org/sqlite`
- **Tree-sitter** — Rust AST indexer for Go, Rust, Python, TS/JS, Java, C/C++, HTML, CSS
- **SQLite** — embedded storage in `.mpm-data/` (never committed)

---

## FAQ

| Question | Tool |
|----------|------|
| How to find a function/class? | `code_search` |
| How to check impact before editing? | `code_impact` |
| How to understand a module's call chain? | `flow_trace` |
| How to run a long task reliably? | `task_chain` |
| How to check index build progress? | `index_status` |
| How to force full re-index? | `initialize_project(force_full_index=true)` |

Full manual: [docs/MANUAL_EN.md](./docs/MANUAL_EN.md)

---

## License

MIT

This project uses [tree-sitter](https://github.com/tree-sitter/tree-sitter) (MIT License) for AST parsing.
