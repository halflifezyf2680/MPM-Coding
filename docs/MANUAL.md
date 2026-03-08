# MPM Complete Manual

> **From "Chatting" to "Controlled Delivery"**

[中文](MANUAL_ZH.md) | English

---

## Table of Contents

1. [Core Concepts](#1-core-concepts)
2. [Tool Reference](#2-tool-reference)
3. [Best Practices](#3-best-practices)
4. [Performance Comparison](#4-performance-comparison)
5. [FAQ](#5-faq)

---

## 1. Core Concepts

### 1.1 Core Concepts at a Glance

This section is not a tool list. It explains the operating model behind MPM:

1. Fix the process (locate first, assess impact first, then edit)
2. Fix task state (long tasks are phased, gated, and resumable)
3. Fix decision memory (store why, not only what)

In practice, it maps to these core concepts:

| Core Concept | One-line Meaning | Main Tools | Practical Result |
|------------|---------|--------------|------------------|
| **Project anchoring** | Bind session to the correct project root first | `initialize_project` | Ensure indexes/memory land in the right directory (some MCP clients do not guarantee the server CWD is the project root) |
| **Locate before edit** | Find real entry points and symbols before changing code | `project_map` / `flow_trace` / `code_search` | Less random file-hopping |
| **Impact before change** | See caller/callee impact before editing functions | `code_impact` | Fewer surprise breakages |
| **Task state machine** | Run long work in phases with explicit gates | `task_chain` | Long tasks stay stable and reviewable |
| **Suspend/resume on blockers** | Pause when blocked and resume with context later | `system_hook` | Interruptions do not reset progress |
| **Memory as audit trail** | Record why decisions were made | `memo` / `system_recall` / `known_facts` | Better replay, migration, and reconstruction |

Note: this is mainly an IDE/MCP-client issue. When a client spawns the server, the process working directory may not be the project root. Without program-level handling, data can end up next to the server binary or under the user's home directory.

Project-level `.mpm-data/` is anchored to the real project root (symbols.db / mcp_memory.db / project_config.json live there). `initialize_project` makes that root explicit so indexes and memory do not land in the wrong place.

### 1.2 Execution Loop (Runtime View)

In practice, the core concepts usually run in this order:

```
initialize_project
  -> project_map / flow_trace (optional orientation)
  -> code_search (pinpoint symbols)
  -> code_impact (check impact before edits)
  -> task_chain (recommended for long tasks, with phases/gates)
  -> system_hook (suspend/resume when blocked)
  -> implement + test
  -> memo (record the why)
  -> system_recall / known_facts (reuse in future sessions)
```

- **Short tasks**: `code_search -> code_impact -> edit/test -> memo`
- **Long tasks**: add `task_chain`; use `system_hook` when blocked
- **Main goal**: every step stays explainable, resumable, and auditable

### 1.3 AST Indexing Principles

Indexing is not just "scan file names". It is a reusable data pipeline with 5 steps:

1) Parse source with Tree-sitter
- Extract functions/methods/classes from source files
- Store file location (path, line range, signature)

2) Normalize symbol identity
- Generate `canonical_id` for each symbol (globally unique)
- Maintain `scope_path` and `qualified_name`
- Result: no guessing when symbols share names

3) Build call graph edges
- First record calls as caller -> callee_name
- Then run a linking pass to resolve `callee_id` when possible
- Result: call chains move from name-based to ID-based links

4) Serve query-time retrieval
- `code_search` runs a 5-layer progressive matcher (exact / prefix_suffix / substring / levenshtein / stem)
- Merge candidates, dedupe by `canonical_id`, then return best match + alternatives

5) Power impact analysis
- `code_impact` runs multi-layer BFS over the call graph
- Returns risk level, direct/indirect impact, and a modification checklist

Core persisted fields:

| Field | Description | Example |
|-------|-------------|---------|
| `canonical_id` | Globally unique identifier | `func:core/auth.go::Login` |
| `scope_path` | Hierarchical scope | `AuthManager::Login` |
| `callee_id` | Precise call target | `func:db/query.go::Exec` |

In short: AST indexing turns symbol lookup and impact checks from text guessing into structured, repeatable queries.

---

## 2. Tool Reference

