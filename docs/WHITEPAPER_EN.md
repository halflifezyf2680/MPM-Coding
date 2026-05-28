# MPM-Coding Technical Whitepaper

English | [中文](WHITEPAPER.md)

> This document is intended for technical decision-makers and developers. It describes the engineering architecture, core algorithms, and differentiating design of MPM-Coding.
> For the user manual, see [MANUAL.md](MANUAL.md).

---

## 1. Core Thesis

The competitive focus of AI coding tools is shifting from "model capability" to "context quality."

MPM-Coding does not attempt to make models smarter. All of its engineering effort revolves around a single goal: **use the fewest tokens to let AI precisely understand code structure**. Every tool's output design, every indexing strategy, every degradation path serves this goal.

This is not just a matter of feature lists. The same feature names can have vastly different underlying implementation quality. This document unpacks those differences.

---

## 2. AST Indexing Engine

### 2.1 Architecture: Go Main Process + Rust Indexer

Index construction is performed by a standalone Rust binary, invoked via `os/exec` with JSON communication. The main process has zero CGO dependencies.

- **Rust side**: tree-sitter parsing + rayon parallel processing; all languages share the same parsing pipeline. Extracted symbol types include function, method, class, struct, interface, typedef, constant, macro, variable, namespace
- **Go side**: MCP tool layer + SQLite storage layer + file watcher layer, with clear separation of concerns

Supports 11 languages (tree-sitter bindings): Python, JavaScript, TypeScript, TSX, HTML, CSS, Go, Rust, Java, C, C++.

### 2.2 Two-Phase Indexing Strategy

```
Phase 1 (default): no --extensions flag → adaptive parsing by actual file extension
     ↓ on failure
Phase 2 (fallback): pass --extensions → parse only files matching detected tech stacks
```

Phase 1 lets the Rust side see all files, matching parsers by real extensions, covering all tree-sitter-supported languages. Phase 2 is a safety net -- triggered only when Phase 1 fails, using the tech stack types detected by the Go side to limit parsing scope.

### 2.3 Incremental Indexing: File-Level Precision

`IndexFiles(projectRoot, changedFiles)` writes the list of changed files to a temporary file (`.files` suffix) and passes it to the Rust side for incremental parsing. Temporary files are automatically cleaned up after indexing completes.

This is not "rebuild the index" or "full scan." It only re-parses the few files that actually changed.

### 2.4 Index Freshness: Three-Layer Cache

| Layer | Mechanism | Threshold |
|-------|-----------|-----------|
| Memory | `lastIndexAt` map + mutex | 5 minutes |
| File | `symbols.db` modification time + file size | - |
| Database | `files` table existence + row count check | - |

Freshness is automatically checked before tool calls; incremental indexing is triggered only when expired. This avoids rebuilding on every single call.

### 2.5 Performance

Measured data:

| Project | Language | Files | Symbols | Call Edges | MPM Indexing Time |
|---------|----------|-------|---------|------------|-------------------|
| MPM-Coding | Go+Rust | 86 | - | 4,392 | <1s |
| Kubernetes | Go | 24,877 | 160,899 | 814,615 | ~4min |
| Godot | C/C++ | 13,817 | 158,388 | 355,283 | ~35s |

Rust AST indexer (rayon parallel + tree-sitter): incremental indexing only re-parses changed files. A large Go project with 25,000 files / 160,000+ symbols completes full indexing in ~4 minutes; a C++ project with 13,000+ files finishes in 35 seconds. Note: tree-sitter's C parser is roughly an order of magnitude slower than its Python/Go parsers; Godot still finishes in 35 seconds thanks to rayon parallelism.

---

## 3. Search Engine

### 3.1 code_search: 5-Layer Fallback + Deep Context Reverse Lookup

```
Exact match → Prefix/suffix match → Substring match → Edit distance match → Stem match
     ↓ all failed
Ripgrep text search + GetSymbolAtLine reverse lookup
```

Key design: the ripgrep fallback is not simple text search. Every line match result invokes `GetSymbolAtLine()` to reverse-query the AST database, finding the symbol that the line belongs to. Output format:

```
L42: `someFunction(args)` in `handleRequest` (function)
```

Even when falling back to the text search path, the LLM receives results with semantic context, not bare line numbers.

### 3.2 Search Engine: ripgrep JSON + Pure Go Fallback

