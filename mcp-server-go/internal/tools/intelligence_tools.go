package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"mcp-server-go/internal/services"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// AnalyzeArgs 任务分析参数
type AnalyzeArgs struct {
	TaskDescription string   `json:"task_description" jsonschema:"required,description=用户的原始指令/任务详情"`
	Intent          string   `json:"intent" jsonschema:"description=LLM 自行判断的意向 (DEBUG/DEVELOP/REFACTOR/DESIGN/RESEARCH)"`
	Symbols         []string `json:"symbols" jsonschema:"description=提取的代码符号"`
	ReadOnly        bool     `json:"read_only" jsonschema:"description=是否为只读分析模式"`
	Scope           string   `json:"scope" jsonschema:"description=任务范围描述"`
	Step            int      `json:"step" jsonschema:"description=执行步骤 (1=分析, 2=生成策略)，默认为1"`
	TaskID          string   `json:"task_id" jsonschema:"description=步骤2时必填，步骤1返回的 task_id"`
}

// FactArgs 事实存档参数
type FactArgs struct {
	Type      string `json:"type" jsonschema:"required,description=事实类型 (如：铁律、避坑)"`
	Summarize string `json:"summarize" jsonschema:"required,description=事实描述"`
}

// MissionBriefing 情报包结构
type MissionBriefing struct {
	MissionControl   MissionControl         `json:"mission_control"`
	ContextAnchors   []CodeAnchor           `json:"context_anchors"`
	VerifiedFacts    []string               `json:"verified_facts"`
	Telemetry        map[string]interface{} `json:"telemetry"`
	Guardrails       Guardrails             `json:"guardrails"`
	Alerts           []string               `json:"alerts"`
	StrategicHandoff string                 `json:"strategic_handoff"`
}

type MissionControl struct {
	Intent        string `json:"intent"`
	UserDirective string `json:"user_directive"`
}

// RegisterIntelligenceTools 注册智能分析工具
func RegisterIntelligenceTools(s *server.MCPServer, sm *SessionManager, ai *services.ASTIndexer) {
	s.AddTool(mcp.NewTool("known_facts",
		mcp.WithDescription(`known_facts - 原子级经验事实存档

用途：
	  将经过验证的代码规则、铁律或重要的避坑经验存入记忆层，用于后续检索与复用。

参数：
  type (必填)
    事实类型，如 "铁律", "避坑", "规范", "逻辑" 等。
  
  summarize (必填)
    事实的具体描述，应简洁明了。

示例：
  known_facts(type="避坑", summarize="修改 context 逻辑前必须先备份 session 数据")
    -> 保存一条重要的经验法则

触发词：
  "mpm 铁律", "mpm 避坑", "mpm fact"`),
		mcp.WithInputSchema[FactArgs](),
	), wrapSaveFact(sm))
}

func wrapAnalyze(sm *SessionManager, ai *services.ASTIndexer) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args AnalyzeArgs
		if err := request.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("参数格式错误: %v", err)), nil
		}

		if sm.ProjectRoot == "" {
			return mcp.NewToolResultError("⚠️ 项目未初始化，无法执行任务分析。请先调用 initialize_project。"), nil
		}

		// 默认 step = 1
		step := args.Step
		if step == 0 {
			step = 1
		}

		// 生成或使用任务 ID
		var taskID string
		if step == 1 {
			// Step 1: 生成新的 taskID
			taskID = fmt.Sprintf("analyze_%d", time.Now().UnixNano())
		} else {
			// Step 2: 使用用户传入的 taskID
			taskID = args.TaskID
			if taskID == "" {
				return mcp.NewToolResultError("⚠️ Step 2 需要提供 task_id 参数（来自 Step 1 的返回值）"), nil
			}
		}

		if step == 1 {
			// ===== 步骤1：真实分析 =====
			return handleAnalyzeStep1(ctx, sm, ai, args, taskID)
		} else {
			// ===== 步骤2：动态策略 =====
			return handleAnalyzeStep2(sm, ai, args, taskID)
		}
	}
}