### 2.0 Tool Inventory (14 Tools)

| # | Tool Name | Category | Description |
|---|-----------|----------|-------------|
| 1 | `initialize_project` | System | Initialize project environment and database |
| 2 | `index_status` | System | Check background AST indexing status |
| 3 | `project_map` | Perception | Project structure navigation map |
| 4 | `module_map` | Perception | Module-level business portrait (scope-focused analysis) |
| 5 | `flow_trace` | Perception | Business flow trace (entry/upstream/downstream) |
| 6 | `code_search` | Perception | AST-based symbol lookup with precise location |
| 7 | `code_impact` | Perception | Call chain impact analysis |
| 8 | `task_chain` | Scheduling | Protocol state machine for multi-step tasks |
| 9 | `system_hook` | Scheduling | Create/list/release todo hooks (unified tool) |
| 10 | `memo` | Memory | Record change documentation (SSOT) |
| 11 | `system_recall` | Memory | Retrieve historical decisions and changes |
| 12 | `known_facts` | Memory | Archive verified rules and pitfall experiences |
| 13 | `persona` | Enhancement | AI personality management |
| 14 | `open_timeline` | Enhancement | Generate and open project evolution timeline |

---

### 2.1 System Tools

#### initialize_project

**Purpose**: Initialize project environment with AST indexing, tech stack detection, and project rules generation. **Must call before any other MPM operation.**

**Triggers**: `mpm init`, `mpm 初始化`

**Parameters**:
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `project_root` | string | No* | Absolute path to project root. Auto-detected if empty. |
| `force_full_index` | boolean | No | Force full indexing (disable bootstrap strategy). Default: `false` |

*If empty, attempts auto-detection via `.mpm-data/project_config.json` anchor.

**Outputs**:
- Success message with project path
- AST indexing status (background, mode=auto/full)
- Path to generated `_MPM_PROJECT_RULES.md`

**Gotchas**:
- Manual `project_root` must use absolute paths
- Creates `.mpm-data/project_config.json` as project anchor
- Generates `_MPM_PROJECT_RULES.md` for LLM reference
- Refuses to guess root if multiple anchors found in workspace

---

#### index_status

**Purpose**: Query background AST indexing task status started by `initialize_project`.

**Triggers**: `mpm index status`, `mpm 索引状态`

**Parameters**:
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `project_root` | string | No | Project root path. Uses session project if empty. |

**Outputs**:
- `status`: running/success/failed
- `mode`: auto/full
- `started_at` / `finished_at` timestamps
- `total_files` / `elapsed_ms`
- `heartbeat`: progress indicator (processed/total)
- `db_file_sizes`: symbols.db and WAL file sizes

**Gotchas**:
- Status file: `.mpm-data/index_status.json`
- Heartbeat file: `.mpm-data/heartbeat`
- Useful for large repositories to monitor indexing progress

---

### 2.2 Perception Tools

#### project_map

**Purpose**: Project navigation map. Use when lost or unsure which file to modify. Provides structured overview with complexity heat map.

**Triggers**: `mpm map`, `mpm structure`, `mpm 地图`, `mpm 结构`

**Parameters**:
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `scope` | string | No | Directory/file scope (empty = entire project) |
| `level` | string | No | Level: "structure"(dirs)/"symbols"(functions+classes). Default: "symbols" |
| `core_paths` | string | No | JSON array of core paths to highlight |

**Outputs**:
- **Project Stats**: File count, symbol count
- **Complexity Heat Map**: High-complexity symbols marked with 🔴
- **Directory Structure** (structure level): File counts per directory
- **Symbol Structure** (symbols level): Functions/classes per file with line ranges

**Example**:
```
📊 Project Stats: 156 files, 892 symbols

🔴 High Complexity Hotspots:
  - SessionManager::Handle (Score: 85)
  - PaymentService::Process (Score: 72)

📁 src/core/ (12 files)
  ├── session.go
  │   └── func GetSession (L45-80) 🔴
  └── config.go
      └── func LoadConfig (L20-40) 🟢
```

**Gotchas**:
- Large outputs (>2000 chars) saved to `.mpm-data/project_map_*.md`
- Use "structure" level for quick directory overview
- Use "symbols" level for detailed code navigation
- Complexity analysis based on DICE algorithm