- Defaults to `rg --json` for structured output parsing (not text stream parsing)
- When the rg binary is not available, automatically degrades to a pure Go implementation (file traversal + string matching)
- Context line collection: 64-line sliding window, reverse scan reconstruction
- Excludes 20+ noise directories/files by default (`.mpm-data`, `*.lock`, `*.min.js`, `*.map`, etc.)

### 3.3 Candidate List: Always Displayed

Even when an exact match is found, code_search still outputs up to 5 candidates (after deduplication), including symbol type, path, and match score. The LLM can evaluate "the second one might be what I actually need," rather than being forced to accept or reject a single result.

---

## 4. flow_trace: Call Chain Analysis Engine

### 4.1 Dual-Mode Input Recognition

```
Input "handleRequest"              → symbol mode (trace a single symbol)
Input "internal/tools/a.go"         → file mode (trace all functions in the file)
Input "internal/tools/a.go:handle"  → file+symbol hybrid mode
```

The system automatically identifies the input type; no manual mode specification is needed.

### 4.2 File Mode: Smart Entry Filtering

Processing flow in file mode:

1. Extract all callable (function/method) and type (class/struct/interface) symbols from the file
2. Build a flow snapshot (call chain analysis) for each symbol
3. Rank by composite score, truncate for display (brief=1, standard=2, deep=4)

Scoring formula:

```
score = ExternalIn * 50      // cross-file in-degree (highest weight)
      + ExternalOut * 1       // cross-file out-degree
      + BackwardDirect * 8    // direct upstream count
      + BackwardIndirect * 2  // indirect upstream count
      + ForwardDirect * 2     // direct downstream count
      + ForwardIndirect * 1   // indirect downstream count
      + complexity / 8.0      // complexity contribution
```

Functions with more cross-file callers rank highest -- because they are the core interfaces exposed by the file.

### 4.3 Side Effect Detection: Evidence-Based Two-Phase Analysis

Not simple name keyword matching, but evidence collection followed by judgment:

```
Phase 1: collect all called function names from node.Calls and related nodes' Calls
Phase 2: score against 5 side-effect categories (filesystem/database/network/process/state) by API characteristics
Phase 3: Occam's razor -- return empty when no evidence, avoid overgeneralization
```

Each side-effect category has independent evidence rules (e.g., database evidence matches actual API calls like `query`/`begin`/`commit`/`db.exec`), with 10 points per match, and a report threshold of total score >= 10.

Simultaneously detects business phases (init -> validate -> execute -> query -> persist), helping the LLM quickly understand code execution flow.

### 4.4 Output Design: Attention Convergence

The output of flow_trace is a **call chain index**, not source code. Below the header, a boundary is declared:

> The following is a call chain index (upstream/downstream symbol relationships), without implementation details. Read the full context of target files before proposing solutions.

This prevents the LLM from mistaking the call chain output for complete understanding and skipping actual code reading.

---

## 5. code_impact: Impact Analysis

### 5.1 Two-Phase Analysis: BFS Chain Discovery + Dice Random Walk Scoring

**Phase 1 -- BFS Chain Discovery**: Starting from the target symbol, perform BFS along the call graph (forward adjacency or reverse reverse_adjacency) with a depth limit of 3. Discover direct callers and indirect callers, and determine risk level (affected node count: 0->low, <=3->low, <=10->medium, >10->high).

**Phase 2 -- Dice Random Walk Complexity Scoring**: From the target symbol, execute 1000 random walks on the directed graph (10 steps each, damping 0.85). Count the nodes covered by walks (coverage), and calculate complexity combining fan-out and fan-in:

```
complexity = coverage * 0.5 + fanOut * 2.0 + fanIn * 1.0
```

Normalized to 0-100, graded: Simple(<20) / Medium(<50) / High(<80) / Extreme(>=80).

### 5.2 Structured Markdown Output

Each impact analysis outputs structured Markdown: risk level, complexity score, affected node count, direct caller list (top 10, with file path and line number), indirect caller list (top 20, with file path and line number, sorted by BFS distance). Consistent with other tool outputs, directly consumable by the LLM.

---

## 6. known_facts: Experience Strategy Engine

### 6.1 Multi-Dimensional Weighted Scoring

When `before_action` mode recalls relevant facts, each KnownFact is scored and ranked by the following formula:

```
score = contextMatch * 0.45    // context keyword match degree
      + confidence * 0.25      // fact confidence
      + successRate * 0.20     // historical adoption success rate
      + exploration * 0.10     // cold-start exploration bonus
      - failurePenalty         // failure penalty
```

