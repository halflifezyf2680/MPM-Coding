# MPM-Coding 技术白皮书

中文 | [English](WHITEPAPER_EN.md)

> 本文档面向技术决策者和开发者，阐述 MPM-Coding 的工程架构、核心算法和差异化设计。
> 使用手册见 [MANUAL.md](MANUAL.md)。

---

## 1. 核心论点

AI 编程工具的竞争焦点正在从"模型能力"转向"上下文质量"。

MPM-Coding 不尝试让模型变聪明。它的全部工程投入围绕一个目标：**用最少的 token 让 AI 精确理解代码结构**。每一个工具的输出设计、每一条索引策略、每一个降级路径，都服务于这个目标。

这不仅是功能列表的问题。同样的功能名，底层实现质量差异巨大。本文档展开这些差异。

---

## 2. AST 索引引擎

### 2.1 架构：Go 主进程 + Rust 索引器

索引构建由独立 Rust 二进制完成，通过 `os/exec` 调用，JSON 通信。主进程零 CGO 依赖。

- **Rust 端**：tree-sitter 解析 + rayon 并行处理，所有语言共享同一套解析管线
- **Go 端**：MCP 工具层 + SQLite 存储层 + 文件监控层，职责分离

支持 11 种语言（tree-sitter 绑定）：Python、JavaScript、TypeScript、TSX、HTML、CSS、Go、Rust、Java、C、C++。

### 2.2 两阶段索引策略

```
第一阶段（默认）：不传 --extensions → 按实际文件扩展名自适应解析
     ↓ 失败时
第二阶段（降级）：传 --extensions → 仅解析检测到的技术栈对应文件
```

第一阶段让 Rust 端看到所有文件，按真实扩展名匹配 parser，覆盖所有 tree-sitter 支持的语言。第二阶段是安全网——仅在第一阶段失败时触发，用 Go 端检测到的技术栈类型限制解析范围。

### 2.3 增量索引：精确到文件

`IndexFiles(projectRoot, changedFiles)` 将变更文件列表写入临时文件（`.files` 后缀），传给 Rust 端执行增量解析。索引完成后自动清理临时文件。

不是"重建索引"，不是"全量扫描"。只重解析改过的那几个文件。

### 2.4 索引新鲜度：三层缓存

| 层级 | 机制 | 阈值 |
|------|------|------|
| 内存层 | `lastIndexAt` map + mutex | 5 分钟 |
| 文件层 | `symbols.db` 修改时间 + 文件大小 | - |
| 数据库层 | `files` 表存在性 + 行数检查 | - |

工具调用前自动检查新鲜度，过期才触发增量索引。避免每次调用都重建。

### 2.5 性能基准

实测数据（SWE-Bench 工作区，4 个开源项目混合）：

| 规模 | 文件数 | MPM 索引耗时 |
|------|--------|-------------|
| 单项目（xarray） | 328 | <1s |
| 单项目（sphinx） | 1,391 | <1s |
| 单项目（sympy） | 1,512 | ~1s |
| 单项目（astropy） | 1,142 | ~12s |
| 整个工作区 | 5,128 | 27s |

同等条件下，同类竞品（Node.js 实现）索引 astropy 单项目（1,142 文件）耗时 51.8 秒，且对其他 3 个项目静默失败（代码在子目录中，无法识别）。

---

## 3. 搜索引擎

### 3.1 code_search：5 层降级 + 深度上下文反查

```
精确匹配 → 前后缀匹配 → 子串匹配 → 编辑距离匹配 → 词根匹配
     ↓ 全部失败
Ripgrep 文本搜索 + GetSymbolAtLine 反查
```

关键设计：ripgrep 降级不是简单的文本搜索。每一行匹配结果都会调用 `GetSymbolAtLine()` 反查 AST 数据库，找到该行所属的符号。输出格式为：

```
L42: `someFunction(args)` in `handleRequest` (function)
```

即使走文本搜索路径，LLM 拿到的也是带语义上下文的结果，不是裸行号。

### 3.2 搜索引擎：ripgrep JSON + 纯 Go 降级

- 默认使用 `rg --json` 解析结构化输出（不是文本流解析）
- rg 二进制不存在时，自动降级到纯 Go 实现（文件遍历 + 字符串匹配）
- 上下文行收集：64 行滑动窗口，反向扫描重建
- 默认排除 20+ 种噪声目录/文件（`.mpm-data`、`*.lock`、`*.min.js`、`*.map` 等）