---

#### module_map

**Purpose**: Module-level business portrait. Generates a "how this area works in the system" profile for a directory or file scope. Helps LLMs build mental models before reading code.

**Triggers**: `mpm module`, `mpm 模块`

**Parameters**:
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `scope` | string | Yes | Module scope (directory or file path) |
| `entry_symbol` | string | No | Optional entry symbol; auto-selected if empty |
| `mode` | string | No | Output level: "brief"/"standard"/"deep". Default: "standard" |
| `max_entries` | integer | No | Max candidate entries to analyze. Default: 3 |

**Outputs**:
- **Module Positioning**: What this area does (based on scope name + top entries + stages)
- **Core Components**: Key files/symbols/types in the scope
- **Main Flow Steps**: Step 1 → Step 2 → Step 3 (inferred from stages and downstream nodes)
- **Top Entries**: High-score entry points for modifications
- **Test Anchors**: Test files in scope or nearby directories
- **Reading Suggestions**: Which files/entries to read first
- **Stage Tags** (standard/deep mode): init/validate/execute/query/persist
- **Side Effect Tags** (standard/deep mode): filesystem/database/network/process/state

**Example**:
```
### 📦 Module Map

#### 📋 模块定位
📁 **tools** 区域 — 阶段特征: execute / query
🎯 主要入口: `RegisterAnalysisTools`

#### 🧩 核心组成
- 📄 `internal/tools/analysis_tools.go`
- 🔹 [callable] `wrapProjectMap`
- 🔹 [callable] `buildFlowSnapshot`

#### 🔄 主流程步骤
- Step 1: [execute]
- Step 2: → `MapProjectWithScope`
- Step 3: → `RenderStandard`

#### 🎯 修改入口 (Top Entries)
- 🔥 `RegisterAnalysisTools` (评分: 85.0)
- 🔥 `wrapProjectMap` (评分: 72.5)

#### ✅ 测试锚点
- ✅ `TestDetectSideEffects_EvidenceFilesystem` @ analysis_tools_test.go

#### 📖 阅读建议
- 1️⃣ 先读 `RegisterAnalysisTools` (internal/tools/analysis_tools.go)
- 2️⃣ 再读 `internal/tools/map_renderer.go`
```

**Gotchas**:
- `scope` is mandatory (unlike `project_map` which defaults to entire project)
- Entry selection is heuristic-based (name patterns, call counts, scores)
- Does NOT perform strict classification (module/feature/subsystem); provides weak labels and positioning hints
- Focuses on "how this area works" rather than directory structure
- Reuses underlying `MapProjectWithScope`, `buildFlowSnapshot`, and `AnalyzeComplexity` services directly

**When to Use**:
- When you need to understand a specific module/area quickly
- Before diving into code in a new directory
- When planning modifications to a subsystem
- To find entry points and test anchors for a scope

---

#### code_search

**Purpose**: AST-based symbol lookup. **Use when you know the name (function/class) but not the file location.** More precise than grep.

**Triggers**: `mpm search`, `mpm locate`, `mpm 定位`, `mpm 符号`, `mpm find`

**Parameters**:
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `query` | string | Yes | Symbol name (NOT natural language) |
| `scope` | string | No | Directory scope (e.g., "internal/core") |
| `search_type` | string | No | Filter: "any"/"function"/"class". Default: "any" |

**Outputs**:
- **Best Match**: Exact symbol with:
  - `node_type`: function/class/method/struct
  - `name`: Symbol name
  - `file_path`: File location
  - `line_start` / `line_end`: Line range
  - `canonical_id`: Unique identifier (e.g., `func:core/auth.go::Login`)
  - `signature`: Function signature
  - `calls`: Functions this symbol calls (top 5)
  - `related_nodes`: Callers (top 5)
- **Other Candidates**: List of similar matches with scores
- **Text Search (Ripgrep)**: Fallback if no AST match found

**Example**:
```
### About Login

Best match: src/auth/login.go L45-67
ID: func:src/auth/login.go::Login
Signature: func Login(ctx context.Context, cred Credentials) (*Token, error)

Other candidates:
  [func] LoginUser @ src/api/user.go (score: 0.85)
```

