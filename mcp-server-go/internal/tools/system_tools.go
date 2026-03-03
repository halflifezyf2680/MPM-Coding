package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"mcp-server-go/internal/core"
	"mcp-server-go/internal/services"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	headerKnownFacts = "## 📌 Known Facts (%d)\n\n"
	headerMemos      = "## 📝 Memos (%d)\n\n"
	formatFact       = "- **[%s]** %s _(ID: %d, %s)_\n"
	formatMemo       = "- **[%d] %s** (%s) %s: %s\n"
)

type index_build_status struct {
	Status      string `json:"status"`
	Mode        string `json:"mode,omitempty"`
	ProjectRoot string `json:"project_root"`
	StartedAt   string `json:"started_at,omitempty"`
	FinishedAt  string `json:"finished_at,omitempty"`
	TotalFiles  int    `json:"total_files,omitempty"`
	ElapsedMs   int64  `json:"elapsed_ms,omitempty"`
	Error       string `json:"error,omitempty"`
}

func indexStatusFile(projectRoot string) string {
	return filepath.Join(projectRoot, core.DataDirName, "index_status.json")
}

func writeIndexStatus(projectRoot string, st index_build_status) {
	st.ProjectRoot = projectRoot
	statusPath := indexStatusFile(projectRoot)
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	tmpPath := statusPath + ".tmp"
	if err := os.WriteFile(tmpPath, raw, 0644); err != nil {
		return
	}
	_ = os.Rename(tmpPath, statusPath)
}

func startAsyncIndexBuild(projectRoot string, ai *services.ASTIndexer, forceFull bool) {
	startedAt := time.Now()
	mode := "auto"
	if forceFull {
		mode = "full"
	}
	writeIndexStatus(projectRoot, index_build_status{
		Status:    "running",
		Mode:      mode,
		StartedAt: startedAt.Format(time.RFC3339),
	})

	go func(root string, started time.Time) {
		var (
			result *services.IndexResult
			err    error
		)
		if forceFull {
			result, err = ai.IndexFull(root)
		} else {
			result, err = ai.Index(root)
		}
		if err != nil {
			writeIndexStatus(root, index_build_status{
				Status:     "failed",
				Mode:       mode,
				StartedAt:  started.Format(time.RFC3339),
				FinishedAt: time.Now().Format(time.RFC3339),
				Error:      err.Error(),
			})
			return
		}

		if analysis, aErr := ai.AnalyzeNamingStyle(root); aErr == nil {
			rulesPath := filepath.Join(root, "_MPM_PROJECT_RULES.md")
			_ = generateProjectRules(rulesPath, analysis)
		}

		writeIndexStatus(root, index_build_status{
			Status:     "success",
			Mode:       mode,
			StartedAt:  started.Format(time.RFC3339),
			FinishedAt: time.Now().Format(time.RFC3339),
			TotalFiles: result.TotalFiles,
			ElapsedMs:  result.ElapsedMs,
		})
	}(projectRoot, startedAt)
}

// InitArgs 初始化参数
type InitArgs struct {
	ProjectRoot    string `json:"project_root" jsonschema:"description=项目根路径 (绝对路径)"`
	ForceFullIndex bool   `json:"force_full_index" jsonschema:"description=强制全量索引（禁用大仓库bootstrap策略，默认false）"`
}

type SessionManager struct {
	Memory        *core.MemoryLayer
	ProjectRoot   string
	TaskChainsV3  map[string]*TaskChainV3   // 协议状态机任务链
	AnalysisState map[string]*AnalysisState // 历史遗留的分析状态缓存（兼容保留）
}

// AnalysisState 第一步分析结果（临时存储）
type AnalysisState struct {
	Intent         string                 `json:"intent"`
	UserDirective  string                 `json:"user_directive"`
	ContextAnchors []CodeAnchor           `json:"context_anchors"`
	VerifiedFacts  []string               `json:"verified_facts"`
	Telemetry      map[string]interface{} `json:"telemetry"`
	Guardrails     Guardrails             `json:"guardrails"`
	Alerts         []string               `json:"alerts"`
}