// handleAnalyzeStep1 执行第一步：真实分析，保存状态
func handleAnalyzeStep1(ctx context.Context, sm *SessionManager, ai *services.ASTIndexer, args AnalyzeArgs, taskID string) (*mcp.CallToolResult, error) {
	// 1. 意图识别
	intent := determineIntent(args.TaskDescription, args.Intent, args.ReadOnly)

	scope, err := normalizeProjectRelativePath(sm.ProjectRoot, args.Scope, "scope")
	if err != nil {
		return mcp.NewToolResultError("❌ " + err.Error()), nil
	}

	// 1.1 索引预热（避免使用过期索引）
	var prewarmAlert string
	if err := warmIndexForPath(ai, sm.ProjectRoot, scope); err != nil {
		prewarmAlert = fmt.Sprintf("⚠️ 索引预热失败，当前分析可能基于旧索引: %v", err)
	}

	// 2. 符号预搜索 (Code Anchors)
	var anchors []CodeAnchor
	limit := 10
	if len(args.Symbols) < limit {
		limit = len(args.Symbols)
	}

	uniqueSymbols := make(map[string]bool)
	for i := 0; i < limit; i++ {
		sym := args.Symbols[i]
		if uniqueSymbols[sym] {
			continue
		}
		uniqueSymbols[sym] = true

		anchor := resolveCodeAnchor(ctx, sm, ai, sym, scope)
		if anchor == nil {
			continue
		}
		anchors = append(anchors, *anchor)
	}

	// 3. 记忆加载（仅 Facts）
	var facts []string
	if sm.Memory != nil {
		keywords := buildFactKeywords(args.TaskDescription, args.Symbols)
		knownFacts, _ := sm.Memory.QueryFacts(ctx, keywords, 10)
		for _, f := range knownFacts {
			facts = append(facts, f.Summarize)
		}
	}

	// 4. 构建禁令 (Guardrails)
	guardrails := buildGuardrails(intent, args.ReadOnly)

	// 5. 复杂度分析与遥测
	telemetry := make(map[string]interface{})
	var complexityAlerts []string

	if len(args.Symbols) > 0 {
		compReport, err := ai.AnalyzeComplexity(sm.ProjectRoot, args.Symbols)
		if err == nil && compReport != nil {
			maxScore := 0.0
			for _, risk := range compReport.HighRiskSymbols {
				if risk.Score > maxScore {
					maxScore = risk.Score
				}
				if risk.Score >= 50 {
					complexityAlerts = append(complexityAlerts, fmt.Sprintf("⚠️ [Complexity] %s: %.1f - %s", risk.SymbolName, risk.Score, risk.Reason))
				}
			}

			level := getComplexityLevel(maxScore)
			if level == "High" {
				telemetry["complexity"] = map[string]interface{}{
					"score": maxScore,
					"level": level,
				}
			}
		}
	}

	// 6. 生成综合警告
	alerts := generateAlerts(args.TaskDescription, intent, args.ReadOnly)
	if prewarmAlert != "" {
		alerts = append(alerts, prewarmAlert)
	}
	alerts = append(alerts, complexityAlerts...)

	// 7. 保存状态到 Session
	directive := truncateRunes(args.TaskDescription, 300)

	state := &AnalysisState{
		Intent:         intent,
		UserDirective:  directive,
		ContextAnchors: anchors,
		VerifiedFacts:  facts,
		Telemetry:      telemetry,
		Guardrails:     guardrails,
		Alerts:         alerts,
	}

	if sm.AnalysisState == nil {
		sm.AnalysisState = make(map[string]*AnalysisState)
	}
	sm.AnalysisState[taskID] = state

	// 8. 返回第一步结果（不包含 strategic_handoff）
	step1Result := map[string]interface{}{
		"step":    1,
		"task_id": taskID,
		"mission_control": map[string]interface{}{
			"intent":         intent,
			"user_directive": directive,
		},
		"context_anchors": anchors,
		"verified_facts":  facts,
		"telemetry":       telemetry,
		"guardrails":      guardrails,
		"alerts":          alerts,
		"next_step":       "调用 step=2 并携带 task_id 生成战术策略",
	}

	jsonData, err := json.MarshalIndent(step1Result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("JSON 序列化失败: %v", err)), nil
	}

	return mcp.NewToolResultText(string(jsonData)), nil
}