**5-Layer Fallback Search**:
```
1. Exact match
2. Prefix/suffix match
3. Substring match
4. Levenshtein distance
5. Stem match
```

**Gotchas**:
- Query should be exact symbol name, NOT natural language description
- If no AST match is found, falls back to ripgrep text search (with context)
- `scope` is passed to the indexer; the server also applies a best-effort scope check for the best match
- Type filtering supports function/method and class/struct/interface

---

#### code_impact

**Purpose**: Analyze impact scope when modifying a function or class. **MUST call before modifying functions.** Shows complete call chain.

**Triggers**: `mpm impact`, `mpm dependency`, `mpm 影响`, `mpm 依赖`

**Parameters**:
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `symbol_name` | string | Yes | Exact symbol name (function/class) |
| `direction` | string | No | Direction: "backward"(callers)/"forward"(callees)/"both". Default: "backward" |

**Outputs**:
- **Risk Level**: low/medium/high
- **Complexity Score**: Numeric complexity
- **Affected Nodes**: Total count
- **Direct Callers**: Top 10 with file:line locations
- **Indirect Callers**: Count and top 20 names
- **Modification Checklist**: Target and verification points
- JSON summary for programmatic use

**Example**:
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

**Gotchas**:
- Symbol name must be EXACT code symbol, not string search
- Uses AST analysis, not text matching
- Returns error if symbol is not a function/class definition
- Use `code_search` first if unsure about exact name

---

#### flow_trace

**Purpose**: Business flow trace for understanding main logic chains. Outputs "entry-upstream-downstream" flow summary. More readable than `code_impact`.

**Triggers**: `mpm flow`, `mpm 流程`

**Parameters**:
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `symbol_name` | string | No* | Entry symbol (function/class). Takes priority if both provided. |
| `file_path` | string | No* | Target file path. Alternative to symbol_name. |
| `scope` | string | No | Scope filter (recommended for large projects) |
| `direction` | string | No | Direction: "backward"/"forward"/"both". Default: "both" |
| `mode` | string | No | Output level: "brief"/"standard"/"deep". Default: "brief" |
| `max_nodes` | integer | No | Output node budget. Default: 40 |

*Exactly one of `symbol_name` or `file_path` required. Empty strings are invalid.

**Outputs**:
- **Entry Point**: Symbol name, type, location, score
- **Upstream/Downstream**: Key nodes with impact counts
- **Critical Paths**: Top 3 important paths
- **Stage Summary**: Init/validate/execute/query/persist stages
- **Side Effects**: filesystem/database/network/process/state
- **Recommendations**: Next steps

**Gotchas**:
- `symbol_name` and `file_path` are mutually exclusive; exactly one must be provided
- Do NOT pass file names or basenames (e.g., `SurvivalManager.go`, `SurvivalManager`) to `symbol_name`; use `file_path` for file-level analysis
- When `symbol_name` is not found, tool returns candidate symbols (candidates) for selection
- File mode: analyzes multiple entry candidates, shows top-scored ones
- Use "brief" for quick overview, "standard"/"deep" for more detail
- Scope strongly recommended for large repositories
- Output truncated at `max_nodes` with summary of omitted nodes

---

### 2.3 Scheduling Tools

#### task_chain

**Purpose**: Protocol state machine for multi-step task management. Supports gates, loops, conditional branching, and cross-session persistence.

**Triggers**: `mpm chain`, `mpm taskchain`, `mpm 任务链`, `mpm 续传`

**Parameters**:
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `mode` | string | Yes | Operation mode (see below) |
| `task_id` | string | Mode-dependent | Task identifier |
| `description` | string | init mode | Task description |
| `protocol` | string | init mode | Protocol name: "linear"/"develop"/"debug"/"refactor". Default: "linear" |
| `phase_id` | string | Multiple modes | Phase identifier |
| `result` | string | complete gate | Gate result: "pass"/"fail" |
| `summary` | string | complete modes | Phase/subtask summary |
| `sub_id` | string | complete_sub | Subtask ID |
| `sub_tasks` | array | spawn mode | List of subtasks |
| `phases` | array | init mode | Custom phase definitions |