### 3.3 候选列表：始终展示

即使找到精确匹配，code_search 仍输出最多 5 个候选（去重后），包含符号类型、路径、匹配分数。LLM 可以判断"第二个可能才是我想要的"，而不是只能接受或拒绝唯一结果。

---

## 4. flow_trace：调用链分析引擎

### 4.1 双模式入口识别

```
输入 "handleRequest"              → symbol 模式（追踪单个符号）
输入 "internal/tools/a.go"         → file 模式（追踪文件内所有函数）
输入 "internal/tools/a.go:handle"  → file+symbol 混合模式
```

系统自动识别输入类型，无需手动指定模式。

### 4.2 文件模式：智能入口筛选

文件模式下的处理流程：

1. 提取文件中所有 callable（function/method）和 type（class/struct/interface）符号
2. 为每个符号构建 flow snapshot（调用链分析）
3. 按综合评分排序，截断展示（brief=1, standard=2, deep=4）

评分公式：

```
score = ExternalIn * 50      // 跨文件入度（权重最高）
      + ExternalOut * 1       // 跨文件出度
      + BackwardDirect * 8    // 直接上游数
      + BackwardIndirect * 2  // 间接上游数
      + ForwardDirect * 2     // 直接下游数
      + ForwardIndirect * 1   // 间接下游数
      + complexity / 8.0      // 复杂度贡献
```

跨文件被调用多的函数排在最前面——因为它们是文件对外暴露的核心接口。

### 4.3 副作用检测：基于证据的两阶段分析

不是简单的名称关键词匹配，而是收集实际调用证据后判定：

```
Phase 1: 从 node.Calls 和 related 节点的 Calls 中收集所有被调用函数名
Phase 2: 对 5 类副作用（filesystem/database/network/process/state）按 API 特征打分
Phase 3: 奥卡姆剃刀——无证据则返回空，避免泛化
```

每类副作用有独立的证据规则（如 database 证据匹配 `query`/`begin`/`commit`/`db.exec` 等实际 API 调用），单次匹配得 10 分，总分 >= 10 才报告。

同时检测业务阶段（init → validate → execute → query → persist），帮助 LLM 快速理解代码执行流程。

### 4.4 输出设计：注意力收敛

flow_trace 的输出是**调用链索引**，不是源代码。标题下方声明边界：

> 以下为调用链索引（上下游符号关系），不含实现细节。提出方案前应阅读目标文件完整上下文。

这防止 LLM 将调用链输出误认为完整理解，跳过实际代码阅读。

---

## 5. code_impact：影响分析

### 5.1 两阶段分析：BFS 链路发现 + Dice Random Walk 评分

**阶段 1 — BFS 链路发现**：从目标符号出发，沿调用图（正向 adjacency 或反向 reverse_adjacency）BFS 搜索，深度上限 3。发现直接调用者和间接调用者，确定风险等级（影响节点数 0→low, ≤3→low, ≤10→medium, >10→high）。

**阶段 2 — Dice Random Walk 复杂度评分**：从目标符号出发，在有向图上执行 1000 次随机游走（每次 10 步，damping 0.85）。统计游走覆盖的节点数（coverage），结合 fan-out 和 fan-in 计算复杂度：

```
complexity = coverage × 0.5 + fanOut × 2.0 + fanIn × 1.0
```

归一化到 0-100，分级：Simple(<20) / Medium(<50) / High(<80) / Extreme(≥80)。

### 5.2 结构化 Markdown 输出

每次影响分析输出结构化的 Markdown：风险等级、复杂度评分、影响节点数、直接调用者列表（前 10 个，含文件路径和行号）、间接调用者列表（前 20 个，含文件路径和行号，BFS 按距离排序）。与其他工具输出风格一致，LLM 直接消费。

---

## 6. known_facts：经验策略引擎

### 6.1 多维加权评分

`before_action` 模式召回相关事实时，每个 KnownFact 按以下公式评分排序：

```
score = contextMatch * 0.45    // 上下文关键词匹配度
      + confidence * 0.25      // 事实置信度
      + successRate * 0.20     // 历史采纳成功率
      + exploration * 0.10     // 冷门探索奖励
      - failurePenalty         // 失败惩罚
```

其中 `exploration` 项使用 `sqrt(log(totalHits+1)+1) / (hitCount+1)` 计算——鼓励曝光少但全局命中多的高质量冷门事实浮出水面。

