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

```
Locate ──▶ Analyze ──▶ Execute ──▶ Record
code_search  code_impact  task_chain  memo
```
- **Short tasks**: `code_search -> code_impact -> edit -> memo`
- **Long tasks**: add `task_chain` in the middle; use `system_hook` when blocked

### 1.1 AST Indexing

`initialize_project` triggers background index build. The process:

1. Tree-sitter (Rust-side, rayon parallelism) parses source code, extracting symbols such as functions/classes/methods/type definitions/constants/macros/namespaces
2. Generates `canonical_id` (globally unique) for each symbol, with type-prefixed IDs: `func:`, `class:`, `typedef:`, `macro:`, `const:`, `namespace:`
3. Builds call edges (caller -> callee), forming a call graph
4. Writes to `symbols.db` (SQLite)

The indexing strategy uses a two-phase design. Phase 1 places no restrictions on file types; the Rust side adaptively parses all tree-sitter-supported languages based on actual file extensions. Only when Phase 1 fails does it fall back to Phase 2 (parsing only files corresponding to the tech stack detected by the Go side).

After file changes, there is no need to manually rebuild the index. MPM starts file monitoring (2000ms debounce) upon initialization. Changed files are marked as stale, and the next tool call automatically triggers incremental indexing -- only the modified files are re-parsed, not a full scan.

`code_search` queries this database. `code_impact` performs BFS link discovery + Dice Random Walk complexity scoring on the call graph.

Supported languages: Go, Rust, Python, TS/JS, Java, C/C++, HTML, CSS.

### 1.2 Context Cleaning & Attention Convergence

The core bottleneck in AI coding isn't model capability -- it's **too much garbage in the context window**.

In a 5,000-file project, if the AI relies on reading files to understand code, it either reads everything (token explosion) or guesses which files to read (missing critical dependencies). Both approaches are disastrous.

MPM tools don't do IDE things -- completions, go-to-definition, rename. AI clients already handle those. MPM solves a different problem: **how to make the AI understand code structure with minimal tokens**.

- `code_search` returns the exact location of a symbol definition -- not a pile of grep results
- `code_impact` returns the full call chain panorama -- not making the AI guess file by file who calls it
- `flow_trace` returns the main business logic chain -- not a directory listing
- `project_map` returns a structured symbol inventory -- not `ls` output

The **output** of these tools constitutes context cleaning -- injecting only deterministic structural information, filtering out noise. The AI no longer needs to blindly search through oceans of code. Tool outputs have already focused its attention on the few symbols and relationships that matter.

This is **attention convergence**: from "guessing files" to "querying symbols", from "reading entire files" to "following call chains". What's truly valuable is the effect these results produce once injected into the AI's context.