**Modes**:
| Mode | Description |
|------|-------------|
| `init` | Initialize task chain with protocol |
| `start` | Begin a phase |
| `complete` | Complete a phase (gate types need result) |
| `spawn` | Generate subtasks in loop phase |
| `complete_sub` | Complete a subtask |
| `status` | View task progress (auto-loads from DB) |
| `resume` | Resume/restore task |
| `finish` | Permanently close task chain |
| `protocol` | List available protocols |

**Built-in Protocols**:
| Protocol | Phases | Use Case |
|----------|--------|----------|
| `linear` | main (execute) | Single-step deterministic tasks |
| `develop` | analyze → plan_gate → implement(loop) → verify_gate → finalize | Cross-module development |
| `debug` | reproduce → locate → fix(loop) → verify_gate → finalize | Bug investigation |
| `refactor` | baseline → analyze → refactor(loop) → verify_gate → finalize | Large-scale refactoring |

**Example**:
```javascript
// 1. Initialize a refactoring task
task_chain(mode="init", task_id="AUTH_REFACTOR", protocol="refactor", description="Refactor auth module")

// 2. Complete baseline check
task_chain(mode="complete", task_id="AUTH_REFACTOR", phase_id="baseline", summary="Current tests pass")

// 3. Enter refactor loop
task_chain(mode="spawn", task_id="AUTH_REFACTOR", phase_id="refactor", sub_tasks=[
  {"name": "Decouple SessionStore"},
  {"name": "Rewrite JWT signing"}
])

// 4. Complete a sub-task
task_chain(mode="complete_sub", task_id="AUTH_REFACTOR", phase_id="refactor", sub_id="sub_001", summary="Store extracted to interface")
```

**Gotchas**:
- Default protocol is `linear`
- Use `develop` protocol for large projects with loop phases
- Gate phases require `result="pass"` or `result="fail"`
- Re-init protection: 2nd re-init on same task_id is blocked
- V3 `linear` protocol with `loop` phases replaces old Linear Step Mode

---

#### system_hook

**Purpose**: Unified tool for managing todo hooks (create/list/release). Create and suspend a "hook" when a task cannot proceed due to missing information, user confirmation, or blocking issues.

**Triggers**: `mpm suspend`, `mpm hook`, `mpm todolist`, `mpm release`, `mpm 挂起`, `mpm 待办`, `mpm 待办列表`, `mpm 释放`, `mpm 完成`

**Parameters**:
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `mode` | string | Yes | Operation mode: "create"/"list"/"release" |
| `description` | string | create mode | Todo/blocker description |
| `priority` | string | No | Priority: "high"/"medium"/"low". Default: "medium" |
| `task_id` | string | No | Associated task ID |
| `tag` | string | No | Category tag |
| `expires_in_hours` | integer | No | Expiration time in hours. 0 = never expires. Default: 0 |
| `status` | string | list mode | Filter: "open"/"closed". Default: "open" |
| `hook_id` | string | release mode | Hook identifier (e.g., "#001" or UUID) |
| `result_summary` | string | No | Completion summary (release mode) |

**Modes**:
| Mode | Description |
|------|-------------|
| `create` | Create and suspend a todo/checkpoint |
| `list` | List all hooks filtered by status |
| `release` | Complete and close a hook |

**Outputs**:
- **create**: Hook ID and confirmation details
- **list**: List of hooks with ID, priority, description, expiration status
- **release**: Confirmation with hook ID and result summary

**Gotchas**:
- Default list mode shows only "open" hooks
- Expired hooks are marked as "EXPIRED" in listings
- Use for cross-session task continuity
- Closed hooks no longer appear in default (open) listings
- Use meaningful result summaries for audit trails

---

### 2.4 Memory Tools

#### memo

**Purpose**: Record change documentation. **MUST call after any code modification.** This is the project's Single Source of Truth (SSOT) for evolution history.

**Triggers**: `mpm memo`, `mpm record`, `mpm 存档`

**Parameters**:
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `items` | array | Yes | Array of memo items (see below) |
| `lang` | string | No | Language: "zh"/"en". Default: "zh" |

**MemoItem fields**:
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `category` | string | Yes | Category: "修改"/"开发"/"决策"/"重构"/"避坑" |
| `entity` | string | Yes | Changed entity (filename, function name, module) |
| `act` | string | Yes | Action: "修复Bug"/"新增功能"/"技术选型" |
| `path` | string | Yes | File path |
| `content` | string | Yes | Detailed explanation (WHY, not just WHAT) |

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