The `exploration` term is calculated using `sqrt(log(totalHits+1)+1) / (hitCount+1)` -- encouraging high-quality obscure facts with low exposure but high global hit counts to surface.

### 6.2 Bayesian-Style Confidence Evolution

`after_action` updates fact confidence based on results:

| Scenario | Formula | Effect |
|----------|---------|--------|
| Adopted + Succeeded | `confidence += 0.10 * (1 - confidence)` | Exponential convergence toward 1.0, diminishing returns |
| Adopted + Failed | `confidence -= 0.20 * confidence` | Rapid penalty, proportional shrinkage |
| Not Adopted | `confidence -= 0.02 * confidence` | Mild decay, no false kills |

Automatic state promotion: `confidence >= 0.75 && support >= 3` -> active; `confidence < 0.25 && reject >= 2` -> rejected.

### 6.3 Event Audit Trail

Every fact interaction (exposure/adopt/reject/add) records a `FactEvent`, including eventType, task_id, phase, context_signature, and payload_json. This forms a complete fact lifecycle audit chain.

### 6.4 Auto-Persistence + Threshold Governance

`after_action` automatically writes new observations to `.claude/CLAUDE.md` and the project-level `AGENTS.md`:

- Uses marked sections (`## MPM Known Facts` ... `<!-- MPM_KNOWN_FACTS_END -->`)
- Checks for content existence before writing (deduplication)
- Atomic write via `.tmp` + `os.Rename`
- Dead zone threshold signals: >50 entries suggest deduplication, >70 entries suggest compression (only triggered when crossing upward, preventing oscillation)

---

## 7. task_chain: State Machine-Driven Task System

### 7.1 Auto-Progression Design

```
init      → auto-start first phase
spawn     → auto-start first sub-task
complete_sub → auto-start next pending sub-task
```

The AI only needs to call complete; the system automatically advances to the next step. This reduces the number of calls and lowers the probability of errors.

### 7.2 Gate Failure: Route Fallback, Not Simple Retry

```
gate failed → RetryCount++
           → exceeded max_retries? → task marked as failed
           → not exceeded?
              → has on_fail? → jump to target phase, reset target phase state, clear summary
              → no on_fail? → retry current gate
```

The on_fail route is not "re-run the gate," but "go back to a previous phase and re-execute." The fallback scope is determined by the phases definition at init time.

### 7.3 Re-Init Self-Check Guard

When the same task_id is init'd more than once, the system directly returns an error, forcing the AI to stop and explain to the user. This prevents the AI from repeatedly re-initing in an infinite loop.

### 7.4 Cross-Session Resume

Task state is persisted to SQLite. After a session interruption, `resume` restores the checkpoint from the database, including phase state, sub-task progress, and summaries of completed phases.

---

## 8. File Watcher: Lazy Update Mode

```
File change → watcher detects (2000ms debounce) → marks staleFiles
                                                  ↓
Tool call → ensureFresh() → extract staleFiles → IndexFiles() incremental indexing
```

This is not "rebuild index as soon as a file changes," but "mark dirty, wash when needed." This avoids frequent indexing caused by rapid consecutive saves (IDE auto-save).

---

## 9. Data Layer

### 9.1 SQLite Configuration

```sql
PRAGMA journal_mode = WAL;       -- high-concurrency read/write
PRAGMA synchronous = NORMAL;     -- balance between performance and safety
PRAGMA busy_timeout = 30000;     -- 30-second busy wait
```

Go connection pool: `SetMaxOpenConns(1)` + `SetMaxIdleConns(1)` -- single connection reduces in-process lock contention.

### 9.2 Busy Wait Retry

```go
// with_sqlite_busy_retry: 6 retries, linear backoff (200ms * attempt)
for attempt := 0; attempt < 6; attempt++ {
    err = fn()
    if err == nil { return nil }
    if !isBusy(err) { return err }
    time.Sleep(200 * time.Millisecond * time.Duration(attempt+1))
}
```

### 9.3 Schema Self-Healing

`healSchema()` automatically executes every time the database is opened:
- Check if tables exist; create if missing
- Check if columns exist (`PRAGMA table_info`); `ALTER TABLE ADD COLUMN` if missing
- Detect duplicate columns (may arise from cross-version upgrades); skip
- Create missing indexes

### 9.4 Memo Disaster Recovery

```
DB init → check if memos table is empty
  → empty? try to recover from memo_archive.jsonl (JSONL format)
  → JSONL also missing? recover by regex parsing from dev-log.md
```

