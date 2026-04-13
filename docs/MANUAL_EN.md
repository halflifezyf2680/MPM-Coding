# MPM Complete Manual

[中文](MANUAL.md) | English

---

## Table of Contents

1. [Core Concepts](#1-core-concepts)
2. [Tool Reference](#2-tool-reference)
3. [Workflows](#3-workflows)
4. [FAQ](#4-faq)

---

## 1. Core Concepts

### 1.1 Execution Loop

```
 locate          analyze          execute          record
┌──────────┐   ┌──────────┐   ┌──────────┐   ┌──────────┐
│  code_   │──▶│  code_   │──▶│  task_   │──▶│   memo   │
│  search  │   │  impact  │   │  chain   │   │          │
└──────────┘   └──────────┘   └──────────┘   └──────────┘
```

- **Short tasks**: `code_search → code_impact → edit → memo`
- **Long tasks**: add `task_chain` in the middle; use `system_hook` when blocked

### 1.2 AST Indexing

`initialize_project` triggers background index build:

1. Tree-sitter parses source, extracts functions/classes/methods
2. Generates `canonical_id` (globally unique) per symbol
3. Builds call edges (caller → callee), forms a call graph
4. Writes to `symbols.db` (SQLite)

`code_search` queries this DB. `code_impact` runs BFS propagation on the call graph.

Supported languages: Go, Rust, Python, TS/JS, Java, C/C++, HTML, CSS.

---

## 2. Tool Reference

### 2.0 Overview

| # | Tool | Category | One-liner |
|---|------|----------|----------|
| 1 | `initialize_project` | System | Bootstrap project env + AST index |
| 2 | `index_status` | System | Check index build progress |
| 3 | `project_map` | Navigation | Project structure + symbol inventory |
| 4 | `flow_trace` | Navigation | Function call chain tracing |
| 5 | `code_search` | Navigation | AST-level symbol lookup |
| 6 | `code_impact` | Safety | Call chain impact analysis |
| 7 | `task_chain` | Execution | Phased task chain with gates + resume |
| 8 | `system_hook` | Execution | Block suspend / resume |
| 9 | `memo` | Memory | Change record (SSOT) |
| 10 | `system_recall` | Memory | Search history |
| 11 | `known_facts` | Memory | Archive rules and pitfalls |
| 12 | `persona` | Enhancement | AI personality switching |
| 13 | `open_timeline` | Enhancement | Project evolution timeline visualization |

---

### 2.1 initialize_project

Initialize project environment. **Prerequisite for all other operations.**

**Parameters**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `project_root` | string | No | Absolute path to project root. Auto-detected if empty. |
| `force_full_index` | boolean | No | Force full indexing. Default: false. |

**Behavior**:
- Creates `.mpm-data/` (SQLite DBs + index)
- Creates `.mpm-data/project_config.json` as project anchor
- Starts AST index build in background
- Generates `_MPM_PROJECT_RULES.md`

---

### 2.2 index_status

Check AST index build progress.

**Parameters**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `project_root` | string | No | Uses current session project if empty. |

**Returns**: `status`(running/success/failed), `mode`, `heartbeat`(processed/total), `db_file_sizes`

---

### 2.3 project_map

Project structure navigation.

**Parameters**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `level` | string | No | `structure` (dirs) / `symbols` (functions). Default: symbols |
| `scope` | string | No | Directory scope. Empty = entire project. |

**Example output**:
```
66 files | 168 symbols
Complexity: High: 6 | Med: 18 | Low: 144

internal/tools/ (14 files)
  search_tools.go
    wrapSearch L47 [HIGH:83.5]
    SearchArgs L15
```

---

### 2.4 code_search

AST-level symbol lookup. Use when you know the name but not the location.

**Parameters**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `query` | string | Yes | Symbol name (not natural language) |
| `scope` | string | No | Directory scope |
| `search_type` | string | No | `any` / `function` / `class`. Default: any |

**5-layer fallback matching**: exact → prefix/suffix → substring → levenshtein → stem. Falls back to ripgrep if no AST match.

**Returns**: best match with `canonical_id`, file location, signature, call relations + candidate list.

---

### 2.5 code_impact

Call chain impact analysis. Use before modifying code.

**Parameters**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `symbol_name` | string | Yes | Exact symbol name |
| `direction` | string | No | `backward` (who calls me) / `forward` (what I call) / `both`. Default: backward |

**Returns**: risk level (low/medium/high), complexity score, direct/indirect caller lists, modification checklist.

---

### 2.6 flow_trace

Trace function call chains for code reading.

**Parameters**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `symbol_name` | string | No* | Entry symbol name |
| `file_path` | string | No* | Entry file path |
| `scope` | string | No | Directory scope |
| `direction` | string | No | `backward` / `forward` / `both`. Default: both |
| `mode` | string | No | `brief` / `standard` / `deep`. Default: brief |
| `max_nodes` | int | No | Max nodes. Default: 40 |

\* Provide at least one. If both given, `symbol_name` takes priority.

---

### 2.7 task_chain

Phased task chain with gate verification and cross-session resume.

**Modes**:

| Mode | Description | Required params |
|------|-------------|-----------------|
| `init` | Initialize | `task_id`, `description`, `protocol` or `phases` |
| `update` | Modify goal | `task_id`, `description`/`phases` |
| `start` | Begin phase | `task_id`, `phase_id` |
| `complete` | Complete phase | `task_id`, `phase_id`, `summary` |
| `spawn` | Dispatch subtasks | `task_id`, `phase_id`, `sub_tasks` |
| `complete_sub` | Complete subtask | `task_id`, `phase_id`, `sub_id` |
| `status` | View progress | `task_id` |
| `resume` | Resume task | `task_id` |
| `finish` | Close chain | `task_id` |

**Built-in protocols**:

| Protocol | Phases | Use case |
|----------|--------|----------|
| `linear` | main | One-shot deterministic tasks |
| `develop` | implement → verify_gate → finalize | Feature development |
| `debug` | fix → verify_gate → finalize | Bug fixes |
| `refactor` | refactor → verify_gate → finalize | Refactoring |

Gate phases require `result="pass"` or `result="fail"`. Fail blocks until retry.

**Example**:
```javascript
// Initialize
task_chain(mode="init", task_id="AUTH", protocol="develop", description="Add OAuth2")

// Complete implementation
task_chain(mode="complete", task_id="AUTH", phase_id="implement", summary="Flow implemented")

// Pass gate
task_chain(mode="complete", task_id="AUTH", phase_id="verify_gate", result="pass", summary="Tests pass")

// Finalize
task_chain(mode="complete", task_id="AUTH", phase_id="finalize", summary="Done")
```

---

### 2.8 system_hook

Suspend/resume mechanism for blocked tasks.

**Modes**:

| Mode | Description | Required params |
|------|-------------|-----------------|
| `create` | Create hook | `description`, `priority` |
| `list` | List hooks | `status` (open/closed) |
| `release` | Release hook | `hook_id`, `result_summary` |

**Priority**: `high` / `medium` / `low`

---

### 2.9 memo

Change record. Call after every code modification.

**Parameters**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `items` | array | Yes | Array of records with `category`, `entity`, `act`, `path`, `content` |
| `lang` | string | No | `zh` / `en`. Default: zh |

**category**: `修改` / `开发` / `决策` / `重构` / `避坑`

**Example**:
```javascript
memo(items=[{
  category: "fix",
  entity: "GetSession",
  act: "add idempotency check",
  path: "core/session.go",
  content: "prevent duplicate requests from creating multiple sessions"
}])
```

Data written to both SQLite (`mcp_memory.db`) and Markdown (`dev-log.md`).

---

### 2.10 system_recall

Search history. Check before modifying to avoid repeating past mistakes.

**Parameters**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `keywords` | string | Yes | Space-separated keywords |
| `category` | string | No | Type filter |
| `limit` | int | No | Max results. Default: 20 |

Searches: Memos + Known Facts + Task records. Known Facts shown first.

---

### 2.11 known_facts

Archive verified rules and pitfalls.

**Parameters**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | `铁律` / `避坑` / `规范` / `逻辑` |
| `summarize` | string | Yes | Description |

Retrievable via `system_recall`.

---

### 2.12 persona

AI personality management.

**Modes**: `list` / `activate` / `create` / `update` / `delete`

Optional fields for create/update: `name`, `display_name`, `hard_directive`, `aliases`, `style_must`, `style_signature`, `style_taboo`, `triggers`

Data saved to `.mcp-config/personas.json`.

---

### 2.13 open_timeline

Generate project evolution timeline (HTML) from memo records. Auto-opens in browser.

No parameters.

---

## 3. Workflows

### 3.1 Setup

```
1. initialize_project
2. Put _MPM_PROJECT_RULES.md into your client's system rules
3. Start working
```

### 3.2 Code modification

```
code_search(query="target symbol")          → locate
code_impact(symbol_name="target symbol")    → assess impact
(read code, make changes)
memo(items=[{...}])                         → record why
```

### 3.3 Long tasks

```
task_chain(mode="init", protocol="develop")      → initialize
task_chain(mode="start", phase_id="1")           → begin phase
(do the work)
task_chain(mode="complete", phase_id="1", ...)    → complete phase
(repeat until all phases done)
task_chain(mode="finish")                        → close
```

### 3.4 Blocked

```
system_hook(mode="create", description="waiting for API key")   → suspend
(user provides the key)
system_hook(mode="release", hook_id="#001", result_summary="configured")  → resume
```

---

## 4. FAQ

### Which languages are supported?

Go, Rust, Python, TypeScript/JavaScript, Java, C/C++, HTML (structural symbols), CSS (structural symbols)

### Where is data stored?

| Data | Location |
|------|----------|
| AST index | `.mpm-data/symbols.db` |
| Memos/Tasks/Hooks | `.mpm-data/mcp_memory.db` |
| Human-readable log | `.mpm-data/dev-log.md` |
| Project anchor | `.mpm-data/project_config.json` |
| Project rules | `_MPM_PROJECT_RULES.md` |

`.mpm-data/` is never committed to git.

### How to switch between projects?

Each project has its own `.mpm-data/`. When multiple projects exist under a workspace, `initialize_project` requires explicit `project_root`.

### Can I use tools before indexing finishes?

No. `code_search` and related tools depend on the AST index. Use `index_status` to check progress on large repos.

### Do I need to reinitialize for a new chat?

No. As long as the MCP Server is running and `.mpm-data/` exists, just continue. For new sessions, run `system_recall` to restore context.

---

*MPM Manual v2.3 — 2026-04*