**Gotchas**:
- `items` must be an array, even for single item: `[{...}]`
- Record the "why" not just the "what"
- Use user's conversation language for content
- Retrievable via `system_recall`

---

#### system_recall

**Purpose**: Retrieve past decisions, changes, and verified facts from memory. Use **before modifying code** to avoid repeating mistakes or reinventing solutions.

**Triggers**: `mpm recall`, `mpm 历史`, `mpm 召回`

**Parameters**:
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `keywords` | string | Yes | Search keywords (space-separated, fuzzy match) |
| `category` | string | No | Filter by type: "开发"/"重构"/"避坑"/etc. |
| `limit` | integer | No | Max results. Default: 20 |

**Outputs**:
- **Known Facts**: Verified rules/pitfalls with ID and date
- **Memos**: Historical change records with timestamp, category, act, content

**Wide-In Strict-Out Strategy**:
- **Wide-In**: OR match across `Entity` / `Act` / `Content` fields
- **Strict-Out**: Filter by `category` + limit by `limit`
- **Refined Output**: Categorized display (Known Facts first) + timestamp (recent→old)

**Example**:
```
## 📌 Known Facts (2)

- **[pitfall]** Must check dependencies before modifying session logic _(ID: 1, 2026-01-15)_

## 📝 Memos (3)

- **[42] 2026-02-15 14:30** (fix) add idempotency check: prevent duplicate requests...
- **[41] 2026-02-14 10:00** (develop) add timeout parameter: adapt to Alibaba Cloud...
```

**Gotchas**:
- Uses "wide-in, strict-out" strategy: OR match across multiple fields, then filter
- Known Facts displayed first (higher priority)
- Returns "未找到相关记录" if no matches

---

#### known_facts

**Purpose**: Archive verified code rules, iron laws, or important pitfall experiences for later retrieval via `system_recall`.

**Triggers**: `mpm fact`, `mpm 铁律`, `mpm 避坑`

**Parameters**:
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | Fact type: "铁律"/"避坑"/"规范"/"逻辑"/etc. |
| `summarize` | string | Yes | Concise description of the fact |

**Example**:
```javascript
known_facts(type="pitfall", summarize="Must check dependencies before modifying session logic")
```

**Gotchas**:
- Facts are retrievable via `system_recall`
- Keep summaries concise and actionable
- Use consistent type naming for better filtering

---

### 2.5 Enhancement Tools

#### persona

**Purpose**: Switch or manage AI personalities (roles). Changes tone, response style, and thinking protocols for specific scenarios.

**Triggers**: `mpm persona`, `mpm 人格`, `激活人格`, `切换人格`, `列出人格`, `创建人格`, `删除人格`

**Parameters**:
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `mode` | string | No | Mode: "list"/"activate"/"create"/"update"/"delete". Default: "list" |
| `name` | string | Mode-dependent | Persona name or alias |
| `new_name` | string | update mode | New name |
| `display_name` | string | create/update | Display name |
| `hard_directive` | string | create/update | Core directive |
| `aliases` | array | create/update | Alias list |
| `style_must` | array | create/update | Required style elements |
| `style_signature` | array | create/update | Signature expressions |
| `style_taboo` | array | create/update | Forbidden expressions |
| `triggers` | array | create/update | Trigger phrases |

**Modes**:
| Mode | Description |
|------|-------------|
| `list` | List all available personas |
| `activate` | Activate a persona by name or alias |
| `create` | Create new persona (saved to `.mcp-config/personas.json`) |
| `update` | Update existing persona |
| `delete` | Delete a persona |

**Built-in Personas**:
| Name | Display | Style | Use Case |
|------|---------|-------|----------|
| `zhuge` | 孔明 | Classical Chinese | Architecture, code review |
| `trump` | 特朗普 | Confident, superlatives | Brainstorming, breaking deadlock |
| `doraemon` | 哆啦A梦 | Friendly, enthusiastic | Beginner guidance |
| `detective_conan` | 柯南 | Logical deduction | Bug investigation |
| `tangseng` | 唐僧 | Street leader style | Team coordination |
| `tsundere_taiwan_girl` | 小智 | Tsundere, Taiwan accent | Code review |
| `lich_king_arthas` | 阿尔萨斯 | Cold, majestic | Serious debugging |