// handleAnalyzeStep2 执行第二步：基于第一步结果动态生成 strategic_handoff
func handleAnalyzeStep2(sm *SessionManager, ai *services.ASTIndexer, args AnalyzeArgs, taskID string) (*mcp.CallToolResult, error) {
	// 1. 从 Session 读取第一步的状态
	state, exists := sm.AnalysisState[taskID]
	if !exists {
		return mcp.NewToolResultError("⚠️ 未找到第一步的分析结果，请先执行 step=1"), nil
	}

	// 2. 基于第一步结果动态生成 strategic_handoff
	strategicHandoff := generateDynamicStrategicHandoff(state)

	// 3. 组装完整的 Mission Briefing
	briefing := MissionBriefing{
		MissionControl: MissionControl{
			Intent:        state.Intent,
			UserDirective: state.UserDirective,
		},
		ContextAnchors:   state.ContextAnchors,
		VerifiedFacts:    state.VerifiedFacts,
		Telemetry:        state.Telemetry,
		Guardrails:       state.Guardrails,
		Alerts:           state.Alerts,
		StrategicHandoff: strategicHandoff,
	}

	// 4. 清理临时状态
	delete(sm.AnalysisState, taskID)

	// 5. 返回第二步结果
	jsonData, err := json.MarshalIndent(briefing, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("JSON 序列化失败: %v", err)), nil
	}

	return mcp.NewToolResultText(string(jsonData)), nil
}

// generateDynamicStrategicHandoff 基于第一步分析结果动态生成 strategic_handoff
func generateDynamicStrategicHandoff(state *AnalysisState) string {
	var parts []string

	// 1. 任务意图
	intentHint := getIntentHint(state.Intent)
	parts = append(parts, fmt.Sprintf("[任务意图]: %s", state.Intent))
	parts = append(parts, intentHint)

	// 2. 基于真实分析结果的建议
	parts = append(parts, "")
	parts = append(parts, "[情报评估与建议]")

	// 2.1 代码定位情况
	if len(state.ContextAnchors) == 0 {
		parts = append(parts, "!!! CRITICAL: 未定位到任何代码符号 !!!")
		parts = append(parts, "建议：使用 project_map 查看项目结构，或检查 symbols 参数是否正确")
	} else {
		parts = append(parts, fmt.Sprintf("已定位到 %d 个代码符号", len(state.ContextAnchors)))
	}

	// 2.2 复杂度评估
	if comp, ok := state.Telemetry["complexity"].(map[string]interface{}); ok {
		if level, ok := comp["level"].(string); ok {
			switch level {
			case "High":
				parts = append(parts, "!!! 任务复杂度极高 !!!")
				parts = append(parts, "建议：使用 code_impact 先分析影响范围，避免遗漏依赖关系")
			case "Medium":
				parts = append(parts, "任务复杂度中等，建议谨慎处理")
			case "Low":
				parts = append(parts, "任务复杂度较低，可直接开始")
			}
		}
	}

	// 2.3 约束提醒
	if len(state.Guardrails.Critical) > 0 {
		parts = append(parts, "")
		parts = append(parts, "!!! CRITICAL CONSTRAINTS (MANDATORY) !!!")
		for _, constraint := range state.Guardrails.Critical {
			parts = append(parts, fmt.Sprintf("- %s", constraint))
		}
		parts = append(parts, "!!! END OF CRITICAL CONSTRAINTS !!!")
	}

	// 3. 执行策略（按 intent 差异化）
	parts = append(parts, "")
	parts = append(parts, "[执行策略]")
	parts = append(parts, getIntentChecklist(state.Intent)...)

	// 4. Tool Strategy
	parts = append(parts, "")
	parts = append(parts, "[Tool Strategy - 基于情报分析]")
	parts = append(parts, getIntentToolStrategy(state.Intent, len(state.ContextAnchors) > 0)...)

	// 5. 你的判断
	parts = append(parts, "")
	parts = append(parts, "[你的判断]")
	parts = append(parts, "以上情报基于实际代码分析生成。请根据情报充分性判断是否需要补充调研。")
	parts = append(parts, "你拥有完全自主权。")

	return strings.Join(parts, "\n")
}