Three-layer recovery strategy ensures that even if the SQLite file is corrupted, historical records are not completely lost. All new memos are asynchronously appended to the JSONL archive.

---

## 10. Path Safety

### 10.1 Project Root Detection: 4-Level Strategy

| Priority | Strategy | Mechanism |
|----------|----------|-----------|
| 1 | Anchor upward search | Look for `.mpm-data/project_config.json` |
| 2 | Bounded BFS downward | Depth <= 3, max 200 directories |
| 3 | Environment variables | 5 keys including `MPM_PROJECT_ROOT`, `WORKSPACE_FOLDER` |
| 4 | CWD fallback | Current directory + 10 project marker file checks |

### 10.2 Blacklist Filtering

Blocks initialization of the following directories:
- System directories: `windows`, `system32`, `program files`
- IDE runtime directories: `vscode`, `cursor`, `claude`, `windsurf`, etc.
- Sensitive directories: `appdata`, `temp`, `prefetch`
- Volume roots (e.g., `D:\`)

Sole exception: if `.git` exists within the directory, it is treated as a genuine project root.

### 10.3 Parameter Path Normalization

All scope/file_path parameters go through 5-step processing before being passed in:
1. Reject absolute paths (including Windows drive letters)
2. Reject `../` path traversal
3. `\` -> `/` unification
4. `path.Clean()` cleanup
5. Strip `./` prefix

### 10.4 Legacy Auto-Migration

When a `.mcp-data` directory is detected, automatically migrates to `.mpm-data`, preferring rename (fast), with copy+fallback for Windows cross-volume scenarios, leaving a `MIGRATED.txt` marker after migration.

---

## 11. project_map: Adaptive Output

### 11.1 Directory Tree Expansion Strategy

Automatically adjusts expansion depth based on the number of top-level directories:

| Directory Count | Strategy |
|----------------|----------|
| <= 20 | Expand all to L3 |
| 20-40 | First 8 expanded to L3, first 18 expanded to L2 |
| > 40 | First 6 expanded to L3, first 12 expanded to L2 |

### 11.2 Symbol View: Three-Level Folding

| File Number | Display Method |
|-------------|----------------|
| 1-10 | Detailed display for each symbol (name, line number, complexity marker) |
| 11-30 | Display only filename and symbol count |
| > 30 | Folded into omission notice |

Each symbol includes a `[HIGH:xx.x]` / `[MED:xx.x]` / `[LOW:xx.x]` complexity marker, sorted by complexity in descending order.

### 11.3 Large Output Auto-Save

When output exceeds 2000 characters, it is automatically saved to `.mpm-data/project_map_*.md`, and the file path is returned. This avoids injecting large amounts of text into the context window.

---

## 12. Tech Stack Detection

Recursively scans project files (max depth 8 levels), detecting 6 tech stacks, each with dedicated ignore directories:

| Tech Stack | Extensions | Dedicated Ignores |
|------------|------------|-------------------|
| Python | .py | site-packages, htmlcov, .pytest_cache |
| Frontend | .js/.jsx/.ts/.tsx/.html/.css | coverage, .next, .nuxt |
| Go | .go | vendor, bin |
| Rust | .rs | target |
| C/C++ | .c/.h/.cpp/.hpp/.cc | cmake-build-debug |
| Java | .java | .gradle |

Integrates `.gitignore` parsing to automatically exclude user-defined ignore directories.

---

## 13. Design Philosophy Summary

MPM's engineering decisions follow three principles:

**1. Output Is Context Cleansing**

Every tool's output is not a report for humans; it is a precise injection for the LLM. code_search returns symbol locations, not grep results. code_impact returns a call chain panorama, not a file list. flow_trace returns a main chain index, not source code. The output itself is a filter against context noise.

**2. Degradation Must Not Reduce Quality**

When ripgrep is unavailable, it degrades to pure Go search, but still performs GetSymbolAtLine reverse lookup. When AST matching fails, it degrades to text search, but still annotates the owning symbol. Every degradation path maintains semantic context and never devolves to bare text.

**3. Metadata First, Source Code On Demand**

Tools return locations, signatures, call relationships, and complexity scores -- all metadata. After receiving metadata, the LLM autonomously decides whether it needs to Read source files. Source code is never proactively stuffed into the context window, because every additional page of source code means more token cost and more attention fragmentation.

---

*MPM-Coding Whitepaper v1.0 — 2026-05*