**Context Dilution Detection**:

Personality expression strength serves as a **signal** for context health:

| Personality Expression | Meaning | Recommendation |
|----------------------|---------|------------|
| Distinct style | Context healthy | Continue current session |
| Blurred expression | Context diluted | New session / compact / input prompt to converge attention |

**Gotchas**:
- Persona is a BUFF mechanism, not persistent config
- Blurred personality expression indicates context dilution
- Custom personas saved to `.mcp-config/personas.json`
- Activation includes hidden system directive for LLM

---

#### open_timeline

**Purpose**: Generate and open an interactive HTML visualization of project evolution history based on `memo` records.

**Triggers**: `mpm timeline`, `mpm 时间线`

**Parameters**: None

**Outputs**:
- Path to generated `project_timeline.html`
- Attempts to open in default browser

**Gotchas**:
- Requires `visualize_history.py` script in project root or `scripts/` directory
- Requires Python installed
- Opens in Microsoft Edge by default on Windows (falls back to default browser)

---

## 3. Best Practices

### 3.0 Rules First (Required)

After initialization, apply `_MPM_PROJECT_RULES.md` to your client system rules before starting any coding task.

**Minimal steps**:

1. Run `initialize_project`
2. Open `_MPM_PROJECT_RULES.md` in project root
3. Paste it into your client's system-rules area

**Common client locations** (labels vary by version):

| Client | Recommended location |
|--------|----------------------|
| Claude Code | System Prompt / Project Instructions |
| OpenCode | System Rules / Workspace Rules |
| Cursor | Rules for AI / Project Rules |

**Recommended first prompt**:

`Read and strictly follow _MPM_PROJECT_RULES.md before executing tasks.`

### 3.1 Standard Workflow

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Locate    │ ──▶ │   Execute   │ ──▶ │   Record    │
│ code_search │     │ Code Change │     │    memo     │
│ code_impact │     │             │     │             │
└─────────────┘     └─────────────┘     └─────────────┘
       │                   ▲                   │
       └───────────────────┴───────────────────┘