// CodeAnchor 代码锚点
type CodeAnchor struct {
	Symbol string `json:"symbol"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Type   string `json:"type"`
}

// Guardrails 约束规则
type Guardrails struct {
	Critical []string `json:"critical"`
	Advisory []string `json:"advisory"`
}

// SystemRecallArgs 历史召回参数
type SystemRecallArgs struct {
	Keywords string `json:"keywords" jsonschema:"required,description=检索关键词"`
	Category string `json:"category" jsonschema:"description=过滤类型 (开发/重构/避坑等)"`
	Limit    int    `json:"limit" jsonschema:"default=20,description=返回条数"`
}

// IndexStatusArgs 索引状态参数
type IndexStatusArgs struct {
	ProjectRoot string `json:"project_root" jsonschema:"description=可选项目根路径，留空时使用当前会话项目"`
}

// RegisterSystemTools 注册系统工具
func RegisterSystemTools(s *server.MCPServer, sm *SessionManager, ai *services.ASTIndexer) {
	s.AddTool(mcp.NewTool("initialize_project",
		mcp.WithDescription(`initialize_project - 初始化项目环境与数据库

用途：
  任何其他 MPM 操作前，必须先调用此工具初始化项目环境。它会建立数据库索引、检测技术栈并生成项目规则。

参数：
  project_root (必填)
    项目根目录的绝对路径。如果留空，工具会尝试自动探测。
  force_full_index (可选)
    强制全量索引（禁用大仓库 bootstrap 策略）。默认 false。

说明：
  - 手动指定 project_root 时必须使用绝对路径。
  - 初始化成功后，会生成 _MPM_PROJECT_RULES.md 供 LLM 参考。

示例：
  initialize_project(project_root="D:/AI_Project/MyProject")
    -> 初始化指定路径的项目

触发词：
  "mpm 初始化", "mpm init"`),
		mcp.WithInputSchema[InitArgs](),
	), wrapInit(sm, ai))

	s.AddTool(mcp.NewTool("open_timeline",
		mcp.WithDescription(`open_timeline - 项目演进可视化界面

用途：
  生成并展示交互式时间线，可视化项目的开发历史和决策演进。

参数：
  无

说明：
  - 基于 memo 记录生成 project_timeline.html。
  - 会尝试自动在默认浏览器中打开生成的文件。

示例：
  open_timeline()
    -> 在浏览器中打开项目演进时间线

触发词：
  "mpm 时间线", "mpm timeline"`),
	), wrapOpenTimeline(sm))

	s.AddTool(mcp.NewTool("system_recall",
		mcp.WithDescription(`system_recall - 你的记忆回溯器 (少走弯路)

用途：
  【下手前推荐】想改某个功能，但不确定以前有没有类似的逻辑？或者怕踩到以前的坑？
  用此工具查一下记忆库，避免重复造轮子或重蹈覆辙。

参数策略：
  keywords (必填)
    想查什么就填什么，支持模糊匹配（空格拆分）。
  
  category (可选)
    缩小范围：如 "避坑" / "开发" / "决策"

触发词：
  "mpm 召回", "mpm 历史", "mpm recall"`),
		mcp.WithInputSchema[SystemRecallArgs](),
	), wrapSystemRecall(sm))

	s.AddTool(mcp.NewTool("index_status",
		mcp.WithDescription(`index_status - 查看 AST 索引后台任务状态

用途：
  查询 initialize_project 启动的后台索引任务进度、心跳和数据库文件大小。

参数：
  project_root (可选)
    指定项目根路径。留空时使用当前会话项目。

返回：
  - status/mode/started_at/finished_at
  - heartbeat(processed/total)
  - symbols.db / symbols.db-wal / symbols.db-shm 文件大小

触发词：
  "mpm 索引状态", "mpm index status"`),
		mcp.WithInputSchema[IndexStatusArgs](),
	), wrapIndexStatus(sm))
}

func wrapInit(sm *SessionManager, ai *services.ASTIndexer) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args InitArgs
		if err := request.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("参数格式错误： %v", err)), nil
		}

		root := args.ProjectRoot

		// 1. 危险路径过滤：拒绝可能导致路径漂移的输入
		dangerousRoots := []string{"", ".", "..", "/", "\\", "./", ".\\"}
		for _, d := range dangerousRoots {
			if root == d {
				root = "" // 强制触发自动探测
				break
			}
		}

		if root == "" {
			// 自动探测
			root = core.DetectProjectRoot()
		}

		if root == "" {
			return mcp.NewToolResultText("❌ 无法自动识别项目路径，请手动指定 project_root（需为绝对路径）。"), nil
		}

		// 1. 路径统一化 (Path Normalization)
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("路径解析失败： %v", err)), nil
		}

		absRoot = filepath.ToSlash(filepath.Clean(absRoot))
		if len(absRoot) > 1 && absRoot[1] == ':' {
			drive := strings.ToUpper(string(absRoot[0]))
			absRoot = drive + absRoot[1:]
		}

		// 2. 校验路径安全性
		if !core.ValidateProjectPath(absRoot) {
			return mcp.NewToolResultError(fmt.Sprintf("⛔ 敏感路径（系统或 IDE 目录），禁止在此初始化项目： %s", absRoot)), nil
		}

		// 3. 确保数据目录存在（自动处理迁移）
		dataDir, err := core.GetDataDir(absRoot)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("获取/创建数据目录失败： %v", err)), nil
		}

		// 4. 持久化项目配置
		configPath := filepath.Join(dataDir, core.ProjectConfigFile)
		configContent := fmt.Sprintf(`{
  "project_root": "%s",
  "initialized_at": "%s"
}`, absRoot, time.Now().Format(time.RFC3339))
		_ = os.WriteFile(configPath, []byte(configContent), 0644)

		// 5. 初始化记忆层
		mem, err := core.NewMemoryLayer(absRoot)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("初始化记忆层失败： %v", err)), nil
		}

		sm.Memory = mem
		sm.ProjectRoot = absRoot

		// 6. 植入 visualize_history.py (Timeline 生成脚本)
		// 写入到项目根目录，如果不存在或强制更新（这里简化为覆盖）
		scriptPath := filepath.Join(absRoot, "visualize_history.py")
		if err := os.WriteFile(scriptPath, []byte(VisualizeHistoryScript), 0644); err != nil {
			// 记录警告但不阻断
			fmt.Printf("Warning: Failed to inject visualize_history.py: %v\n", err)
		}

		// 7. 立即写入一份规则模板，索引完成后会在后台自动刷新为真实统计
		var rulesMsg = "\n\n[NEW] 已同步项目规则模板: _MPM_PROJECT_RULES.md\nIDE 将自动加载更新后的规则。"
		rulesPath := filepath.Join(absRoot, "_MPM_PROJECT_RULES.md")
		_ = generateProjectRules(rulesPath, &services.NamingAnalysis{IsNewProject: true})

		// 8. 异步启动索引，避免大项目初始化阻塞/超时
		startAsyncIndexBuild(absRoot, ai, args.ForceFullIndex)
		statusPath := filepath.ToSlash(indexStatusFile(absRoot))
		mode := "auto"
		if args.ForceFullIndex {
			mode = "full"
		}
		indexStatus := fmt.Sprintf("🚀 后台构建中（mode=%s, 状态文件: %s）", mode, statusPath)

		return mcp.NewToolResultText(fmt.Sprintf("✅ 项目初始化成功！\n\n项目目录: %s\n数据库已准备就绪。\nAST 索引: %s%s", absRoot, indexStatus, rulesMsg)), nil
	}
}

func generateProjectRules(path string, analysis *services.NamingAnalysis) error {
	mpmProtocol := `# MPM 强制协议

## 🚨 死规则 (违反即失败)

1. **改代码前** → 必须先 ` + "`code_search`" + ` 或 ` + "`project_map`" + ` 定位，严禁凭记忆改
2. **预计任务很长** → 必须使用 ` + "`task_chain`" + ` 协议状态机执行，禁止单次并发操作
3. **改代码后** → 必须立即 ` + "`memo`" + ` 记录
4. **准备改函数时** → 必须先 ` + "`code_impact`" + ` 分析谁在调用它
5. **code_search 失败** → 必须换词重试（同义词/缩写/驼峰变体），禁止放弃
6. **阅读业务流程时** → 优先使用 ` + "`flow_trace`" + `，禁止只看文件名凭感觉推断

---

## 🔧 工具使用时机

| 场景 | 必须使用的工具 |
|------|---------------|
| 刚接手陌生项目且无任何代码线索 / 上下文过多需收敛注意力 | ` + "`project_map`" + ` / ` + "`flow_trace`" + ` |
| 任务涉及多模块/多阶段修改，预计需要多轮对话才能完成 | ` + "`task_chain`" + ` (协议状态机) |
| 刚接手项目 / 宏观探索 | ` + "`project_map`" + ` |
| 理解业务逻辑主链 | ` + "`flow_trace`" + ` |
| 找具体函数/类的定义 | ` + "`code_search`" + ` |
| 准备修改某函数 | ` + "`code_impact`" + ` |
| 代码改完了 | ` + "`memo`" + ` (SSOT) |

---

## 🚫 禁止

- 禁止凭记忆修改代码
- 禁止 code_search 失败后直接放弃
- 禁止修改代码后不调用 memo
- 禁止并发调用工具
`

	var namingRules string
	if analysis.IsNewProject {
		namingRules = fmt.Sprintf(`
# 项目命名规范 (由 MPM 自动分析生成)

> **检测到新项目** (文件数: %d)
> 这是您的新项目，请建立良好的命名习惯。推荐使用 Pythonic 风格。

## 推荐规范

- **函数/变量**: snake_case (e.g., get_user, total_count)
- **类名**: PascalCase (e.g., UserHandler, DataModel)
- **私有成员**: 使用 _ 前缀 (e.g., _internal_state)

---
`, analysis.FileCount)
	} else {
		funcExample := "`get_task`, `session_manager`"
		classExample := "`TaskContext`, `SessionManager`"
		if analysis.DominantStyle == "camelCase" {
			funcExample = "`getTask`, `sessionManager`"
		}

		prefixesStr := "无特殊前缀"
		if len(analysis.CommonPrefixes) > 0 {
			prefixesStr = strings.Join(analysis.CommonPrefixes, ", ")
		}

		samplesStr := strings.Join(analysis.SampleNames, ", ")

		namingRules = fmt.Sprintf(`
# 项目命名规范 (由 MPM 自动分析生成)

> **重要**: 此规范基于项目现有代码自动提取。LLM 必须严格遵守以确保风格一致。

## 检测结果

| 项目类型 | 旧项目 (检测到 %d 个源码文件，%d 个符号) |
|---------|------|
| **函数/变量风格** | %s (%s) |
| **类名风格** | %s |
| **常见前缀** | %s |

## 命名约定

-   **函数/变量**: 使用 %s，示例: %s
-   **类名**: 使用 %s，示例: %s
-   **禁止模糊修改**: 修改前必须用 code_search 确认目标唯一性。

## 代码示例 (从项目中提取)

%s

---

> **提示**: 如需修改规范，请直接编辑此文件。IDE 会自动读取更新后的内容。
`,
			analysis.FileCount,
			analysis.SymbolCount,
			analysis.DominantStyle,
			analysis.SnakeCasePct,
			analysis.ClassStyle,
			prefixesStr,
			analysis.DominantStyle,
			funcExample,
			analysis.ClassStyle,
			classExample,
			samplesStr,
		)
	}

	content := mpmProtocol + "\n" + namingRules
	return os.WriteFile(path, []byte(content), 0644)
}

func wrapIndexStatus(sm *SessionManager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		_ = ctx

		var args IndexStatusArgs
		if err := request.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("参数错误: %v", err)), nil
		}

		root := strings.TrimSpace(args.ProjectRoot)
		if root == "" {
			root = sm.ProjectRoot
		}
		if root == "" {
			return mcp.NewToolResultError("项目未初始化，请先执行 initialize_project 或传入 project_root"), nil
		}

		absRoot, err := filepath.Abs(root)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("路径解析失败: %v", err)), nil
		}
		absRoot = filepath.ToSlash(filepath.Clean(absRoot))

		result := map[string]interface{}{
			"project_root": absRoot,
		}

		statusPath := indexStatusFile(absRoot)
		result["status_file"] = filepath.ToSlash(statusPath)
		if raw, err := os.ReadFile(statusPath); err == nil {
			var status map[string]interface{}
			if err := json.Unmarshal(raw, &status); err == nil {
				result["index_status"] = status
			} else {
				result["index_status_raw"] = string(raw)
			}
		} else {
			result["index_status_error"] = err.Error()
		}

		heartbeatPath := filepath.Join(absRoot, core.DataDirName, "heartbeat")
		result["heartbeat_file"] = filepath.ToSlash(heartbeatPath)
		if raw, err := os.ReadFile(heartbeatPath); err == nil {
			var heartbeat map[string]interface{}
			if err := json.Unmarshal(raw, &heartbeat); err == nil {
				result["heartbeat"] = heartbeat
			} else {
				result["heartbeat_raw"] = string(raw)
			}
		} else {
			result["heartbeat_error"] = err.Error()
		}

		sizeMap := map[string]int64{}
		for _, name := range []string{"symbols.db", "symbols.db-wal", "symbols.db-shm"} {
			p := filepath.Join(absRoot, core.DataDirName, name)
			if st, err := os.Stat(p); err == nil {
				sizeMap[name] = st.Size()
			}
		}
		result["db_file_sizes"] = sizeMap

		rawOut, _ := json.MarshalIndent(result, "", "  ")
		return mcp.NewToolResultText(string(rawOut)), nil
	}
}

func wrapOpenTimeline(sm *SessionManager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		root := sm.ProjectRoot
		if root == "" {
			return mcp.NewToolResultError("❌ 项目未初始化，请先调用 initialize_project"), nil
		}

		// 1. 定位脚本 (优先 scripts/, 其次 root)
		scriptPath := filepath.Join(root, "scripts", "visualize_history.py")
		if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
			scriptPath = filepath.Join(root, "visualize_history.py")
			if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
				return mcp.NewToolResultError(fmt.Sprintf("❌ 找不到生成脚本: %s (checked scripts/ and root)", "visualize_history.py")), nil
			}
		}

		// 2. 生成 HTML (Python)
		cmd := exec.Command("python", scriptPath)
		cmd.Dir = root
		output, err := cmd.CombinedOutput()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("❌ 生成 Timeline 失败:\n%s\nOutput: %s", err, string(output))), nil
		}

		// 3. 定位 HTML
		htmlPath := filepath.Join(root, "project_timeline.html")
		if _, err := os.Stat(htmlPath); os.IsNotExist(err) {
			return mcp.NewToolResultError("❌ 脚本执行成功但未生成 project_timeline.html"), nil
		}

		// 4. 打开浏览器
		htmlURL := "file:///" + filepath.ToSlash(htmlPath)
		edgeCmd := exec.Command("cmd", "/c", "start", "msedge", fmt.Sprintf("--app=%s", htmlURL))
		if err := edgeCmd.Start(); err != nil {
			fallbackCmd := exec.Command("cmd", "/c", "start", htmlURL)
			if err := fallbackCmd.Start(); err != nil {
				return mcp.NewToolResultText(fmt.Sprintf("⚠️ Timeline 已生成但无法自动打开。\n路径: %s", htmlPath)), nil
			}
		}

		return mcp.NewToolResultText(fmt.Sprintf("✅ Timeline 已生成并尝试打开。\n文件: %s", htmlPath)), nil
	}
}

func wrapSystemRecall(sm *SessionManager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args SystemRecallArgs
		if err := request.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("参数错误: %v", err)), nil
		}

		if sm.ProjectRoot == "" {
			return mcp.NewToolResultError("项目未初始化"), nil
		}

		// 1. 查询 Memos（历史修改记录）
		memos, err := sm.Memory.SearchMemos(ctx, args.Keywords, args.Category, args.Limit)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("检索 memos 失败: %v", err)), nil
		}

		// 2. 查询 Known Facts（铁律/避坑经验）
		facts, err := sm.Memory.QueryFacts(ctx, args.Keywords, args.Limit)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("检索 known_facts 失败: %v", err)), nil
		}

		// 3. 检查是否有结果
		if len(memos) == 0 && len(facts) == 0 {
			return mcp.NewToolResultText("未找到相关记录"), nil
		}

		// 4. 构建返回结果
		var sb strings.Builder

		// 输出 Known Facts
		if len(facts) > 0 {
			sb.WriteString(fmt.Sprintf(headerKnownFacts, len(facts)))
			for _, f := range facts {
				sb.WriteString(fmt.Sprintf(formatFact,
					f.Type,
					f.Summarize,
					f.ID,
					f.CreatedAt.Format("2006-01-02")))
			}
			sb.WriteString("\n")
		}

		// 输出 Memos
		if len(memos) > 0 {
			sb.WriteString(fmt.Sprintf(headerMemos, len(memos)))
			for _, m := range memos {
				sb.WriteString(fmt.Sprintf(formatMemo,
					m.ID,
					m.Timestamp.Format("2006-01-02 15:04"),
					m.Category,
					m.Act,
					m.Content))
			}
		}

		return mcp.NewToolResultText(sb.String()), nil
	}
}