func resolveCodeAnchor(ctx context.Context, sm *SessionManager, ai *services.ASTIndexer, query, scope string) *CodeAnchor {
	if strings.TrimSpace(query) == "" {
		return nil
	}

	// 1) AST 精确匹配（对齐 code_search 的核心策略：先精确，再降级）
	astResult, _ := ai.SearchSymbolWithScope(sm.ProjectRoot, query, scope)
	if astResult != nil {
		if node := selectExactNodeForAnchor(astResult, query, scope); node != nil {
			return &CodeAnchor{Symbol: query, File: node.FilePath, Line: node.LineStart, Type: node.NodeType}
		}
	}

	// 2) 文本搜索兜底（ripgrep），并尝试用 GetSymbolAtLine 回溯所属符号
	rg := services.NewRipgrepEngine()
	searchRoot := sm.ProjectRoot
	if strings.TrimSpace(scope) != "" {
		searchRoot = filepath.Join(sm.ProjectRoot, scope)
	}

	matches, err := rg.Search(ctx, services.SearchOptions{
		Query:         query,
		RootPath:      searchRoot,
		CaseSensitive: false,
		WordMatch:     false,
		MaxCount:      20,
		ContextLines:  0,
	})
	if err != nil || len(matches) == 0 {
		return nil
	}

	var fallbackOwner *services.Node
	for _, m := range matches {
		owner, _ := ai.GetSymbolAtLine(sm.ProjectRoot, m.FilePath, m.LineNumber)
		if owner == nil {
			continue
		}
		if isInScope(owner.FilePath, scope) {
			if strings.EqualFold(owner.Name, query) || strings.EqualFold(owner.QualifiedName, query) {
				return &CodeAnchor{Symbol: query, File: owner.FilePath, Line: owner.LineStart, Type: owner.NodeType}
			}
			if fallbackOwner == nil {
				fallbackOwner = owner
			}
		}
	}

	if fallbackOwner != nil {
		return &CodeAnchor{Symbol: query, File: fallbackOwner.FilePath, Line: fallbackOwner.LineStart, Type: fallbackOwner.NodeType}
	}

	// 兜底：返回首个文本命中位置
	first := matches[0]
	return &CodeAnchor{Symbol: query, File: first.FilePath, Line: first.LineNumber, Type: "text"}
}

func selectExactNodeForAnchor(result *services.QueryResult, query, scope string) *services.Node {
	if result == nil {
		return nil
	}

	// Scope filtering (client-side)
	if result.FoundSymbol != nil {
		if !isInScope(result.FoundSymbol.FilePath, scope) {
			result.FoundSymbol = nil
		}
	}

	// 只接受“精确命名匹配”的 AST 结果，避免把相似候选当成锚点
	if result.FoundSymbol != nil {
		n := result.FoundSymbol
		if strings.EqualFold(n.Name, query) || strings.EqualFold(n.QualifiedName, query) {
			return n
		}
	}

	for i := range result.Candidates {
		c := result.Candidates[i].Node
		if !isInScope(c.FilePath, scope) {
			continue
		}
		if strings.EqualFold(c.Name, query) || strings.EqualFold(c.QualifiedName, query) {
			return &c
		}
	}

	return nil
}

func isInScope(filePath, scope string) bool {
	if strings.TrimSpace(scope) == "" {
		return true
	}
	path := strings.ReplaceAll(filePath, "\\", "/")
	s := strings.ReplaceAll(scope, "\\", "/")
	return strings.Contains(path, s)
}

func buildFactKeywords(taskDescription string, symbols []string) string {
	uniq := make(map[string]bool)
	var out []string

	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if uniq[s] {
			return
		}
		uniq[s] = true
		out = append(out, s)
	}

	// 优先使用 symbols（通常是最强的检索键）
	for _, sym := range symbols {
		if len(out) >= 8 {
			break
		}
		add(sym)
	}

	// 再补充 task_description 中的 ASCII 标识符（如函数名、文件名、工具名）
	for _, t := range extractASCIITokens(taskDescription, 12) {
		if len(out) >= 8 {
			break
		}
		add(t)
	}

	// 最后补充中文关键词
	for _, t := range extractHanTokens(taskDescription, 12) {
		if len(out) >= 8 {
			break
		}
		add(t)
	}

	return strings.Join(out, " ")
}

func extractASCIITokens(s string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	var tokens []string
	uniq := make(map[string]bool)

	var buf []rune
	flush := func() {
		if len(buf) == 0 {
			return
		}
		t := strings.TrimSpace(string(buf))
		buf = buf[:0]
		if len(t) < 3 || len(t) > 40 {
			return
		}
		lower := strings.ToLower(t)
		if lower == "http" || lower == "https" {
			return
		}
		if uniq[lower] {
			return
		}
		uniq[lower] = true
		tokens = append(tokens, t)
	}

	for _, r := range s {
		isOK := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '/' || r == '.'
		if isOK {
			buf = append(buf, r)
			continue
		}
		flush()
		if len(tokens) >= limit {
			break
		}
	}
	flush()
	if len(tokens) > limit {
		return tokens[:limit]
	}
	return tokens
}