### 6.2 贝叶斯式置信度演化

`after_action` 根据结果更新事实置信度：

| 场景 | 公式 | 效果 |
|------|------|------|
| 采纳 + 成功 | `confidence += 0.10 * (1 - confidence)` | 指数趋近 1.0，收敛递减 |
| 采纳 + 失败 | `confidence -= 0.20 * confidence` | 快速惩罚，比例收缩 |
| 未采纳 | `confidence -= 0.02 * confidence` | 微弱衰减，不误杀 |

自动状态晋升：`confidence >= 0.75 && support >= 3` → active；`confidence < 0.25 && reject >= 2` → rejected。

### 6.3 事件审计追踪

每次事实交互（exposure/adopt/reject/add）都记录 `FactEvent`，包含 eventType、task_id、phase、context_signature 和 payload_json。构成完整的事实生命周期审计链。

### 6.4 自动持久化 + 阈值治理

`after_action` 将新观察自动写入 `.claude/CLAUDE.md` 和项目级 `AGENTS.md`：

- 使用标记区间（`## MPM Known Facts` ... `<!-- MPM_KNOWN_FACTS_END -->`）
- 写入前检查内容是否已存在（去重）
- `.tmp` + `os.Rename` 原子写入
- 死区阈值信号：>50 条建议去重，>70 条建议压缩（只在向上跨越时触发，防止震荡）

---

## 7. task_chain：状态机驱动任务体系

### 7.1 自动推进设计

```
init      → 自动 start 首阶段
spawn     → 自动 start 首子任务
complete_sub → 自动 start 下一个 pending 子任务
```

AI 只需要调 complete，系统自动推进到下一步。减少调用次数，降低出错概率。

### 7.2 Gate 失败：路由回退，不是简单重试

```
gate 失败 → RetryCount++
         → 超过 max_retries？ → 任务标记失败
         → 未超限？
            → 有 on_fail？ → 跳转到目标阶段，重置目标阶段状态，清空 summary
            → 无 on_fail？ → 重试当前 gate
```

on_fail 路由不是"重新跑 gate"，而是"回到前面的阶段重新执行"。回退范围由 init 时的 phases 定义决定。

### 7.3 Re-init 自审守卫

同一 task_id 被 init 两次以上时，系统直接返回错误，强制 AI 停下来向用户说明。防止 AI 在死循环中反复 re-init。

### 7.4 跨会话续传

任务状态持久化到 SQLite。会话中断后 `resume` 从数据库恢复断点，包括阶段状态、子任务进度和已完成阶段的 summary。

---

## 8. 文件监控：懒更新模式

```
文件变更 → watcher 检测（2000ms debounce）→ 标记 staleFiles
                                              ↓
工具调用 → ensureFresh() → 提取 staleFiles → IndexFiles() 增量索引
```

不是文件一改就重建索引，而是"标记脏，用的时候才洗"。避免快速连续保存（IDE auto-save）导致的频繁索引。

---

## 9. 数据层

### 9.1 SQLite 配置

```sql
PRAGMA journal_mode = WAL;       -- 高并发读写
PRAGMA synchronous = NORMAL;     -- 性能与安全平衡
PRAGMA busy_timeout = 30000;     -- 30秒忙等待
```

Go 连接池：`SetMaxOpenConns(1)` + `SetMaxIdleConns(1)`——单连接减少进程内锁竞争。

### 9.2 忙等待重试

```go
// with_sqlite_busy_retry: 6 次重试，线性退避 (200ms * attempt)
for attempt := 0; attempt < 6; attempt++ {
    err = fn()
    if err == nil { return nil }
    if !isBusy(err) { return err }
    time.Sleep(200 * time.Millisecond * time.Duration(attempt+1))
}
```

### 9.3 Schema 自愈

`healSchema()` 在每次打开数据库时自动执行：
- 检查表是否存在，不存在则创建
- 检查列是否存在（`PRAGMA table_info`），缺失则 `ALTER TABLE ADD COLUMN`
- 检测重复列（跨版本升级可能产生），跳过
- 创建缺失的索引

### 9.4 Memo 灾难恢复

```
DB 初始化 → 检查 memos 表是否为空
  → 空？尝试从 memo_archive.jsonl 恢复（JSONL 格式）
  → JSONL 也没有？从 dev-log.md 用正则解析恢复
```

