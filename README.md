# MPM-Coding MCP

> **Turning AI Coding from "Demos" into "Delivery"**

English | [中文](README_ZH.md)

![License](https://img.shields.io/badge/license-MIT-blue.svg) ![Go](https://img.shields.io/badge/Go-1.21+-00ADD8.svg) ![MCP](https://img.shields.io/badge/MCP-v1.0-FF4F5E.svg)

---

## AI Coding That Survives Reality

AI coding is fun until you try it on a real repo:

- The model forgets context ("where is the code?")
- It edits based on guesses ("this should be fine")
- Long tasks drift, skip steps, or die halfway
- Later you cannot answer "what changed and why?"

MPM is not trying to make the LLM "smarter" or "better at chatting". That is the model's job.
MPM makes the work *finishable*: locate first (`code_search`), check impact (`code_impact`), run long tasks as a phased chain with gates (`task_chain`), then store the why (`memo`).

In AI coding, "smart" usually means "steady": solves real problems, leaves a trail, can resume, with fewer guesses and fewer misses.

Even if your git history gets messy (or you rebuild the repo), the reasoning trail can still be there: `memo` stores the why in `.mpm-data/`.
Back up `.mpm-data/` and you can usually reconstruct faster (often cleaner) with AI.

---

## What is MPM?

MPM is a set of MCP tools + rules for long-running, high-signal AI coding.
Initialize once, then paste `_MPM_PROJECT_RULES.md` into your client's system rules.

### 🚀 30-Second Start (Do This First)

```text
1) initialize_project
2) Paste _MPM_PROJECT_RULES.md into client system rules
3) Ask directly: "Help me fix XXX and follow the rules"
```

If you do this first, you can start effectively without learning every tool in advance.

**Core Differentiators**:

| Traditional Approach | MPM Approach |
|---------------------|--------------|
| `grep "some symbol"` → 500 results | `code_search("some symbol")` → exact file:line |
| "I think this change should work" | `code_impact` → full call chain analysis |
| Starting from scratch every session | `system_recall` → cross-session memory |
| Long tasks drift or stop halfway | `task_chain` → long-running task chain with gates |

### Practical Workflow: One Complete Loop (Example)

Below is a copy-paste ready example. Paste it into any MCP client to run.

#### Standard Mode (Recommended for Beginners)

```text
Read _MPM_PROJECT_RULES.md and follow it.

Task: Fix <the issue you actually have>.
Requirements:
1. Locate the code first
2. Analyze impact scope
3. Implement the fix
4. Record the change reason
```

The AI will automatically execute: `initialize_project` → `code_search` → `code_impact` → modify code → `memo` to record.

#### Strict Mode (With Explicit Gates)

```text
Read _MPM_PROJECT_RULES.md and follow it.

Use task_chain to complete the following task:
Task: Fix <the issue you actually have>.

Phase requirements:
1. Locate phase: Use code_search to find the target function
2. Analyze phase: Use code_impact to evaluate impact scope
3. Implement phase: Fix and pass tests
4. Wrap-up phase: Use memo to record change reason

Report results after each phase and wait for confirmation before proceeding.
```

#### Closed-Loop Checklist

- **Understand**: `project_map` for structure, `flow_trace` for main chains
- **Locate**: `code_search` to pinpoint symbols
- **Assess**: `code_impact` to analyze call chain impact
- **Change**: Write code, fix compilation errors
- **Verify**: Run tests to confirm functionality
- **Record**: `memo` to archive change rationale

> ⚠️ **Data Hygiene**: The `.mpm-data/` directory stores local data and should not be committed to version control.
>
> **Project Binding**: `initialize_project` creates `.mpm-data/project_config.json` as an anchor. Future sessions auto-bind to this project root. If multiple anchors are found under a workspace aggregator folder, MPM refuses to guess and requires explicit `project_root`.

---

## What You Get

- Find the right code faster (`code_search`, `project_map`, `flow_trace`)
- Change with fewer surprises (`code_impact`)
- Run long tasks with checkpoints (`task_chain`, `system_hook`)
- Keep a usable change log (`memo`, `system_recall`)

---

## Quick Start

### 1. Build

```powershell
# Windows
powershell -ExecutionPolicy Bypass -File scripts\build-windows.ps1

# Linux/macOS
./scripts/build-unix.sh
```

### 2. Configure MCP

Point to the build output: `mcp-server-go/bin/mpm-go(.exe)`

### 3. Start Using

```text
Initialize project
Help me locate and fix <your issue>, and follow _MPM_PROJECT_RULES.md
```

After initialization, MPM generates `_MPM_PROJECT_RULES.md` automatically. Treat it as the project's operating playbook:

- It tells the LLM naming conventions, tool order, and hard constraints
- You can start effectively without learning every tool detail first
- In a new chat, ask the LLM to read this file first to reduce mistakes

Recommended first prompt: `Read _MPM_PROJECT_RULES.md and follow it`

### 4. Release Packaging (Fixed Directory)

```powershell
python package_product.py
```

Notes:

- Output directory is fixed: `mpm-release/MyProjectManager`
- Each run removes previous `mpm-release` first, then rebuilds clean package contents


---

## Documentation

- **[MANUAL.md](./docs/MANUAL.md)** - Complete manual (all 13 tools + best practices + case studies)
- **[MANUAL_ZH.md](./docs/MANUAL_ZH.md)** - 中文版完整手册
- **[README_ZH.md](./README_ZH.md)** - 中文版

---

## Common Search Questions

- `How to do impact analysis in MCP?` -> use `code_impact`
- `How to make LLM understand business logic flow?` -> use `flow_trace`
- `How to quickly understand a module/area in the system?` -> use `project_map` (structure) + `flow_trace` (main chains)
- `How to monitor indexing progress for large repositories?` -> use `index_status`
- `How to force full indexing?` -> `initialize_project(force_full_index=true)`

See [MANUAL.md](./docs/MANUAL.md) for detailed examples.

---

## OpenCode Multi-Agent Mode

MPM provides a 5-role Agent pack (PM / Architect / Coder / Expert / Spider) for direct use in OpenCode. See [opencode-agents/README.md](./opencode-agents/README.md).

---

## Contact

- Support: GitHub Issues
- Email: `halflifezyf2680@gmail.com`

---

## License

MIT License