func extractHanTokens(s string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	var tokens []string
	uniq := make(map[string]bool)

	var buf []rune
	flush := func() {
		if len(buf) == 0 {
			return
		}
		t := string(buf)
		buf = buf[:0]
		r := []rune(t)
		if len(r) < 2 {
			return
		}
		if len(r) > 4 {
			t = string(r[:4])
		}
		if uniq[t] {
			return
		}
		uniq[t] = true
		tokens = append(tokens, t)
	}

	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			buf = append(buf, r)
			continue
		}
		flush()
		if len(tokens) >= limit {
			break
		}
	}
	flush()
	if len(tokens) > limit {
		return tokens[:limit]
	}
	return tokens
}

func getIntentChecklist(intent string) []string {
	switch intent {
	case "DEBUG":
		return []string{
			"• 先复现并收集证据（日志/堆栈/最小复现），再动手改代码",
			"• 优先缩小范围：只改根因附近，避免大面积重写",
			"• 修复后补回归用例（防止同类问题复发）",
		}
	case "DEVELOP":
		return []string{
			"• 先找现有实现/约定（避免重复造轮子与风格漂移）",
			"• 新增接口/工具时同步更新输入 schema 与说明",
			"• 以可验证的最小增量推进（每步可回归）",
		}
	case "REFACTOR":
		return []string{
			"• 外部行为优先保持不变：小步替换，每步可验证",
			"• 改动前先跑通测试；每次重构后跑回归",
			"• 改函数/类前先做 code_impact，避免漏改调用点",
		}
	case "DESIGN":
		return []string{
			"• 先讨论方案与边界，必要时再输出设计文档",
			"• 不改业务代码（只读/文档化输出）",
		}
	case "RESEARCH":
		return []string{
			"• 以只读方式收集证据：定位入口、梳理链路、给出结论",
			"• 需要历史证据时优先 system_recall/memo",
		}
	case "PERFORMANCE":
		return []string{
			"• 先 profile 再优化：避免凭感觉改",
			"• 优化后必须用基准/指标验证收益",
		}
	case "REFLECT":
		return []string{
			"• 基于历史证据复盘：system_recall + memo + timeline",
			"• 不做未经证据支持的推断",
		}
	default:
		return []string{
			"• 明确边界与验证方式，再开始实施",
		}
	}
}

func getIntentToolStrategy(intent string, hasAnchors bool) []string {
	if !hasAnchors {
		return []string{
			"• 优先使用 project_map 了解项目结构",
			"• 使用 code_search 精确定位代码符号",
		}
	}

	switch intent {
	case "DEBUG":
		return []string{
			"• 已定位代码：用 flow_trace 快速理清主链路与关键分支",
			"• 修复前做 code_impact 评估外溢影响",
			"• 修改代码后务必使用 memo 记录结论与原因",
		}
	case "DEVELOP":
		return []string{
			"• 已定位代码：先 code_impact 看上游/下游再改",
			"• 需要调用链时用 flow_trace（brief/standard）",
			"• 修改代码后务必使用 memo 记录",
		}
	case "REFACTOR":
		return []string{
			"• 已定位代码：先 code_impact 分析影响范围",
			"• 每个重构子步骤后回归测试（go test ./...）",
			"• 修改代码后务必使用 memo 记录",
		}
	default:
		return []string{
			"• 已定位代码，可直接使用 code_impact 分析影响范围",
			"• 修改代码后务必使用 memo 记录",
		}
	}
}

// 辅助逻辑

func determineIntent(desc, explicitIntent string, readOnly bool) string {
	validIntents := map[string]bool{
		"DEBUG": true, "DEVELOP": true, "REFACTOR": true,
		"DESIGN": true, "RESEARCH": true, "PERFORMANCE": true, "REFLECT": true,
	}

	if explicitIntent != "" {
		upper := strings.ToUpper(explicitIntent)
		if validIntents[upper] {
			return upper
		}
	}

	descLower := strings.ToLower(desc)
	if strings.Contains(descLower, "debug") || strings.Contains(descLower, "fix") || strings.Contains(descLower, "修复") || strings.Contains(descLower, "报错") {
		return "DEBUG"
	}
	if strings.Contains(descLower, "refactor") || strings.Contains(descLower, "重构") {
		return "REFACTOR"
	}
	if strings.Contains(descLower, "analy") || strings.Contains(descLower, "分析") || strings.Contains(descLower, "调研") || strings.Contains(descLower, "research") {
		return "RESEARCH"
	}
	if strings.Contains(descLower, "design") || strings.Contains(descLower, "设计") || strings.Contains(descLower, "架构") {
		return "DESIGN"
	}

	if readOnly {
		return "RESEARCH"
	}

	return ""
}