[View interactive architecture diagram](https://halflifezyf2680.github.io/MPM-Coding/architecture.html)

---

## 2. Tool Reference

### 2.0 Overview

| # | Tool | Category | One-liner |
|---|------|----------|-----------|
| 1 | `initialize_project` | System | Initialize project environment, AST index, and file monitoring |
| 2 | `index_status` | System | Check index build progress |
| 3 | `project_map` | Navigation | Adaptive project structure + symbol inventory + complexity markers |
| 4 | `flow_trace` | Navigation | Call chain tracing + side effect detection + stage analysis |
| 5 | `code_search` | Navigation | 5-layer degradation symbol lookup + ripgrep deep context reverse lookup |
| 6 | `code_impact` | Safety | BFS chain discovery + Dice Random Walk complexity scoring |
| 7 | `task_chain` | Execution | State-machine task chain (gates + auto-advancement + resume) |
| 8 | `system_hook` | Execution | Block suspend / resume |
| 9 | `memo` | Memory | Change record (SSOT + disaster recovery) |
| 10 | `system_recall` | Memory | Search history (Memo + Known Facts) |
| 11 | `known_facts` | Memory | Experience strategy engine (multi-dimensional scoring + confidence evolution + event audit) |
| 12 | `persona` | Enhancement | AI personality switching |
| 13 | `open_timeline` | Enhancement | Project evolution timeline visualization |
| 14 | `ensure_languages` | System | Scan project file extensions, download missing tree-sitter grammars |

---

### 2.1 initialize_project

Initialize project environment. **Prerequisite for all other operations.**

**Parameters**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `project_root` | string | No | Absolute path to project root. Auto-detected if empty. |
| `force_full_index` | boolean | No | Force full indexing. Default: false. |

**Full behavior**:

1. **Path safety validation**: Filters dangerous paths (`.`, `..`, `/`, volume roots, etc.), blocks system directories, IDE runtime directories, and sensitive directories from initialization. Only exception: directory contains `.git`
2. **Legacy migration**: When `.mcp-data` (old format) is detected, automatically migrates to `.mpm-data` (rename or copy+fallback), leaving `MIGRATED.txt` after migration
3. **Tech stack detection**: Recursively scans file extensions (max depth 8 levels), detects 6 tech stacks (Python / Frontend / Go / Rust / C++ / Java), integrates `.gitignore` parsing to ignore directories
4. **Create data directory**: `.mpm-data/` (SQLite databases + index files)
5. **Initialize memory layer**: `mcp_memory.db` (Memo / Known Facts / Task / Hook)
6. **Start AST index build in background**: Two-phase strategy (full index first -> whitelist fallback), executes asynchronously without blocking
7. **Start file monitoring**: 2000ms debounce, changes marked as stale, triggers incremental indexing on next tool call
8. **Generate project rules**: `_MPM_PROJECT_RULES.md` (MPM protocol rules template), refreshed in the background after indexing completes

**Project root detection** (auto-executed when left empty, 4-level strategy):

| Priority | Strategy |
|----------|----------|
| 1 | Search upward for `.mpm-data/project_config.json` anchor |
| 2 | Bounded BFS downward search (depth <= 3, max 200 directories) |
| 3 | Environment variables `MPM_PROJECT_ROOT`, `WORKSPACE_FOLDER`, etc. |
| 4 | CWD fallback + project marker file check |

---

### 2.2 index_status

Check AST index build progress.

**Parameters**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `project_root` | string | No | Uses current session project if empty. |

**Returns**:
- `index_status`: status (running/success/failed), mode (auto/full), started_at, finished_at, total_files, elapsed_ms
- `heartbeat`: processed / total progress
- `db_file_sizes`: symbols.db / symbols.db-wal / symbols.db-shm file sizes

---

### 2.3 project_map

Project structure navigation. Two view modes, adaptive output.

**Parameters**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `level` | string | No | `structure` (directory tree) / `symbols` (symbol inventory + complexity). Default: symbols |
| `scope` | string | No | Directory scope (relative path). Empty = entire project. |

**structure view**: Directories sorted by file count, adaptive expansion depth:

| Directory count | Expansion strategy |
|-----------------|---------------------|
| <= 20 | Expand all to L3 |
| 20-40 | First 8 expand to L3, first 18 expand to L2 |
| > 40 | First 6 expand to L3, first 12 expand to L2 |

**symbols view**: Symbols displayed with complexity tier markers, 3-tier folding:

| File sequence | Display mode |
|---------------|--------------|
| 1-10 | Full symbol details (name, line number, complexity `[HIGH:xx.x]`/`[MED:xx.x]`/`[LOW:xx.x]`) |
| 11-30 | File name + symbol count only |
| > 30 | Collapsed into omission hint |

Complexity scoring is based on Fan-in/Fan-out model: `score = maxFanOut * 1.0 + maxFanIn * 0.5`. FanOut > 10 marked as "High Coupling", FanIn > 20 marked as "Core Module".

**Large output auto-save**: When output exceeds 2000 characters, it is automatically saved to `.mpm-data/project_map_symbols.md` or `.mpm-data/project_map_structure.md`, returning the file path. This prevents massive text from being injected into the context window.

**Example output**:
```
66 files | 168 symbols
Complexity: High: 6 | Med: 18 | Low: 144

internal/tools/ (14 files)
  search_tools.go
    f wrapSearch L47 [HIGH:83.5]
    SearchArgs L15
```

---

### 2.4 code_search

AST-level symbol lookup. Use when you know the name but can't find the location.

**Parameters**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `query` | string | Yes | Symbol name (not natural language) |
| `scope` | string | No | Directory scope (relative path) |
| `search_type` | string | No | `any` / `function` / `class`. Default: any. `class` also matches struct/interface/typedef/namespace |

**Search strategy -- 5-layer degradation + ripgrep deep context reverse lookup**:

```
AST exact match -> prefix/suffix match -> substring match -> edit distance match -> stem match
     | all failed
Ripgrep text search + GetSymbolAtLine reverse lookup
```

Key design: the ripgrep fallback is not a simple text search. Each matched line calls `GetSymbolAtLine()` to reverse-lookup the AST database, finding the symbol that line belongs to. The output format is `L42: \`someFunction(args)\` in \`handleRequest\` (function)`. Even when taking the text search path, the LLM receives results with semantic context -- not raw line numbers.

`search_type` filtering is effective at both the AST layer and text layer: `function` matches function/method, `class` matches class/struct/interface/component/template/typedef/namespace, etc.

**Candidate list**: Even when an exact match is found, up to 5 candidates are output (after deduplication), including symbol type, file path, and match score. The LLM can discover "the second one might be what I actually need."

**Output**: Best match's canonical_id, file location, signature, call relations (functions called by this symbol + locations that call this symbol) + candidate list + ripgrep text search results (with semantic context annotations).

---

### 2.5 code_impact

Call chain impact analysis. Use before modifying code.

**Parameters**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `symbol_name` | string | Yes | Exact symbol name (not natural language search) |
| `direction` | string | No | `backward` (who calls me) / `forward` (what I call) / `both`. Default: backward |

**Risk scoring**: Two-phase analysis. First, BFS (depth 3) discovers direct and indirect callers along the call graph, determining the risk level (by number of affected nodes). Then, the Dice algorithm (Random Walk, 1000 walks x 10 steps x 0.85 damping) calculates a complexity score using the formula `coverage * 0.5 + fanOut * 2.0 + fanIn * 1.0`, normalized to 0-100. Complexity tiers: Simple(<20) / Medium(<50) / High(<80) / Extreme. When no direct callers exist, outputs "safe to modify."

**Output**: Risk level, complexity score, affected node count, direct caller list (top 10, with file path and line number), indirect caller list (top 20, with file path and line number, sorted by BFS distance). When no direct callers exist, outputs "safe to modify."

---

### 2.6 flow_trace

Call chain tracing engine. Establishes code reading paths.

**Parameters**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `symbol_name` | string | Yes | Function name, class name, or file path. Auto-detected by the system. Supports: `handleRequest`, `internal/tools/a.go`, `internal/tools/a.go:handleRequest` |
| `scope` | string | No | Directory scope (relative path) |
| `direction` | string | No | `backward` / `forward` / `both`. Default: both |
| `mode` | string | No | `brief` / `standard` / `deep`. Default: brief |
| `max_nodes` | int | No | Max nodes. Default: 40, upper limit: 120 |

**Dual-mode entry recognition**:

| Input | Mode | Behavior |
|-------|------|----------|
| `handleRequest` | symbol mode | Traces upstream and downstream of a single symbol |
| `internal/tools/a.go` | file mode | Extracts all symbols in the file, sorts by score, displays top entry points |
| `internal/tools/a.go:handle` | mixed mode | Finds the specified symbol within the specified file |

**File mode smart filtering**: Extracts all callable and type symbols within the file, builds a call chain snapshot for each, and sorts by composite score. The scoring formula weights cross-file in-degree highest (x50), meaning core externally-exposed interfaces rank first. Truncation by mode: brief=1, standard=2, deep=4.

**Side effect detection**: Evidence-based two-phase analysis. Collects called function names from actual call chains, then scores and determines 5 categories of side effects:

| Side effect type | Evidence examples |
|------------------|-------------------|
| filesystem | readFile, writeFile, os.Open, mkdir, remove |
| database | query, begin, commit, db.Exec, stmt.Query |
| network | listen, dial, http.Get, grpc.Dial |
| process | exec.Command, StartProcess, spawn |
| state | Lock, Mutex, cache.Set, session.Save |

Occam's razor principle: no evidence, no report. Avoids over-generalization.

**Stage detection**: Automatically identifies init -> validate -> execute -> query -> persist business stages.

**Output boundary declaration**: Below the title, annotates `> The following is a call chain index (upstream/downstream symbol relationships) and does not include implementation details. Read the full context of target files before proposing solutions.` This prevents the LLM from mistaking the call chain for complete understanding.

**Mode differences**:

| Information | brief | standard | deep |
|-------------|-------|----------|------|
| Entry location/type | Yes | Yes | Yes |
| Cross-file connection count | Yes | Yes | Yes |
| Upstream/downstream impact count | Yes | Yes | Yes |
| Upstream key nodes | No | Yes | Yes |
| Critical path Top 3 | No | Yes | Yes |
| Stage summary | No | Yes | Yes |
| Side effect annotations | No | Yes | Yes |
| Candidate entry count | 1 | 2 | 4 |

---

### 2.7 task_chain

**State-machine-driven self-iterating task system.** Not a simple step list -- init defines phases and gates, AI drives itself forward, gate failures auto-rollback and retry, humans only inspect at key checkpoints.

**Core mechanisms**:

- **Declarative phases** -- init defines phase goals and acceptance criteria; AI has full execution freedom within each phase
- **Gate checkpoints** -- gate phases require result="pass" to proceed; on fail, checks on_fail routing: if configured, jumps to the specified phase and resets its state (not simply retrying the current gate, but going back to a previous phase to re-execute); if unconfigured, retries the current gate. Exceeding max_retries (default 3) marks the task as failed
- **Loop phases** -- spawn subtasks in batch, complete_sub one by one, auto-passed when all complete
- **Auto-advancement** -- init auto-starts the first phase, spawn auto-starts the first subtask, complete_sub auto-starts the next pending subtask. AI only needs to call complete, the system auto-advances to the next step
- **Runtime adjustment via update** -- Goal changed? update modifies description or replaces unfinished phases; completed phases are always preserved
- **Cross-session resume** -- resume restores from SQLite (phase state, subtask progress, completed summaries). Session breaks don't lose progress
- **Re-init guard** -- When the same task_id is init-ed more than once, an error is thrown immediately, forcing the AI to stop and explain to the user. Prevents infinite loops

**mode parameters**:

| mode | Description | Required params |
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
| `develop` | implement -> verify_gate -> finalize | Feature development |
| `debug` | fix -> verify_gate -> finalize | Bug fixes |
| `refactor` | refactor -> verify_gate -> finalize | Refactoring |

Gate phases require `result="pass"` or `result="fail"`. Fail triggers on_fail routing or retry.

**Example**:
```javascript
// Initialize (auto-starts first phase implement)
task_chain(mode="init", task_id="AUTH", protocol="develop", description="Add OAuth2")

// Complete implementation
task_chain(mode="complete", task_id="AUTH", phase_id="implement", summary="Flow implemented")

// Pass gate
task_chain(mode="complete", task_id="AUTH", phase_id="verify_gate", result="pass", summary="Tests pass")

// Finalize
task_chain(mode="complete", task_id="AUTH", phase_id="finalize", summary="Done")
```

**Real-world scenario**: Plan the full task chain upfront, then let a capable model run on its own. Start a `develop` protocol chain before bed -- from implementation through verification to finalization, each phase has a gate that blocks on failure, auto-retries or rolls back, and self-corrects when drifting off track. Next morning, check results with `task_chain(mode="status")`. Session broke due to network? No problem -- `resume` picks up from the checkpoint, no progress lost. This is task_chain's true value: **let AI independently complete multi-step tasks, humans only inspect results**.

---

### 2.8 system_hook

Suspend/resume mechanism for blocked tasks.

**mode parameters**:

| mode | Description | Required params |
|------|-------------|-----------------|
| `create` | Create hook | `description`, `priority` |
| `list` | List hooks | `status` (open/closed) |
| `release` | Release hook | `hook_id`, `result_summary` |

**Parameters**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `description` | string | Required for create | Reason for suspension |
| `priority` | string | No | `high` / `medium` / `low`. Default: medium |
| `tag` | string | No | Category tag |
| `expires_in_hours` | int | No | Expiration time (hours). Expired hooks show EXPIRED marker in list |
| `hook_id` | string | Required for release | Hook number (e.g. #001) |
| `result_summary` | string | Optional for release | Completion summary |

**Real-world scenario**: While discussing a problem with AI, you spot 5 things that need handling but can't act on right now. `create` a hook for each one and keep the conversation going. Keep finding new issues? Keep `create`-ing hooks. When the discussion wraps up, batch `release` them one by one. Nothing to hold in your head, nothing to forget. **system_hook is the ultimate decision-making tool** -- it turns "discuss and forget" into "discuss, record, and batch-process."

---

### 2.9 memo

Change record (SSOT). Must call after every code modification.

**Parameters**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `items` | array | Yes | Array of records, each with `category`, `entity`, `act`, `path`, `content` |
| `lang` | string | No | `zh` / `en`. Default: zh |

**category**: `modification` / `development` / `decision` / `refactoring` / `pitfall`

**Example**:
```javascript
memo(items=[{
  category: "modification",
  entity: "GetSession",
  act: "add idempotency check",
  path: "core/session.go",
  content: "prevent duplicate requests from creating multiple sessions"
}])
```

**Storage mechanism**:
- Written to SQLite (`mcp_memory.db`)
- Async refresh to Markdown (`dev-log.md`, top 100 entries, newest first)
- Async append to JSONL archive (`dev-log-archive/memo_archive.jsonl`)

**Disaster recovery**: On initialization, checks whether the DB memos table is empty. If empty, attempts recovery from the JSONL archive; if JSONL is also empty, recovers from `dev-log.md` via regex parsing. This three-layer strategy ensures that even if the SQLite file is corrupted, historical records are not completely lost.

---

### 2.10 system_recall

Search history. Check before modifying code to avoid repeating past mistakes.

**Parameters**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `keywords` | string | Yes | Space-separated keywords, supports fuzzy matching |
| `category` | string | No | Type filter (e.g. "pitfall", "development", "decision") |
| `limit` | int | No | Max results. Default: 20 |

**Search scope**: Memo (change records) + Known Facts (experience strategies). Known Facts shown first.

---

### 2.11 known_facts

Experience strategy engine. Recall experience before action, write back results and evolve confidence after action.

Legacy `type + summarize` saves are still supported; the new main path is the `before_action -> execute -> after_action` loop.

**Parameters**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `mode` | string | No | `before_action` / `after_action` / `maintain` / `status`; empty keeps legacy save behavior |
| `type` | string | No | Fact type: `pitfall` / `success_pattern` / `rule` / `constraint`; used by legacy saves |
| `summarize` | string | No | Fact description; used by legacy saves or `mode=add` |
| `context` | object | No | Current task context: task, task_id, intent, phase, risk, files, symbols, tools |
| `outcome` | object | No | Used by `after_action`: result, signal, summary, adopted_facts, rejected_facts, new_observations |
| `limit` | int | No | Return limit. Default: 3 |

**before_action (pre-action recall)**:

Based on current context (task/intent/phase/files/symbols/tools), recalls relevant Known Facts from the fact database, sorted by multi-dimensional weighted scoring:

```
score = contextMatch * 0.45    // Context keyword match degree (highest weight)
      + confidence * 0.25      // Fact confidence
      + successRate * 0.20     // Historical adoption success rate
      + exploration * 0.10     // Cold discovery bonus (under-exposed but globally frequently matched facts surface first)
      - failurePenalty         // Failure penalty
```

Also returns strategy suggestions (dynamically generated based on intent and risk level).

**after_action (post-action writeback)**:

Updates fact confidence based on execution results -- Bayesian-style evolution:

| Scenario | Formula | Effect |
|----------|---------|--------|
| Adopted + success | `confidence += 0.10 * (1 - confidence)` | Exponential approach to 1.0; harder to increase as it gets higher |
| Adopted + failure | `confidence -= 0.20 * confidence` | Rapid proportional penalty |
| Not adopted | `confidence -= 0.02 * confidence` | Weak decay; avoids false kills |

Auto-promotion: `confidence >= 0.75 && support >= 3` -> active; `confidence < 0.25 && reject >= 2` -> rejected.

**Auto-persistence**: after_action writes new observations to `.claude/CLAUDE.md` and project-level `AGENTS.md`:
- Uses marker regions for locating (`## MPM Known Facts` ... `<!-- MPM_KNOWN_FACTS_END -->`)
- Checks for duplicate content before writing (deduplication)
- `.tmp` + `os.Rename` atomic write
- Threshold governance: >50 entries suggests deduplication, >70 entries suggests compaction (dead zone design, only triggers on upward crossing)

**Event audit**: Every fact interaction (exposure/adopt/reject/add) records a `FactEvent`, including eventType, task_id, phase, context_signature, and payload_json. A complete fact lifecycle audit chain.

**`new_observations` writing guidelines**: Only write reusable experience (format: "When X, should/should not Y"). Do not write task completion confirmations (e.g. "Successfully fixed...") or action logs.

**Common calls**:

- `mode=before_action`: Returns Relevant Known Facts and Strategy based on current context.
- `mode=after_action`: Strengthens/weakens facts based on results, and writes new observations to candidates.
- `mode=status`: Shows fact state (confidence, adoption/rejection counts, hit counts).
- `mode=maintain`: Shows convergence maintenance guidance (counts by state).

`system_recall` remains a history search tool and does not own KnownFact strategy evolution.

---

### 2.12 persona

AI personality management.

**Modes**: `list` / `activate` / `create` / `update` / `delete`

Optional fields for create/update: `name`, `display_name`, `hard_directive`, `aliases`, `style_must`, `style_signature`, `style_taboo`, `triggers`

Personality data saved to `.mcp-config/personas.json`.

---

### 2.13 open_timeline

Generates project evolution timeline (HTML) based on memo records. Auto-opens in browser.

No parameters.

---

### 2.14 ensure_languages

Scans project file extensions and triggers tree-sitter grammar downloads. Usually executed automatically during `initialize_project`; no manual invocation needed.

**Parameters**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `project_root` | string | No | Absolute path to project root. Uses current session project if empty. |

**Behavior**: Recursively scans the project directory, collects all file extensions, detects corresponding languages via `tree-sitter-language-pack`, and triggers grammar compilation/download. Languages already present are skipped.

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
code_search(query="target symbol")          -> locate (AST 5-layer degradation + ripgrep reverse lookup)
code_impact(symbol_name="target symbol")    -> assess impact (call chain BFS + risk scoring)
(read code, make changes)
memo(items=[{...}])                         -> record why (SSOT + disaster recovery)
```

### 3.3 Understanding business logic

```
flow_trace(symbol_name="entry function", mode="standard")  -> call chain + side effects + stages
or
flow_trace(symbol_name="internal/service/user.go")          -> file mode, auto-filter high-entry symbols
```

### 3.4 Long tasks

```
task_chain(mode="init", protocol="develop")     -> initialize (auto-starts first phase)
(do the phase work)
task_chain(mode="complete", phase_id="1", ...)  -> complete phase (auto-advances to next)
(repeat until all phases done)
task_chain(mode="finish")                       -> close
```

### 3.5 Experience accumulation

```
// Before action: recall relevant experience
known_facts(mode="before_action", context={task="...", intent="DEVELOP", ...})

// After action: write back results
known_facts(mode="after_action", outcome={
  result: "success",
  adopted_facts: [12],
  new_observations: ["When X, should Y"]
})
```

### 3.6 Blocked

```
system_hook(mode="create", description="waiting for API key", priority="high")  -> suspend
(user provides the key)
system_hook(mode="release", hook_id="#001", result_summary="configured")       -> resume
```

---

## 4. FAQ

### Which languages are supported?

Go, Rust, Python, TypeScript/JavaScript, Java, C/C++, HTML (structural symbols), CSS (structural symbols)

11 tree-sitter bindings total. The first indexing phase places no restrictions on file types; all tree-sitter-parseable languages are indexed.

Extracted symbol types: function, method, class, struct, interface, component, typedef, constant, macro, variable, namespace, selector, keyframes, layout, template, slot.

### Where is data stored?

| Data | Location |
|------|----------|
| AST index | `.mpm-data/symbols.db` |
| Memo/Task/Hook/KnownFacts | `.mpm-data/mcp_memory.db` |
| Human-readable log | `.mpm-data/dev-log.md` |
| JSONL archive | `.mpm-data/dev-log-archive/memo_archive.jsonl` |
| Project anchor | `.mpm-data/project_config.json` |
| Project rules | `_MPM_PROJECT_RULES.md` |
| Large output cache | `.mpm-data/project_map_*.md` |

`.mpm-data/` is never committed to git.

### How to switch between projects?

Each project has its own `.mpm-data/`. When multiple projects exist under a workspace, `initialize_project` requires explicit `project_root`.

### Can I use tools before indexing finishes?

`code_search` and related tools depend on the index. For large repos, use `index_status` to check progress. Results may be incomplete before indexing finishes.

Indexing uses a three-layer freshness cache (in-memory 5-minute threshold -> file modification time -> database row count check). Tools automatically check before each call and only trigger incremental indexing when stale.

### Do I need to reinitialize for a new session?

No. As long as the MCP Server hasn't restarted and `.mpm-data/` exists, just continue directly. For new sessions, it's recommended to run `system_recall` first to restore context.

### Does the index auto-update?

Yes. After initialization, file monitoring starts (2000ms debounce). File changes are automatically marked as stale, and incremental indexing is triggered on the next tool call (only modified files are re-parsed).

---

*MPM Manual v3.0 -- 2026-05*