三层恢复策略确保即使 SQLite 文件损坏，历史记录也不会完全丢失。所有新 memo 异步追加到 JSONL 归档。

---

## 10. 路径安全

### 10.1 项目根探测：4 级策略

| 优先级 | 策略 | 机制 |
|--------|------|------|
| 1 | 锚点搜索向上 | 查找 `.mpm-data/project_config.json` |
| 2 | 有界 BFS 向下 | 深度 ≤ 3，最多 200 目录 |
| 3 | 环境变量 | `MPM_PROJECT_ROOT`、`WORKSPACE_FOLDER` 等 5 个 key |
| 4 | CWD 兜底 | 当前目录 + 10 个项目标记文件检查 |

### 10.2 黑名单过滤

阻止以下目录初始化：
- 系统目录：`windows`、`system32`、`program files`
- IDE 运行时目录：`vscode`、`cursor`、`claude`、`windsurf` 等
- 敏感目录：`appdata`、`temp`、`prefetch`
- 卷根（如 `D:\`）

唯一豁免：目录下存在 `.git`，视为真正的项目根。

### 10.3 参数路径规范化

所有 scope/file_path 参数传入前经过 5 步处理：
1. 拒绝绝对路径（含 Windows 盘符）
2. 拒绝 `../` 路径遍历
3. `\` → `/` 统一
4. `path.Clean()` 清理
5. 去除 `./` 前缀

### 10.4 旧版自动迁移

检测到 `.mcp-data` 目录时自动迁移到 `.mpm-data`，优先 rename（快速），Windows 跨卷时 copy+fallback，迁移后留 `MIGRATED.txt` 标记。

---

## 11. project_map：自适应输出

### 11.1 目录树展开策略

根据一级目录数量自动调整展开深度：

| 目录数 | 策略 |
|--------|------|
| ≤ 20 | 全部展开到 L3 |
| 20-40 | 前 8 个展开到 L3，前 18 个展开到 L2 |
| > 40 | 前 6 个展开到 L3，前 12 个展开到 L2 |

### 11.2 符号视图：三级折叠

| 文件序号 | 展示方式 |
|----------|----------|
| 1-10 | 详细展示每个符号（名称、行号、复杂度标记） |
| 11-30 | 仅展示文件名和符号数 |
| > 30 | 折叠为省略提示 |

每个符号附带 `[HIGH:xx.x]` / `[MED:xx.x]` / `[LOW:xx.x]` 复杂度标记，按复杂度降序排列。

### 11.3 大输出自动保存

输出超过 2000 字符时自动保存到 `.mpm-data/project_map_*.md`，返回文件路径。避免大量文本注入上下文窗口。

---

## 12. 技术栈检测

递归扫描项目文件（最大深度 8 层），检测 6 种技术栈，每种有专属忽略目录：

| 技术栈 | 扩展名 | 专属忽略 |
|--------|--------|----------|
| Python | .py | site-packages, htmlcov, .pytest_cache |
| Frontend | .js/.jsx/.ts/.tsx/.html/.css | coverage, .next, .nuxt |
| Go | .go | vendor, bin |
| Rust | .rs | target |
| C/C++ | .c/.h/.cpp/.hpp/.cc | cmake-build-debug |
| Java | .java | .gradle |

集成 `.gitignore` 解析，自动排除用户自定义的忽略目录。

---

## 13. 设计哲学总结

MPM 的工程决策遵循三条原则：

**1. 输出即上下文清洗**

每个工具的输出不是给人类看的报告，是给 LLM 的精确注入。code_search 返回符号位置不是 grep 结果，code_impact 返回调用链全景不是文件列表，flow_trace 返回主链路索引不是源代码。输出本身就是对上下文噪声的过滤。

**2. 降级不能降质**

ripgrep 不可用时降级到纯 Go 搜索，但仍然做 GetSymbolAtLine 反查。AST 匹配失败时降级到文本搜索，但仍然标注所属符号。每一个降级路径都保持语义上下文，不会退化为裸文本。

**3. 元数据优先，源码按需**

工具返回位置、签名、调用关系、复杂度评分——全是元数据。LLM 拿到元数据后自主决定是否需要 Read 源文件。不主动把源代码塞进上下文窗口，因为每多一页源码就是多一份 token 成本和多一次注意力分散。

---

*MPM-Coding Whitepaper v1.0 — 2026-05*