[Recommended] When project has > 100 files and user mentions no symbols,
run `project_map` first, then use `flow_trace` to narrow the main chain.
```

### 3.2 Golden Rules

| Rule | Description |
|------|-------------|
| **Locate Before Modify** | `code_search` before changing code |
| **Assess Before Big Change** | `code_impact` to see impact |
| **Record Every Change** | Must call `memo` after modification |
| **Read Log on New Session** | Read `dev-log.md` to restore context |

### 3.3 Standard Code Modification Flow

```
1. code_search(query="target_function")      # Locate
2. code_impact(symbol_name="target_function") # Assess impact
3. (Read code)
4. (Execute modification)
5. memo(items=[{...}])                        # Record
```

### 3.4 Naming Conventions (Vibe Coding)

**Three Rules**:

1. **Symbol Anchoring**: Reject generic words
   - ❌ `data = get_data()`
   - ✅ `verified_payload = auth_service.fetch_verified_payload()`

2. **Prefix as Domain**: Use `domain_action_target`
   - `ui_btn_submit`, `api_req_login`, `db_conn_main`

3. **Searchability First**: Longer names, fewer conflicts
   - `transaction_unique_trace_id` is easier to search than `id`

---

## 4. Performance Comparison

### 4.1 Case 1: Symbol Location

**Task**: Analyze `<symbol>` tool implementation logic

| Metric | Without MPM | With MPM | Improvement |
|--------|-------------|----------|-------------|
| Steps | 12+ | 3 | **300%** |
| Tool calls | 10+ | 2 | **400%** |
| First-step accuracy | 0% | 100% | **∞** |

**Reason**: `code_search` directly returns precise coordinates (file:line), no trial and error.

---

### 4.2 Case 2: Impact Assessment

**Task**: Assess risk of modifying `session.go`

| Dimension | Without MPM | With MPM |
|-----------|-------------|----------|
| Risk perception | Based on local guessing | **AST call chain analysis** |
| Token consumption | Read entire files (4000+) | Map summary (~800) |
| Output | Vague questions | **Precise modification checklist** |

---

### 4.3 Case 3: Project Understanding

**Task**: Cold-start understanding of a 300+ file project

| Metric | Without MPM | With MPM |
|--------|-------------|----------|
| Total time | 40 seconds | **15 seconds** |
| Tool calls | 4+ | 1 |
| Cognitive path | Config→Source→Assembly | **Direct to structured map** |

---

### 4.4 Case 4: Disaster Recovery

**Scenario**: Accidentally ran `git reset --hard`, lost a day of uncommitted code

| Dimension | Git | MPM Database |
|-----------|-----|--------------|
| Record trigger | Explicit commit | **Memo on every change** |
| Coverage | Physical text | **Intent + Semantics** |
| Recovery cost | Manual rewrite | **Guided recovery** |

**Conclusion**: MPM protects the development process, Git protects the code.

---

## 5. FAQ

### Q1: When should I call `initialize_project`?

**Only in these cases**:
- Restarted MCP Server / IDE
- First time using MPM

**Advanced options**:
- `force_full_index=true`: force full indexing (disable bootstrap strategy for large repositories)
- `index_status`: inspect background indexing progress / heartbeat / database file sizes

**If just starting a new conversation**: Just read `dev-log.md`, no need to reinitialize.

---

### Q2: What's the difference between `code_search` and IDE search?

| Dimension | IDE Search | `code_search` |
|-----------|------------|---------------|
| Match method | Text level | **AST symbol level** |
| Same-name ambiguity | Cannot distinguish | **canonical_id precision** |
| Context | Need manual viewing | **Auto-return signature** |

**Recommendation**: Use `code_search` to locate, then IDE to read in detail.

---

### Q3: What is the practical value of DICE complexity?

Note: DICE complexity is an internal heuristic computed by the AST engine.

Practical uses:

1) **Prioritization**
- In `project_map` / `code_impact`, complexity highlights hotspots (what to review/verify first).

2) **Execution strategy**
- Lower score: usually safe to edit directly
- Higher score: run impact analysis first
- Very high score: use `task_chain` with phases and gates

3) **Lower miss risk**
- Higher complexity usually means denser dependencies, so call-chain checks matter more.

Note: the exact scoring formula is an implementation detail and may change across versions. Treat it as a guidance signal, not a hard business rule.

---

### Q4: Where is data stored?

| Data | Location |
|------|----------|
| AST index | `.mpm-data/symbols.db` (SQLite) |
| Memos | `.mpm-data/mcp_memory.db` |
| Human-readable log | `dev-log.md` |
| Project rules | `_MPM_PROJECT_RULES.md` |

**Recommendation**: Add `.mpm-data/` to `.gitignore`. `dev-log.md` is auto-generated and should also be ignored.

**Project Binding**: `initialize_project` creates `.mpm-data/project_config.json` as an anchor. Future sessions auto-bind to this project root. If multiple anchors are found under a workspace aggregator folder, MPM refuses to guess and requires explicit `project_root`.

---

### Q5: Which languages are supported?

| Language | Extensions |
|----------|------------|
| Python | .py |
| Go | .go |
| JavaScript/TypeScript | .js, .ts, .tsx, .mjs |
| Rust | .rs |
| Java | .java |
| C/C++ | .c, .cpp, .h, .hpp |

---

## Trigger Quick Reference

| Category | Triggers | Tool |
|----------|----------|------|
| System | `mpm init` | `initialize_project` |
| System | `mpm index status` | `index_status` |
| Location | `mpm search` `mpm locate` | `code_search` |
| Analysis | `mpm impact` `mpm dependency` | `code_impact` |
| Map | `mpm map` `mpm structure` | `project_map` |
| Flow | `mpm flow` | `flow_trace` |
| Chain | `mpm chain` `mpm taskchain` | `task_chain` |
| Todo | `mpm suspend` `mpm todolist` `mpm release` | `system_hook` |
| Memory | `mpm memo` `mpm recall` `mpm rule` | Memory Series |
| Persona | `mpm persona` | `persona` |
| Visual | `mpm timeline` | `open_timeline` |

---

*MPM Manual v2.2 - 2026-03*