func buildGuardrails(intent string, readOnly bool) Guardrails {
	g := Guardrails{
		Critical: []string{},
		Advisory: []string{"最小变更，不做大爆炸重构"},
	}

	if readOnly {
		g.Critical = append(g.Critical, "READ_ONLY: 严禁修改任何文件")
	}

	switch intent {
	case "DESIGN":
		g.Critical = append(g.Critical, "NO_CODE_EDIT: 严禁编辑业务代码", "MD_ONLY: 仅允许创建 .md 文档")
	case "RESEARCH":
		if !readOnly {
			g.Critical = append(g.Critical, "READ_ONLY: 严禁修改任何文件")
		}
	case "DEBUG":
		g.Critical = append(g.Critical, "VERIFY_FIRST: 修改前必须先定位根因", "NO_BLIND_REWRITE: 禁止盲目重写整个文件")
	case "PERFORMANCE":
		g.Critical = append(g.Critical, "PROFILE_FIRST: 修改前必须先执行性能分析定位瓶颈", "MEASURE_AFTER: 优化后必须用基准测试验证性能提升")
	case "REFACTOR":
		g.Advisory = append(g.Advisory, "INCREMENTAL: 小步快跑，每步可验证", "VERIFY_EACH_STEP: 每次修改后运行测试确认未破坏功能")
	case "REFLECT":
		g.Critical = append(g.Critical, "READ_ONLY: 严禁修改任何文件", "EVIDENCE_BASED: 所有结论必须基于 memo/system_recall 的历史证据")
	}

	return g
}

func generateAlerts(desc, intent string, readOnly bool) []string {
	var alerts []string

	if !readOnly && (strings.Contains(desc, "修改") || strings.Contains(desc, "update") || strings.Contains(desc, "change")) {
		alerts = append(alerts, "Modification detected. Call code_impact(symbol_name=...) first.")
	}

	if strings.Contains(desc, "migrate") || strings.Contains(desc, "迁移") || strings.Contains(desc, "升级") {
		alerts = append(alerts, "🔒 **约束建议**: 技术栈变更。建议添加约束规则,禁止使用旧技术栈的API或模式。")
	}

	// 新功能开发调研提醒
	newFeatureKeywords := []string{"开发", "新增", "添加", "implement", "create", "feature", "module"}
	isNewFeature := false
	matchCount := 0
	descLower := strings.ToLower(desc)
	for _, k := range newFeatureKeywords {
		if strings.Contains(descLower, k) {
			matchCount++
		}
	}
	if matchCount >= 1 && !readOnly {
		isNewFeature = true
	}

	if isNewFeature {
		alerts = append(alerts, "[技术调研提醒]: 开发新组件前，请先执行技术调研。使用 search_web 搜索现有库/方案，避免重复造轮子。")
	}

	return alerts
}

func getComplexityLevel(score float64) string {
	if score >= 70 {
		return "High"
	}
	if score >= 30 {
		return "Medium"
	}
	return "Low"
}

func getIntentHint(intent string) string {
	switch intent {
	case "DEBUG":
		return "🔧 定位根因 → 验证修复。可构建/复用项目专用debug环境，可搜索"
	case "DEVELOP":
		return "🚀 明确修改点 → 最小变更。优先找成熟库，可搜索"
	case "REFACTOR":
		return "♻️ 小步快跑，每步可验证。重构前先跑通测试。分析代码语义"
	case "DESIGN":
		return "📐 先讨论方案，有必要再输出设计文档。不动代码"
	case "RESEARCH":
		return "🔍 可退一步全局思考，可复盘，可用顺序思考工具"
	case "PERFORMANCE":
		return "⚡ 先执行性能分析定位瓶颈 → 针对性优化 → 基准测试验证提升"
	case "REFLECT":
		return "🪞 系统性回顾历史决策。可用 system_recall 检索记忆，open_timeline 查看演进，基于事实得出结论"
	default:
		return "📋 自行决定最佳方案"
	}
}

func wrapSaveFact(sm *SessionManager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if sm.Memory == nil {
			return mcp.NewToolResultError("记忆层尚未初始化，请先执行 initialize_project。"), nil
		}

		var args FactArgs
		if err := request.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("参数格式错误: %v", err)), nil
		}

		id, err := sm.Memory.SaveFact(ctx, args.Type, args.Summarize)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("保存事实失败: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("✅ 事实已存入数据库 (ID: %d): [%s] %s", id, args.Type, args.Summarize)), nil
	}
}
