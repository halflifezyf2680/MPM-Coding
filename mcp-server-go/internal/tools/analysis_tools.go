package tools

import (
	"context"
	"fmt"
	"mcp-server-go/internal/core"
	"mcp-server-go/internal/services"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// ImpactArgs 影响分析参数
type ImpactArgs struct {
	SymbolName string `json:"symbol_name" jsonschema:"required,description=要分析的符号名 (函数名或类名)"`
	Direction  string `json:"direction" jsonschema:"default=backward,enum=backward,enum=forward,enum=both,description=分析方向"`
}

// ProjectMapArgs 项目地图参数
type ProjectMapArgs struct {
	Scope     string `json:"scope" jsonschema:"description=限定范围 (目录或文件路径，留空=整个项目)"`
	Level     string `json:"level" jsonschema:"default=symbols,enum=structure,enum=symbols,description=视图层级"`
	CorePaths string `json:"core_paths" jsonschema:"description=核心目录列表 (JSON 数组字符串)"`
}

// FlowTraceArgs 业务流程追踪参数
type FlowTraceArgs struct {
	SymbolName string `json:"symbol_name" jsonschema:"description=入口符号名（函数/类，与 file_path 二选一；若同时提供则优先 symbol_name）"`
	FilePath   string `json:"file_path" jsonschema:"description=目标文件路径（与 symbol_name 二选一）"`
	Scope      string `json:"scope" jsonschema:"description=限定范围（目录，超大仓库建议必填）"`
	Direction  string `json:"direction" jsonschema:"default=both,enum=backward,enum=forward,enum=both,description=追踪方向"`
	Mode       string `json:"mode" jsonschema:"default=brief,enum=brief,enum=standard,enum=deep,description=输出层级（brief/standard/deep）"`
	MaxNodes   int    `json:"max_nodes" jsonschema:"default=40,description=输出节点上限"`
}

// ModuleMapArgs 模块画像参数
type ModuleMapArgs struct {
	Scope       string `json:"scope" jsonschema:"required,description=模块范围（目录或文件路径）"`
	EntrySymbol string `json:"entry_symbol" jsonschema:"description=可选入口符号；不填时自动挑选候选入口"`
	Mode        string `json:"mode" jsonschema:"default=standard,enum=brief,enum=standard,enum=deep,description=输出层级（brief/standard/deep）"`
	MaxEntries  int    `json:"max_entries" jsonschema:"default=3,description=最多分析的候选入口数"`
}

// RegisterAnalysisTools 注册分析类工具
func RegisterAnalysisTools(s *server.MCPServer, sm *SessionManager, ai *services.ASTIndexer) {
	s.AddTool(mcp.NewTool("code_impact",
		mcp.WithDescription(`code_impact - 代码修改影响分析

用途：
  分析修改函数或类时的影响范围，识别需要同步修改的位置

参数：
  symbol_name (必填)
    要分析的符号名（函数名或类名）
    注意：必须是精确的代码符号，不支持字符串搜索
  
  direction (默认: backward)
    - backward: 谁调用了我（影响上游）
    - forward: 我调用了谁（影响下游）
    - both: 双向分析

返回：
  - 风险等级（low/medium/high）
  - 直接调用者列表（前10个）
  - 间接调用者数量
  - 修改检查清单

示例：
  code_impact(symbol_name="Login", direction="backward")
    -> 分析谁在调用 Login 函数

触发词：
  "mpm 影响", "mpm 依赖", "mpm impact"`),
		mcp.WithInputSchema[ImpactArgs](),
	), wrapImpact(sm, ai))

	s.AddTool(mcp.NewTool("project_map",
		mcp.WithDescription(`project_map - 你的项目导航仪 (当不知道代码在哪时)

用途：
  【宏观视角】当你迷路了，或者不知道该改哪个文件时，用我。我会给你一张带导航的地图。

决策指南：
  level (默认: symbols)
    - 刚接手/想看架构？ -> "structure" (只看目录树，不看代码)
    - 找代码/准备修改？ -> "symbols" (列出更详细的函数/类)
  
  scope (可选)
    如果不填，默认看整个项目（可能会很长）。建议填入你感兴趣的目录。

返回：
  一张 ASCII 格式的项目地图 + 复杂度热力图。

触发词：
  "mpm 地图", "mpm 结构", "mpm map"`),
		mcp.WithInputSchema[ProjectMapArgs](),
	), wrapProjectMap(sm, ai))

	s.AddTool(mcp.NewTool("module_map",
		mcp.WithDescription(`module_map - 模块级业务画像

用途：
	  给一个目录或文件范围生成“这块区域在系统里是怎么运作的”画像，帮助 LLM 先建立模块心智模型，再继续 Read。

规则：
	  - scope 必填，填模块目录或文件路径
	  - entry_symbol 可选；不填时自动挑选候选入口
	  - 输出重点是模块定位、主流程、关键入口、测试锚点和修改入口，不是纯目录树

输出：
	  - 模块定位
	  - 核心组成
	  - 主流程步骤
	  - 副作用/修改入口
	  - 测试锚点与阅读建议

		触发词：
		  - mpm 模块
		  - mpm module`),
		mcp.WithInputSchema[ModuleMapArgs](),
	), wrapModuleMap(sm, ai))

	s.AddTool(mcp.NewTool("flow_trace",
		mcp.WithDescription(`flow_trace - 业务流程追踪（文件/函数）

用途：
	  给 LLM 建立代码阅读主链：先定位入口，再看上游触发，再看下游依赖，按关键节点顺序继续 Read，减少直接通读整文件时的遗漏和误判。

规则：
	  - symbol_name / file_path 二选一
	  - symbol_name 只填函数/类名；文件名或文件基名不要填 symbol_name
	  - 不确定符号名时先用 code_search
	  - 拿到 flow 结果后，优先 Read 入口文件和测试锚点，不要直接整文件硬读猜逻辑

输出：
	  - 入口
	  - 上游摘要
	  - 下游摘要
	  - 关键节点
	  - 下一步阅读建议

		触发词：
		  - mpm 流程
		  - mpm flow`),
		mcp.WithInputSchema[FlowTraceArgs](),
	), wrapFlowTrace(sm, ai))
}

type flowTraceSnapshot struct {
	Node        *services.Node
	Forward     *services.ImpactResult
	Backward    *services.ImpactResult
	Direction   string
	Score       float64
	NodeKind    string
	ExternalIn  int
	ExternalOut int
	InternalIn  int
	InternalOut int
	SideEffects []string
	Stages      []string
}

func normalizeFlowMode(mode string) string {
	m := strings.ToLower(strings.TrimSpace(mode))
	switch m {
	case "brief", "standard", "deep":
		return m
	default:
		return "brief"
	}
}

func flowNodeKind(nodeType string) string {
	t := strings.ToLower(strings.TrimSpace(nodeType))
	if t == "" {
		return "callable"
	}
	callableTypes := map[string]bool{
		"function":  true,
		"method":    true,
		"func":      true,
		"procedure": true,
		"lambda":    true,
	}
	typeTypes := map[string]bool{
		"class":     true,
		"struct":    true,
		"interface": true,
		"enum":      true,
		"type":      true,
	}
	if callableTypes[t] {
		return "callable"
	}
	if typeTypes[t] {
		return "type"
	}
	if strings.Contains(t, "module") || strings.Contains(t, "package") || strings.Contains(t, "namespace") {
		return "module"
	}
	return "other"
}

func flowKindPriority(kind string) int {
	switch kind {
	case "callable":
		return 0
	case "type":
		return 1
	case "module":
		return 2
	default:
		return 3
	}
}

func buildCriticalPaths(entry string, upNames []string, downNames []string, limit int) []string {
	if limit <= 0 {
		limit = 3
	}
	paths := make([]string, 0, limit)
	seen := make(map[string]bool)

	push := func(path string) {
		p := strings.TrimSpace(path)
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		paths = append(paths, p)
	}

	if len(upNames) > 0 && len(downNames) > 0 {
		push(fmt.Sprintf("%s -> %s -> %s", upNames[0], entry, downNames[0]))
	}
	for _, up := range upNames {
		push(fmt.Sprintf("%s -> %s", up, entry))
		if len(paths) >= limit {
			break
		}
	}
	for _, down := range downNames {
		push(fmt.Sprintf("%s -> %s", entry, down))
		if len(paths) >= limit {
			break
		}
	}

	if len(paths) > limit {
		return paths[:limit]
	}
	return paths
}

func impactDirectCount(r *services.ImpactResult) int {
	if r == nil {
		return 0
	}
	return len(r.DirectCallers)
}

func impactIndirectCount(r *services.ImpactResult) int {
	if r == nil {
		return 0
	}
	return len(r.IndirectCallers)
}

func callerNames(items []services.CallerInfo, limit int) []string {
	out := make([]string, 0, limit)
	for _, c := range pickCallers(items, limit) {
		name := c.Node.Name
		if strings.TrimSpace(name) == "" {
			name = c.Node.QualifiedName
		}
		if strings.TrimSpace(name) == "" {
			name = c.Node.ID
		}
		if strings.TrimSpace(name) == "" {
			continue
		}
		out = append(out, name)
	}
	return out
}

func mergeUniqueStrings(items ...[]string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0)
	for _, arr := range items {
		for _, s := range arr {
			v := strings.TrimSpace(s)
			if v == "" || seen[v] {
				continue
			}
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func detectSideEffects(node *services.Node, related []services.CallerInfo) []string {
	if node == nil {
		return nil
	}

	// ========== 333（证据优先）：Phase 1 - 收集调用证据 ==========
	callEvidence := make(map[string]bool)
	normalizeCall := func(callee string) string {
		// 规整化：转小写、去前缀路径（取最后一部分）
		parts := strings.FieldsFunc(strings.ToLower(callee), func(r rune) bool {
			return r == '.' || r == '/' || r == ':'
		})
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
		return strings.ToLower(callee)
	}

	// 从 node.Calls 收集
	for _, callee := range node.Calls {
		callEvidence[normalizeCall(callee)] = true
	}
	// 从 related 节点的 Calls 收集
	for _, r := range related {
		for _, callee := range r.Node.Calls {
			callEvidence[normalizeCall(callee)] = true
		}
	}

	// ========== Phase 2 - 证据匹配（优先级高于启发式）==========
	evidenceScores := map[string]int{
		"filesystem": 0,
		"database":   0,
		"network":    0,
		"process":    0,
		"state":      0,
	}

	// 证据匹配规则（强信号，得分高）
	for callee := range callEvidence {
		// filesystem evidence
		if strings.Contains(callee, "readfile") || strings.Contains(callee, "writefile") ||
			strings.Contains(callee, "openfile") || strings.Contains(callee, "create") ||
			strings.Contains(callee, "mkdir") || strings.Contains(callee, "remove") ||
			strings.Contains(callee, "rename") || strings.Contains(callee, "stat") ||
			strings.Contains(callee, "chmod") || strings.Contains(callee, "chown") ||
			strings.Contains(callee, "os.open") || strings.Contains(callee, "ioutil") {
			evidenceScores["filesystem"] += 10
		}

		// database evidence（注意避免 process 的 Exec）
		if strings.Contains(callee, "query") || strings.Contains(callee, "begin") ||
			strings.Contains(callee, "commit") || strings.Contains(callee, "rollback") ||
			strings.Contains(callee, "insert") || strings.Contains(callee, "sqltransaction") ||
			strings.Contains(callee, "db.exec") || strings.Contains(callee, "db.query") ||
			strings.Contains(callee, "stmt.exec") || strings.Contains(callee, "stmt.query") {
			evidenceScores["database"] += 10
		}

		// network evidence
		if strings.Contains(callee, "listen") || strings.Contains(callee, "dial") ||
			strings.Contains(callee, "serve") || strings.Contains(callee, "request") ||
			strings.Contains(callee, "response") || strings.Contains(callee, "http") ||
			strings.Contains(callee, "grpc") || strings.Contains(callee, "websocket") ||
			strings.Contains(callee, "connect") || strings.Contains(callee, "net.dial") {
			evidenceScores["network"] += 10
		}

		// process evidence（避免与 DB 的 Exec 冲突，要求更强证据）
		if strings.Contains(callee, "command") || strings.Contains(callee, "startprocess") ||
			strings.Contains(callee, "spawn") || strings.Contains(callee, "fork") ||
			strings.Contains(callee, "kill") || strings.Contains(callee, "wait") ||
			strings.Contains(callee, "exec.command") || strings.Contains(callee, "os.exec") {
			evidenceScores["process"] += 10
		}

		// state evidence（设更高阈值，避免泛化）
		if strings.Contains(callee, "lock") || strings.Contains(callee, "unlock") ||
			strings.Contains(callee, "mutex") || strings.Contains(callee, "atomic") ||
			strings.Contains(callee, "cache") || strings.Contains(callee, "session") {
			evidenceScores["state"] += 8
		}
	}

	// 检查是否有证据命中
	hasEvidence := false
	for _, score := range evidenceScores {
		if score >= 10 {
			hasEvidence = true
			break
		}
	}

	// ========== Phase 3 - 返回结果（仅 evidence，奥卡姆剃刀：无证据则返回空）==========
	if !hasEvidence {
		return nil
	}

	types := make([]string, 0, 5)
	if evidenceScores["filesystem"] >= 10 {
		types = append(types, "filesystem[evidence]")
	}
	if evidenceScores["database"] >= 10 {
		types = append(types, "database[evidence]")
	}
	if evidenceScores["network"] >= 10 {
		types = append(types, "network[evidence]")
	}
	if evidenceScores["process"] >= 10 {
		types = append(types, "process[evidence]")
	}
	if evidenceScores["state"] >= 10 {
		types = append(types, "state[evidence]")
	}
	return mergeUniqueStrings(types)
}

func detectStages(node *services.Node, related []services.CallerInfo) []string {
	if node == nil {
		return nil
	}
	bags := []string{node.Name, node.QualifiedName}
	for _, c := range related {
		bags = append(bags, c.Node.Name, c.Node.QualifiedName)
	}
	joined := strings.ToLower(strings.Join(bags, " "))

	stages := make([]string, 0, 6)
	if strings.Contains(joined, "init") || strings.Contains(joined, "setup") || strings.Contains(joined, "new") || strings.Contains(joined, "bootstrap") || strings.Contains(joined, "load") {
		stages = append(stages, "init")
	}
	if strings.Contains(joined, "validate") || strings.Contains(joined, "check") || strings.Contains(joined, "verify") || strings.Contains(joined, "guard") {
		stages = append(stages, "validate")
	}
	if strings.Contains(joined, "run") || strings.Contains(joined, "process") || strings.Contains(joined, "handle") || strings.Contains(joined, "execute") || strings.Contains(joined, "build") || strings.Contains(joined, "index") {
		stages = append(stages, "execute")
	}
	if strings.Contains(joined, "query") || strings.Contains(joined, "search") || strings.Contains(joined, "map") || strings.Contains(joined, "trace") || strings.Contains(joined, "analyze") {
		stages = append(stages, "query")
	}
	if strings.Contains(joined, "save") || strings.Contains(joined, "write") || strings.Contains(joined, "insert") || strings.Contains(joined, "commit") || strings.Contains(joined, "persist") {
		stages = append(stages, "persist")
	}
	return mergeUniqueStrings(stages)
}

func pickCallers(items []services.CallerInfo, limit int) []services.CallerInfo {
	if limit <= 0 {
		limit = 10
	}
	seen := make(map[string]bool)
	out := make([]services.CallerInfo, 0, limit)
	for _, c := range items {
		id := c.Node.ID
		if id == "" {
			id = c.Node.QualifiedName
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, c)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func buildFlowSnapshot(ai *services.ASTIndexer, projectRoot string, node *services.Node, direction string) (*flowTraceSnapshot, error) {
	if node == nil {
		return nil, fmt.Errorf("入口符号为空")
	}
	query := node.QualifiedName
	if query == "" {
		query = node.Name
	}

	s := &flowTraceSnapshot{Node: node, Direction: direction, NodeKind: flowNodeKind(node.NodeType)}
	needForward := direction == "forward" || direction == "both"
	needBackward := direction == "backward" || direction == "both"

	if needForward {
		forward, err := ai.Analyze(projectRoot, query, "forward")
		if err != nil {
			return nil, err
		}
		s.Forward = forward
	}
	if needBackward {
		backward, err := ai.Analyze(projectRoot, query, "backward")
		if err != nil {
			return nil, err
		}
		s.Backward = backward
	}

	forwardDirect := 0
	forwardIndirect := 0
	backwardDirect := 0
	backwardIndirect := 0
	complexity := 0.0

	if s.Forward != nil {
		forwardDirect = len(s.Forward.DirectCallers)
		forwardIndirect = len(s.Forward.IndirectCallers)
		complexity = s.Forward.ComplexityScore
	}
	if s.Backward != nil {
		backwardDirect = len(s.Backward.DirectCallers)
		backwardIndirect = len(s.Backward.IndirectCallers)
		if complexity == 0 {
			complexity = s.Backward.ComplexityScore
		}
	}

	if s.Backward != nil {
		for _, c := range s.Backward.DirectCallers {
			if strings.TrimSpace(c.Node.FilePath) != "" && c.Node.FilePath != node.FilePath {
				s.ExternalIn++
			} else {
				s.InternalIn++
			}
		}
	}
	if s.Forward != nil {
		for _, c := range s.Forward.DirectCallers {
			if strings.TrimSpace(c.Node.FilePath) != "" && c.Node.FilePath != node.FilePath {
				s.ExternalOut++
			} else {
				s.InternalOut++
			}
		}
	}

	if complexity > 40 {
		complexity = 40
	}
	s.Score = float64(
		s.ExternalIn*50+
			s.ExternalOut+
			backwardDirect*8+
			backwardIndirect*2+
			forwardDirect*2+
			forwardIndirect,
	) + complexity/8.0
	related := make([]services.CallerInfo, 0)
	if s.Forward != nil {
		related = append(related, pickCallers(s.Forward.DirectCallers, 8)...)
	}
	if s.Backward != nil {
		related = append(related, pickCallers(s.Backward.DirectCallers, 8)...)
	}
	s.SideEffects = detectSideEffects(node, related)
	s.Stages = detectStages(node, related)

	return s, nil
}

func wrapFlowTrace(sm *SessionManager, ai *services.ASTIndexer) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		_ = ctx
		var args FlowTraceArgs
		if err := request.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("参数错误: %v", err)), nil
		}

		if sm.ProjectRoot == "" {
			return mcp.NewToolResultError("项目未初始化，请先执行 initialize_project"), nil
		}

		// 强校验：只允许填一个入口字段（exactly one of）
		hasSymbol := strings.TrimSpace(args.SymbolName) != ""
		hasFile := strings.TrimSpace(args.FilePath) != ""
		if !hasSymbol && !hasFile {
			return mcp.NewToolResultError("❌ 参数错误：必须提供 symbol_name 或 file_path（二选一）\n\n" +
				"提示：\n" +
				"- 想追踪函数/类的调用链？使用 symbol_name=\"函数名\"\n" +
				"- 想追踪文件内的流程？使用 file_path=\"相对路径\""), nil
		}
		if hasSymbol && hasFile {
			return mcp.NewToolResultError("❌ 参数错误：symbol_name 和 file_path 不能同时提供（二选一）\n\n" +
				"提示：\n" +
				"- 如果目标是符号（函数/类），只填 symbol_name\n" +
				"- 如果目标是文件，只填 file_path"), nil
		}

		// 检查 minLength 约束（防止空字符串）
		if hasSymbol && len(strings.TrimSpace(args.SymbolName)) == 0 {
			return mcp.NewToolResultError("❌ 参数错误：symbol_name 不能为空字符串"), nil
		}
		if hasFile && len(strings.TrimSpace(args.FilePath)) == 0 {
			return mcp.NewToolResultError("❌ 参数错误：file_path 不能为空字符串"), nil
		}

		direction := strings.ToLower(strings.TrimSpace(args.Direction))
		if direction == "" {
			direction = "both"
		}
		if direction != "backward" && direction != "forward" && direction != "both" {
			direction = "both"
		}

		mode := normalizeFlowMode(args.Mode)

		maxNodes := args.MaxNodes
		if maxNodes <= 0 {
			maxNodes = 40
		}
		if maxNodes > 120 {
			maxNodes = 120
		}

		var snapshots []*flowTraceSnapshot
		allSnapshots := 0

		if strings.TrimSpace(args.SymbolName) != "" {
			searchResult, err := ai.SearchSymbolWithScope(sm.ProjectRoot, args.SymbolName, args.Scope)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("symbol 定位失败: %v", err)), nil
			}
			if searchResult == nil || searchResult.FoundSymbol == nil {
				// 构造友好的错误提示
				var errMsg strings.Builder
				errMsg.WriteString(fmt.Sprintf("❌ 未找到符号: `%s`\n\n", args.SymbolName))

				// 如果有候选，列出 top 5
				if searchResult != nil && len(searchResult.Candidates) > 0 {
					errMsg.WriteString("**您是不是想找以下符号？**\n\n")
					limit := 5
					if len(searchResult.Candidates) < limit {
						limit = len(searchResult.Candidates)
					}
					for i := 0; i < limit; i++ {
						c := searchResult.Candidates[i]
						name := c.Node.Name
						if name == "" {
							name = c.Node.QualifiedName
						}
						errMsg.WriteString(fmt.Sprintf("%d. `%s` @ %s:%d (%s, score=%.2f)\n",
							i+1, name, c.Node.FilePath, c.Node.LineStart, c.MatchType, c.Score))
					}
					errMsg.WriteString("\n")
				}

				// 明确提示用户
				errMsg.WriteString("**建议**:\n")
				errMsg.WriteString("- 如果您的本意是**文件**，请改用 `file_path` 参数\n")
				errMsg.WriteString("- 如果您的本意是**符号**，请先用 `code_search` 确认精确的符号名\n")
				errMsg.WriteString("- 如果不确定，可以尝试 `code_search(query=\"关键词\")` 进行模糊搜索")

				return mcp.NewToolResultError(errMsg.String()), nil
			}
			snap, err := buildFlowSnapshot(ai, sm.ProjectRoot, searchResult.FoundSymbol, direction)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("flow_trace 失败: %v", err)), nil
			}
			snapshots = append(snapshots, snap)
		} else {
			// file mode
			_, _ = ai.IndexScope(sm.ProjectRoot, args.FilePath)
			mapResult, err := ai.MapProjectWithScope(sm.ProjectRoot, "symbols", args.FilePath)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("文件符号提取失败: %v", err)), nil
			}
			if mapResult == nil || len(mapResult.Structure) == 0 {
				return mcp.NewToolResultError(fmt.Sprintf("文件无可追踪符号: %s", args.FilePath)), nil
			}

			primaryNodes := make([]services.Node, 0)
			secondaryNodes := make([]services.Node, 0)
			for _, list := range mapResult.Structure {
				for _, n := range list {
					kind := flowNodeKind(n.NodeType)
					if kind == "callable" {
						primaryNodes = append(primaryNodes, n)
					} else if kind == "type" || kind == "module" {
						secondaryNodes = append(secondaryNodes, n)
					}
				}
			}

			nodes := primaryNodes
			if len(nodes) == 0 {
				nodes = secondaryNodes
			}
			if len(nodes) == 0 {
				return mcp.NewToolResultError(fmt.Sprintf("文件中无函数/类符号: %s", args.FilePath)), nil
			}
			sort.Slice(nodes, func(i, j int) bool {
				ki := flowKindPriority(flowNodeKind(nodes[i].NodeType))
				kj := flowKindPriority(flowNodeKind(nodes[j].NodeType))
				if ki != kj {
					return ki < kj
				}
				if nodes[i].LineStart == nodes[j].LineStart {
					return nodes[i].Name < nodes[j].Name
				}
				return nodes[i].LineStart < nodes[j].LineStart
			})

			candidateLimit := 8
			if mode == "deep" {
				candidateLimit = 12
			} else if mode == "brief" {
				candidateLimit = 6
			}
			if len(nodes) < candidateLimit {
				candidateLimit = len(nodes)
			}
			for i := 0; i < candidateLimit; i++ {
				n := nodes[i]
				node := n
				snap, err := buildFlowSnapshot(ai, sm.ProjectRoot, &node, direction)
				if err == nil {
					snapshots = append(snapshots, snap)
				}
			}
			allSnapshots = len(snapshots)
			sort.Slice(snapshots, func(i, j int) bool {
				if snapshots[i].ExternalIn != snapshots[j].ExternalIn {
					return snapshots[i].ExternalIn > snapshots[j].ExternalIn
				}
				bi := impactDirectCount(snapshots[i].Backward)
				bj := impactDirectCount(snapshots[j].Backward)
				if bi != bj {
					return bi > bj
				}
				ii := impactIndirectCount(snapshots[i].Backward)
				ij := impactIndirectCount(snapshots[j].Backward)
				if ii != ij {
					return ii > ij
				}
				if snapshots[i].Score == snapshots[j].Score {
					return snapshots[i].Node.LineStart < snapshots[j].Node.LineStart
				}
				return snapshots[i].Score > snapshots[j].Score
			})

			keep := 2
			if mode == "brief" {
				keep = 1
			} else if mode == "deep" {
				keep = 4
			}
			if len(snapshots) > keep {
				snapshots = snapshots[:keep]
			}

			if len(snapshots) == 0 {
				return mcp.NewToolResultError(fmt.Sprintf("文件流程追踪失败: %s", args.FilePath)), nil
			}
		}

		var sb strings.Builder
		sb.WriteString("### 🔄 业务流程追踪\n\n")
		sb.WriteString(fmt.Sprintf("**模式**: %s | **视图**: %s | **方向**: %s\n\n", func() string {
			if strings.TrimSpace(args.SymbolName) != "" {
				return "symbol"
			}
			return "file"
		}(), mode, direction))

		shownNodes := 0
		omitted := 0

		for _, snap := range snapshots {
			n := snap.Node
			sb.WriteString(fmt.Sprintf("#### 入口 `%s`\n", n.Name))
			sb.WriteString(fmt.Sprintf("- 类型: `%s` | 位置: `%s:%d` | score=%.1f\n", snap.NodeKind, n.FilePath, n.LineStart, snap.Score))
			sb.WriteString(fmt.Sprintf("- 跨文件连接: inbound=%d, outbound=%d\n", snap.ExternalIn, snap.ExternalOut))

			upNamesPreview := make([]string, 0)
			downNamesPreview := make([]string, 0)

			if snap.Backward != nil {
				upLimit := maxNodes / 4
				if upLimit < 2 {
					upLimit = 2
				}
				if mode == "deep" {
					upLimit = maxNodes / 3
				}
				upDirect := pickCallers(snap.Backward.DirectCallers, upLimit)
				upIndirect := pickCallers(snap.Backward.IndirectCallers, upLimit)
				sb.WriteString(fmt.Sprintf("- 上游影响: direct=%d, indirect=%d, risk=%s\n", len(upDirect), len(upIndirect), snap.Backward.RiskLevel))
				if len(upDirect) > 0 && mode != "brief" {
					sb.WriteString("- 上游关键节点: ")
					names := callerNames(upDirect, upLimit)
					upNamesPreview = names
					for i, name := range names {
						if i > 0 {
							sb.WriteString(" -> ")
						}
						sb.WriteString(fmt.Sprintf("`%s`", name))
					}
					sb.WriteString("\n")
				}
				shownNodes += len(upDirect) + len(upIndirect)
				if len(snap.Backward.DirectCallers) > len(upDirect) {
					omitted += len(snap.Backward.DirectCallers) - len(upDirect)
				}
				if len(snap.Backward.IndirectCallers) > len(upIndirect) {
					omitted += len(snap.Backward.IndirectCallers) - len(upIndirect)
				}
			}

			if snap.Forward != nil {
				downLimit := maxNodes / 4
				if downLimit < 2 {
					downLimit = 2
				}
				if mode == "deep" {
					downLimit = maxNodes / 3
				}
				downDirect := pickCallers(snap.Forward.DirectCallers, downLimit)
				downIndirect := pickCallers(snap.Forward.IndirectCallers, downLimit)
				sb.WriteString(fmt.Sprintf("- 下游依赖: direct=%d, indirect=%d, complexity=%.1f\n", len(downDirect), len(downIndirect), snap.Forward.ComplexityScore))
				if len(downDirect) > 0 {
					sb.WriteString("- 下游关键节点: ")
					names := callerNames(downDirect, downLimit)
					downNamesPreview = names
					for i, name := range names {
						if i > 0 {
							sb.WriteString(" -> ")
						}
						sb.WriteString(fmt.Sprintf("`%s`", name))
					}
					sb.WriteString("\n")
				}
				shownNodes += len(downDirect) + len(downIndirect)
				if len(snap.Forward.DirectCallers) > len(downDirect) {
					omitted += len(snap.Forward.DirectCallers) - len(downDirect)
				}
				if len(snap.Forward.IndirectCallers) > len(downIndirect) {
					omitted += len(snap.Forward.IndirectCallers) - len(downIndirect)
				}
			}

			if mode != "brief" {
				critical := buildCriticalPaths(n.Name, upNamesPreview, downNamesPreview, 3)
				if len(critical) > 0 {
					sb.WriteString("- 关键路径Top3:\n")
					for i, p := range critical {
						sb.WriteString(fmt.Sprintf("  %d) `%s`\n", i+1, p))
					}
				}
				if len(snap.Stages) > 0 {
					sb.WriteString(fmt.Sprintf("- 阶段摘要: %s\n", strings.Join(snap.Stages, " -> ")))
				}
				if len(snap.SideEffects) > 0 {
					sb.WriteString(fmt.Sprintf("- 副作用: %s\n", strings.Join(snap.SideEffects, ", ")))
				}
			}

			sb.WriteString("\n")
		}

		sb.WriteString("**建议**:\n")
		sb.WriteString("- 若要精确改动风险，用 `code_impact(symbol_name=入口函数, direction=backward)` 二次确认。\n")
		sb.WriteString("- 若输出仍偏长，请缩小 `scope` 到单目录或单文件。\n")
		sb.WriteString("- 若需更多细节，将 `mode` 提升为 `standard` 或 `deep`。\n")

		if allSnapshots > len(snapshots) {
			sb.WriteString(fmt.Sprintf("\n_注：文件模式下候选入口较多，已从 %d 个中展示 %d 个高分入口。_\n", allSnapshots, len(snapshots)))
		}
		if omitted > 0 || shownNodes > maxNodes {
			sb.WriteString(fmt.Sprintf("_注：已按输出预算截断，省略约 %d 个节点（max_nodes=%d）。_\n", omitted, maxNodes))
		}

		return mcp.NewToolResultText(sb.String()), nil
	}
}

func wrapImpact(sm *SessionManager, ai *services.ASTIndexer) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args ImpactArgs
		if err := request.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("参数格式错误: %v", err)), nil
		}

		if sm.ProjectRoot == "" {
			return mcp.NewToolResultError("项目尚未初始化，请先执行 initialize_project。"), nil
		}

		// 默认方向
		if args.Direction == "" {
			args.Direction = "backward"
		}

		// 1. AST 静态分析 (硬调用)
		astResult, err := ai.Analyze(sm.ProjectRoot, args.SymbolName, args.Direction)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("AST 分析失败: %v", err)), nil
		}

		if astResult == nil || astResult.Status != "success" {
			errorMessage := fmt.Sprintf("⚠️ `%s` 不是代码函数/类定义。\n\n", args.SymbolName)
			errorMessage += "> 如果要搜索**字符串**，用 **Grep** 工具\n"
			errorMessage += "> 如果要查找**函数定义**，用 **code_search** 工具"
			return mcp.NewToolResultText(errorMessage), nil
		}

		// 2. 精简输出 (面向 LLM 决策)
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("## `%s` 影响分析\n\n", args.SymbolName))
		sb.WriteString(fmt.Sprintf("**风险**: %s | **复杂度**: %.0f | **影响节点**: %d\n\n",
			astResult.RiskLevel, astResult.ComplexityScore, astResult.AffectedNodes))

		// 直接调用者列表
		if len(astResult.DirectCallers) > 0 {
			sb.WriteString("### 直接调用者（修改前必须检查）\n")
			limit := 10
			if len(astResult.DirectCallers) < limit {
				limit = len(astResult.DirectCallers)
			}
			for i := 0; i < limit; i++ {
				c := astResult.DirectCallers[i]
				sb.WriteString(fmt.Sprintf("- `%s` @ %s:%d\n", c.Node.Name, c.Node.FilePath, c.Node.LineStart))
			}
			if len(astResult.DirectCallers) > limit {
				sb.WriteString(fmt.Sprintf("- ... 还有 %d 个\n", len(astResult.DirectCallers)-limit))
			}
		} else {
			sb.WriteString("✅ 无直接调用者，可安全修改\n")
		}

		// 间接调用总数
		if len(astResult.IndirectCallers) > 0 {
			sb.WriteString(fmt.Sprintf("\n_间接影响: %d 个函数_\n", len(astResult.IndirectCallers)))
		}

		// JSON：直接调用者 + 间接调用者（按距离，前20个）
		sb.WriteString("\n```json\n")
		sb.WriteString(fmt.Sprintf(`{"risk":"%s","direct_count":%d,"indirect_count":%d,"callers":[`,
			astResult.RiskLevel, len(astResult.DirectCallers), len(astResult.IndirectCallers)))

		// 直接调用者
		for i, c := range astResult.DirectCallers {
			if i >= 10 {
				break
			}
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString(fmt.Sprintf(`"%s"`, c.Node.Name))
		}

		// 间接调用者（前20个，BFS已按距离排序）
		indirectLimit := 20
		if len(astResult.IndirectCallers) < indirectLimit {
			indirectLimit = len(astResult.IndirectCallers)
		}
		for i := 0; i < indirectLimit; i++ {
			c := astResult.IndirectCallers[i]
			if i > 0 || len(astResult.DirectCallers) > 0 {
				sb.WriteString(",")
			}
			sb.WriteString(fmt.Sprintf(`"%s"`, c.Node.Name))
		}

		sb.WriteString("]}\n```\n")

		return mcp.NewToolResultText(sb.String()), nil
	}
}

func wrapProjectMap(sm *SessionManager, ai *services.ASTIndexer) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args ProjectMapArgs
		if err := request.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("参数错误: %v", err)), nil
		}

		if sm.ProjectRoot == "" {
			return mcp.NewToolResultError("项目未初始化，请先执行 initialize_project"), nil
		}

		level := args.Level
		if level == "" {
			level = "symbols"
		}

		if level == "structure" {
			// 结构视图走 Rust structure 模式，不触发全量符号索引，避免超大 JSON
			structureResult, err := ai.StructureProjectWithScope(sm.ProjectRoot, args.Scope)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("生成结构地图失败: %v", err)), nil
			}

			type dirCount struct {
				Path  string
				Count int
			}
			dirs := make([]dirCount, 0, len(structureResult.Structure))
			for p, info := range structureResult.Structure {
				dirs = append(dirs, dirCount{Path: p, Count: info.FileCount})
			}
			sort.Slice(dirs, func(i, j int) bool {
				if dirs[i].Count == dirs[j].Count {
					return dirs[i].Path < dirs[j].Path
				}
				return dirs[i].Count > dirs[j].Count
			})

			var sb strings.Builder
			sb.WriteString("### 🗺️ 项目地图 (Structure)\n\n")
			sb.WriteString(fmt.Sprintf("**📊 统计**: %d 文件 | %d 目录\n\n", structureResult.TotalFiles, len(dirs)))
			if strings.TrimSpace(args.Scope) != "" {
				sb.WriteString(fmt.Sprintf("**🔎 Scope**: `%s`\n\n", args.Scope))
			}
			sb.WriteString("**📁 目录结构** (按文件数排序):\n")

			limit := 120
			if len(dirs) < limit {
				limit = len(dirs)
			}
			for i := 0; i < limit; i++ {
				path := dirs[i].Path
				if path == "" {
					path = "(root)"
				}
				sb.WriteString(fmt.Sprintf("- `%s/` (%d files)\n", path, dirs[i].Count))
			}
			if len(dirs) > limit {
				sb.WriteString(fmt.Sprintf("\n... 其余 %d 个目录已省略，请使用 scope 下钻。\n", len(dirs)-limit))
			}

			content := sb.String()
			if len(content) > 2000 {
				mpmDataDir := filepath.Join(sm.ProjectRoot, core.DataDirName)
				_ = os.MkdirAll(mpmDataDir, 0755)
				outputPath := filepath.Join(mpmDataDir, "project_map_structure.md")
				if err := os.WriteFile(outputPath, []byte(content), 0644); err == nil {
					return mcp.NewToolResultText(fmt.Sprintf("⚠️ Map 内容较长 (%d chars)，已自动保存到项目文件：\n👉 `%s`\n\n请使用 view_file 查看。", len(content), outputPath)), nil
				}
			}

			return mcp.NewToolResultText(content), nil
		}

		// symbols 视图：优先按范围补录（热点目录），否则按新鲜度检查全量索引
		if strings.TrimSpace(args.Scope) != "" {
			_, _ = ai.IndexScope(sm.ProjectRoot, args.Scope)
		} else {
			_, _ = ai.EnsureFreshIndex(sm.ProjectRoot)
		}

		// 调用 AST 服务生成数据
		// 注意：如果 scope 为空，底层会自动处理为整个项目
		result, err := ai.MapProjectWithScope(sm.ProjectRoot, level, args.Scope)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("生成地图失败: %v", err)), nil
		}

		// 🆕 收集所有符号名并分析复杂度
		var symbolNames []string
		for _, nodes := range result.Structure {
			for _, node := range nodes {
				// 只分析函数、方法和类
				if node.NodeType == "function" || node.NodeType == "method" || node.NodeType == "class" {
					symbolNames = append(symbolNames, node.Name)
				}
			}
		}

		// 调用复杂度分析
		if len(symbolNames) > 0 {
			complexityReport, err := ai.AnalyzeComplexity(sm.ProjectRoot, symbolNames)
			if err == nil && complexityReport != nil {
				// 构建复杂度映射
				result.ComplexityMap = make(map[string]float64)
				for _, risk := range complexityReport.HighRiskSymbols {
					result.ComplexityMap[risk.SymbolName] = risk.Score
				}
			}
		}

		// 使用 MapRenderer 渲染结果
		mr := NewMapRenderer(result, sm.ProjectRoot)

		content := mr.RenderStandard()

		// 🆕 主动接管大输出：如果 > 2000 字符，保存到文件
		if len(content) > 2000 {
			mpmDataDir := filepath.Join(sm.ProjectRoot, core.DataDirName)
			_ = os.MkdirAll(mpmDataDir, 0755)

			// 按模式固定命名，每次直接覆盖（不保留历史版本）
			filename := fmt.Sprintf("project_map_%s.md", level)
			outputPath := filepath.Join(mpmDataDir, filename)

			if err := os.WriteFile(outputPath, []byte(content), 0644); err == nil {
				return mcp.NewToolResultText(fmt.Sprintf(
					"⚠️ Map 内容较长 (%d chars)，已自动保存到项目文件：\n👉 `%s`\n\n请使用 view_file 查看。",
					len(content), outputPath)), nil
			}
			// 如果保存失败，降级回直接返回
		}

		return mcp.NewToolResultText(content), nil
	}
}

// modulePortrait 模块画像输出结构
type modulePortrait struct {
	ModulePositioning string             // 模块定位
	CoreComponents    []string           // 核心组成
	MainFlowSteps     []string           // 主流程步骤
	TopEntries        []string           // 修改入口
	TestAnchors       []string           // 测试锚点
	ReadSuggestions   []string           // 阅读建议
	Stages            []string           // 启发式阶段标签
	SideEffects       []string           // 副作用标签
	ComplexityScores  map[string]float64 // 复杂度映射
}

// wrapModuleMap 模块画像工具实现
func wrapModuleMap(sm *SessionManager, ai *services.ASTIndexer) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		_ = ctx
		var args ModuleMapArgs
		if err := request.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("参数错误: %v", err)), nil
		}

		if sm.ProjectRoot == "" {
			return mcp.NewToolResultError("项目未初始化，请先执行 initialize_project"), nil
		}

		scope := strings.TrimSpace(args.Scope)
		if scope == "" {
			return mcp.NewToolResultError("❌ scope 参数必填，请指定模块目录或文件路径"), nil
		}

		mode := normalizeFlowMode(args.Mode)
		maxEntries := args.MaxEntries
		if maxEntries <= 0 {
			maxEntries = 3
		}
		if maxEntries > 10 {
			maxEntries = 10
		}

		// 1. 用 MapProjectWithScope 收集范围内的符号
		result, err := ai.MapProjectWithScope(sm.ProjectRoot, "symbols", scope)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("获取模块符号失败: %v", err)), nil
		}

		// 2. 收集所有 callable 节点作为候选入口
		var candidateNodes []*services.Node
		for _, nodes := range result.Structure {
			for i := range nodes {
				if flowNodeKind(nodes[i].NodeType) == "callable" {
					candidateNodes = append(candidateNodes, &nodes[i])
				}
			}
		}

		if len(candidateNodes) == 0 {
			// 如果没有 callable，返回简单结构
			return mcp.NewToolResultText(renderSimpleModulePortrait(scope, result, mode)), nil
		}

		// 3. 如果指定了 entry_symbol，优先用它
		var selectedNodes []*services.Node
		entrySymbol := strings.TrimSpace(args.EntrySymbol)
		if entrySymbol != "" {
			// 搜索指定的入口符号
			searchResult, err := ai.SearchSymbolWithScope(sm.ProjectRoot, entrySymbol, scope)
			if err == nil && searchResult.FoundSymbol != nil {
				selectedNodes = append(selectedNodes, searchResult.FoundSymbol)
			}
		}

		// 4. 如果未指定或搜索失败，自动挑选高分候选入口
		if len(selectedNodes) == 0 {
			// 对每个候选构建快照并评分
			type scoredNode struct {
				Node  *services.Node
				Score float64
			}
			var scoredNodes []scoredNode

			for _, node := range candidateNodes {
				// 快速评分：使用启发式（不调用完整的 buildFlowSnapshot 以节省性能）
				score := scoreNodeQuick(node)
				scoredNodes = append(scoredNodes, scoredNode{Node: node, Score: score})
			}

			// 按分数排序
			sort.Slice(scoredNodes, func(i, j int) bool {
				return scoredNodes[i].Score > scoredNodes[j].Score
			})

			// 取前 maxEntries 个
			for i := 0; i < len(scoredNodes) && i < maxEntries; i++ {
				selectedNodes = append(selectedNodes, scoredNodes[i].Node)
			}
		}

		// 5. 对选中的入口调用 buildFlowSnapshot（获取详细信息）
		var snapshots []*flowTraceSnapshot
		allStages := make(map[string]bool)
		allSideEffects := make(map[string]bool)

		for _, node := range selectedNodes {
			snapshot, err := buildFlowSnapshot(ai, sm.ProjectRoot, node, "both")
			if err != nil {
				continue
			}
			snapshots = append(snapshots, snapshot)

			// 收集所有阶段和副作用
			for _, stage := range snapshot.Stages {
				allStages[stage] = true
			}
			for _, se := range snapshot.SideEffects {
				allSideEffects[se] = true
			}
		}

		// 6. 构建模块画像
		portrait := buildModulePortrait(scope, result, snapshots, allStages, allSideEffects, mode)

		// 7. 渲染输出
		content := renderModulePortrait(portrait, mode)

		// 8. 如果输出太长，保存到文件
		if len(content) > 2000 {
			mpmDataDir := filepath.Join(sm.ProjectRoot, core.DataDirName)
			_ = os.MkdirAll(mpmDataDir, 0755)
			outputPath := filepath.Join(mpmDataDir, "module_map.md")
			if err := os.WriteFile(outputPath, []byte(content), 0644); err == nil {
				return mcp.NewToolResultText(fmt.Sprintf(
					"⚠️ Module Map 内容较长 (%d chars)，已保存到：\n👉 `%s`\n\n请使用 view_file 查看。",
					len(content), outputPath)), nil
			}
		}

		return mcp.NewToolResultText(content), nil
	}
}

// scoreNodeQuick 快速评分节点（启发式）
// 核心原则：目录级 scope 强力排斥技术性入口，优先选择编排点/业务入口
func scoreNodeQuick(node *services.Node) float64 {
	score := 0.0
	name := strings.ToLower(node.Name)
	fileName := strings.ToLower(filepath.Base(node.FilePath))

	// ========== 1. 强降权：技术性/测试/辅助函数 ==========
	// 1.1 纯 init 函数 - 几乎永远不是 LLM 该先读的入口（除非是 main/setup 文件）
	if name == "init" {
		if strings.Contains(fileName, "main") || strings.Contains(fileName, "setup") {
			score -= 10 // 关键文件中的 init 保留一些价值
		} else {
			score -= 100 // 其他文件中的 init 强力降权
		}
	}

	// 1.2 测试/基准函数 - 不应是主入口
	if strings.HasPrefix(node.Name, "Test") || strings.HasPrefix(node.Name, "Benchmark") {
		score -= 120
	}

	// 1.3 纯渲染/格式化/字符串辅助
	if strings.Contains(name, "render") || strings.Contains(name, "format") ||
		strings.Contains(name, "tostring") || strings.Contains(name, "stringify") ||
		strings.Contains(name, "print") || strings.Contains(name, "dump") {
		score -= 80
	}

	// 1.4 纯验证/检查辅助（非主处理器）
	if (strings.Contains(name, "validate") || strings.Contains(name, "check") ||
		strings.Contains(name, "verify") || strings.Contains(name, "guard")) &&
		!strings.Contains(name, "handler") {
		score -= 40
	}

	// 1.5 private helper（小写开头）+ 含有 helper/internal/private 字样
	// 但 wrap* 函数例外（它们通常是 MCP 工具入口，虽然私有但高价值）
	isWrapFunction := strings.Contains(name, "wrap") || strings.Contains(name, "wrapper")
	if len(node.Name) > 0 && node.Name[0] >= 'a' && node.Name[0] <= 'z' {
		if isWrapFunction {
			// wrap* 私有函数不降权，它们是 MCP 工具核心入口
		} else if strings.Contains(name, "helper") || strings.Contains(name, "internal") ||
			strings.Contains(name, "private") || strings.Contains(name, "util") {
			score -= 50
		} else {
			// 普通 private 函数也适度降权
			score -= 15
		}
	}

	// 1.6 getter/setter 风格
	if strings.HasPrefix(name, "get") || strings.HasPrefix(name, "set") ||
		strings.HasPrefix(name, "is") || strings.HasPrefix(name, "has") {
		if len(node.Name) < 15 { // 短小的 getter/setter
			score -= 30
		}
	}

	// ========== 2. 强提权：编排点/业务入口 ==========
	// 2.1 注册/编排函数（目录级最重要的入口）
	if strings.Contains(name, "register") {
		score += 100 // Register* 是目录级最强入口信号
	}
	if strings.Contains(name, "setup") || strings.Contains(name, "bootstrap") {
		score += 80
	}

	// 2.2 创建/工厂函数（通常是模块入口）
	if strings.HasPrefix(node.Name, "New") || strings.HasPrefix(node.Name, "Create") {
		score += 70
	}

	// 2.3 处理器/执行器（核心业务逻辑）
	if strings.Contains(name, "handle") || strings.Contains(name, "handler") {
		score += 65
	}
	if strings.Contains(name, "process") || strings.Contains(name, "execute") {
		score += 55
	}

	// 2.4 wrap/wrapper（通常是高层接口）
	if strings.Contains(name, "wrap") || strings.Contains(name, "wrapper") {
		score += 60
	}

	// 2.5 serve/run/start（服务入口）
	if strings.Contains(name, "serve") || strings.Contains(name, "run") ||
		strings.Contains(name, "start") {
		score += 50
	}

	// 2.6 检测/分析/构建函数（核心业务能力）
	if strings.Contains(name, "detect") || strings.Contains(name, "analyze") ||
		strings.Contains(name, "build") || strings.Contains(name, "index") {
		score += 55
	}

	// 2.7 加载/读取/查询函数（数据获取入口）
	if strings.Contains(name, "load") || strings.Contains(name, "fetch") ||
		strings.Contains(name, "query") || strings.Contains(name, "search") {
		score += 45
	}

	// 2.8 确保/验证函数（有状态变更的入口）
	if strings.Contains(name, "ensure") || strings.Contains(name, "refresh") {
		score += 40
	}

	// ========== 3. 文件重要性加权 ==========
	// 3.1 registry / register 文件（最强信号）
	if strings.Contains(fileName, "register") || strings.Contains(fileName, "registry") {
		score += 40
	}
	// 3.2 manager / service / handler 文件
	if strings.Contains(fileName, "manager") || strings.Contains(fileName, "service") {
		score += 30
	}
	if strings.Contains(fileName, "handler") || strings.Contains(fileName, "controller") {
		score += 25
	}
	// 3.3 core / engine / processor 文件
	if strings.Contains(fileName, "core") || strings.Contains(fileName, "engine") ||
		strings.Contains(fileName, "processor") {
		score += 20
	}

	// ========== 4. 调用关系分析（编排点识别）==========
	callCount := len(node.Calls)
	if callCount > 0 {
		// 4.1 跨文件调用（编排点的核心特征）
		crossFileCalls := 0
		nodeDir := filepath.Dir(node.FilePath)
		for _, callID := range node.Calls {
			if !strings.Contains(callID, nodeDir) {
				crossFileCalls++
			}
		}
		// 跨文件调用权重更高
		score += float64(crossFileCalls * 12)

		// 4.2 总调用数（适度加权）
		if callCount > 10 {
			score += float64(callCount * 4) // 高调用数更可能是编排点
		} else {
			score += float64(callCount * 2)
		}
	}

	// ========== 5. 确保非负 ==========
	if score < 0 {
		score = 0
	}

	return score
}

// buildModulePortrait 构建模块画像
func buildModulePortrait(scope string, result *services.MapResult, snapshots []*flowTraceSnapshot, allStages map[string]bool, allSideEffects map[string]bool, mode string) *modulePortrait {
	portrait := &modulePortrait{
		CoreComponents:   make([]string, 0),
		MainFlowSteps:    make([]string, 0),
		TopEntries:       make([]string, 0),
		TestAnchors:      make([]string, 0),
		ReadSuggestions:  make([]string, 0),
		Stages:           make([]string, 0),
		SideEffects:      make([]string, 0),
		ComplexityScores: make(map[string]float64),
	}

	// 1. 模块定位：基于 scope 名称 + 高分入口 + 阶段
	portrait.ModulePositioning = inferModulePositioning(scope, snapshots, allStages)

	// 2. 核心组成：关键文件/符号/类型
	portrait.CoreComponents = extractCoreComponents(result, snapshots)

	// 3. 主流程步骤：来自高分入口的 stages + 下游关键节点
	portrait.MainFlowSteps = inferMainFlowSteps(snapshots)

	// 4. 修改入口：高分入口 + 复杂度高节点
	portrait.TopEntries = extractTopEntries(snapshots, result)

	// 5. 测试锚点：在 scope 或邻近目录中寻找
	portrait.TestAnchors = findTestAnchors(result)

	// 6. 阅读建议：先读哪几个文件/入口
	portrait.ReadSuggestions = generateReadSuggestions(snapshots, result)

	// 7. 阶段标签
	for stage := range allStages {
		portrait.Stages = append(portrait.Stages, stage)
	}

	// 8. 副作用标签
	for se := range allSideEffects {
		portrait.SideEffects = append(portrait.SideEffects, se)
	}

	return portrait
}

// inferModulePositioning 推断模块定位
// 优化：结合目录名称 + 高分入口角色 + 核心职责
func inferModulePositioning(scope string, snapshots []*flowTraceSnapshot, stages map[string]bool) string {
	parts := strings.Split(scope, "/")
	scopeName := parts[len(parts)-1]
	if scopeName == "" || scopeName == "." {
		scopeName = "root"
	}

	// 1. 基于目录路径推断业务领域
	positioning := inferBusinessDomain(scopeName, scope)

	// 2. 基于高分入口补充职责描述
	if len(snapshots) > 0 && snapshots[0].Node != nil {
		topNode := snapshots[0].Node
		topScore := snapshots[0].Score

		// 2.1 如果高分入口评分足够高，它是核心入口
		if topScore > 60 {
			role := describeNodeRole(topNode.Name)
			positioning += fmt.Sprintf("\n🎯 **核心入口**: `%s` (%s)", topNode.Name, role)

			// 2.2 补充下游关键职责（如果有高价值下游）
			if snapshots[0].Forward != nil && len(snapshots[0].Forward.DirectCallers) > 0 {
				var keyDownstream []string
				for _, caller := range snapshots[0].Forward.DirectCallers {
					if isHighValueNode(caller.Node.Name) && len(keyDownstream) < 2 {
						role := describeNodeRole(caller.Node.Name)
						keyDownstream = append(keyDownstream, fmt.Sprintf("`%s`(%s)", caller.Node.Name, role))
					}
				}
				if len(keyDownstream) > 0 {
					positioning += fmt.Sprintf("\n└─ 编排: %s", strings.Join(keyDownstream, " → "))
				}
			}
		}

		// 2.3 如果有副作用，补充关键职责
		if len(snapshots[0].SideEffects) > 0 {
			sideEffectDesc := describeSideEffectsRole(snapshots[0].SideEffects)
			if sideEffectDesc != "" {
				positioning += fmt.Sprintf("\n⚡ **核心职责**: %s", sideEffectDesc)
			}
		}
	}

	// 3. 补充阶段特征（仅在有明确阶段且有助于理解时）
	stageList := make([]string, 0, len(stages))
	for s := range stages {
		stageList = append(stageList, s)
	}
	if len(stageList) > 0 && len(stageList) <= 2 {
		// 只在阶段标签较少时显示，且转换为更友好的描述
		stageDesc := describeStagesBrief(stageList)
		if stageDesc != "" {
			positioning += fmt.Sprintf("\n🔄 **处理阶段**: %s", stageDesc)
		}
	}

	return positioning
}

// describeSideEffectsRole 将副作用转换为职责描述
func describeSideEffectsRole(sideEffects []string) string {
	var roles []string
	for _, se := range sideEffects {
		// 去掉 [evidence] 后缀
		seClean := strings.TrimSuffix(se, "[evidence]")
		switch seClean {
		case "filesystem":
			roles = append(roles, "文件读写")
		case "database":
			roles = append(roles, "数据持久化")
		case "network":
			roles = append(roles, "网络通信")
		case "process":
			roles = append(roles, "进程管理")
		case "state":
			roles = append(roles, "状态管理")
		}
	}
	if len(roles) > 0 {
		return strings.Join(roles, " / ")
	}
	return ""
}

// describeStagesBrief 简洁描述阶段
func describeStagesBrief(stages []string) string {
	var descs []string
	for _, stage := range stages {
		switch stage {
		case "init":
			descs = append(descs, "初始化")
		case "validate":
			descs = append(descs, "验证")
		case "execute":
			descs = append(descs, "执行")
		case "query":
			descs = append(descs, "查询")
		case "persist":
			descs = append(descs, "持久化")
		default:
			descs = append(descs, stage)
		}
	}
	return strings.Join(descs, " → ")
}

// inferBusinessDomain 基于目录名称和符号特征推断业务领域
// 优化：更精准的业务职责描述，结合符号特征
func inferBusinessDomain(scopeName, scopePath string) string {
	scopeLower := strings.ToLower(scopeName)
	scopePathLower := strings.ToLower(scopePath)

	// 基于完整路径的精准推断（优先级最高）
	switch {
	// === tools 相关 ===
	case strings.Contains(scopePathLower, "/tools") || strings.Contains(scopePathLower, "\\tools"):
		if strings.Contains(scopePathLower, "analysis") {
			return "🔬 **analysis_tools**: AST 分析与代码洞察工具 — 提供 flow_trace / impact / module_map 等深度分析能力"
		}
		if strings.Contains(scopePathLower, "memory") {
			return "💾 **memory_tools**: 项目记忆与备忘工具 — memo / known_facts 等知识存储"
		}
		if strings.Contains(scopePathLower, "search") {
			return "🔍 **search_tools**: 符号搜索与定位工具 — code_search 等代码查找能力"
		}
		if strings.Contains(scopePathLower, "task") {
			return "🔗 **task_tools**: 任务链与状态机工具 — task_chain 等任务编排能力"
		}
		if strings.Contains(scopePathLower, "doc") {
			return "📄 **doc_tools**: 文档生成与模板工具 — 自动化文档能力"
		}
		if strings.Contains(scopePathLower, "system") {
			return "⚙️ **system_tools**: 系统级工具 — hook / persona / timeline 等系统功能"
		}
		if strings.Contains(scopePathLower, "intelligence") {
			return "🧠 **intelligence_tools**: 智能分析工具 — known_facts 等经验存储"
		}
		if strings.Contains(scopePathLower, "enhance") {
			return "🚀 **enhance_tools**: 增强功能工具 — 外部服务集成能力"
		}
		// 通用 tools 目录 — MCP 工具注册入口与请求分发
		return "🛠️ **tools**: MCP 工具注册层 — Register* 函数注册各分区工具，wrap* 处理器执行具体业务"

	// === core 相关 ===
	case strings.Contains(scopePathLower, "/core") || strings.Contains(scopePathLower, "\\core"):
		if strings.Contains(scopePathLower, "database") || strings.Contains(scopePathLower, "db") {
			return "🗄️ **database**: 数据库连接与持久化 — SQLite 连接池、事务管理"
		}
		if strings.Contains(scopePathLower, "memory") || strings.Contains(scopePathLower, "memo") {
			return "🧠 **memory**: 记忆层 (SSOT) — 项目演进记录、memo/known_facts 存储"
		}
		if strings.Contains(scopePathLower, "detector") || strings.Contains(scopePathLower, "detect") {
			return "🔍 **detector**: 项目检测与配置 — 技术栈识别、项目根检测、配置管理"
		}
		if strings.Contains(scopePathLower, "model") || strings.Contains(scopePathLower, "types") {
			return "📊 **models**: 核心数据模型 — Task/Memo/Config 等业务实体定义"
		}
		// 通用 core 目录（目录级）— 底层服务 / 持久化 / 检测 / 状态管理
		return "⚙️ **core**: 底层基础设施 — 数据持久化(memory/database)、项目检测(detector)、核心模型(models)"

	// === services 相关 ===
	case strings.Contains(scopePathLower, "/services") || strings.Contains(scopePathLower, "\\services"):
		if strings.Contains(scopePathLower, "ast") || strings.Contains(scopePathLower, "index") {
			return "🌳 **ast_indexer**: AST 索引服务 — 代码解析、符号提取、调用图构建"
		}
		if strings.Contains(scopePathLower, "search") || strings.Contains(scopePathLower, "engine") {
			return "🔎 **search_engine**: 搜索引擎 — 符号搜索、模糊匹配、范围过滤"
		}
		return "🔧 **services**: 服务层 — 封装业务逻辑、提供可复用的功能模块"

	// === 其他常见目录模式 ===
	case strings.Contains(scopePathLower, "/handlers") || strings.Contains(scopePathLower, "\\handlers"):
		return "🎯 **handlers**: 请求处理器集合 — 处理各类业务请求并协调下游服务"

	case strings.Contains(scopePathLower, "/api") || strings.Contains(scopePathLower, "\\api"):
		return "🌐 **api**: API 接口层 — 暴露外部接口、处理协议转换"

	case strings.Contains(scopePathLower, "/models") || strings.Contains(scopePathLower, "\\models"):
		return "📊 **models**: 数据模型层 — 定义数据结构、类型和业务实体"

	case strings.Contains(scopePathLower, "/utils") || strings.Contains(scopePathLower, "\\utils"):
		return "🛠️ **utils**: 工具函数集 — 提供通用辅助函数和工具方法"

	case strings.Contains(scopePathLower, "/test") || strings.Contains(scopePathLower, "\\test") ||
		strings.Contains(scopePathLower, "_test"):
		return "✅ **test**: 测试区域 — 包含单元测试、集成测试和测试辅助代码"

	case scopeLower == "internal":
		return "🔒 **internal**: 内部实现 — 不对外暴露的内部包和模块"

	case scopeLower == "pkg":
		return "📚 **pkg**: 公共包 — 可被外部项目引用的公共库"

	case scopeLower == "cmd":
		return "🚀 **cmd**: 命令行入口 — 包含各种可执行程序的入口点"

	case scopeLower == "config" || scopeLower == "configs":
		return "⚙️ **config**: 配置管理 — 配置文件解析、环境变量处理"

	// === 基于目录名称的通用推断 ===
	default:
		// 尝试从目录名称推断
		if strings.Contains(scopeLower, "handler") {
			return fmt.Sprintf("🎯 **%s**: 请求处理区域 — 处理具体业务请求", scopeName)
		}
		if strings.Contains(scopeLower, "manager") {
			return fmt.Sprintf("👔 **%s**: 管理器区域 — 状态与生命周期管理", scopeName)
		}
		if strings.Contains(scopeLower, "service") {
			return fmt.Sprintf("🔧 **%s**: 服务区域 — 封装可复用业务逻辑", scopeName)
		}
		if strings.Contains(scopeLower, "store") || strings.Contains(scopeLower, "repo") {
			return fmt.Sprintf("💾 **%s**: 存储区域 — 数据持久化与查询", scopeName)
		}
		if strings.Contains(scopeLower, "cache") {
			return fmt.Sprintf("⚡ **%s**: 缓存区域 — 数据缓存与快速访问", scopeName)
		}
		// 默认
		return fmt.Sprintf("📁 **%s** 区域", scopeName)
	}
}

// extractCoreComponents 提取核心组成
// 优化：减少机械重复，优先显示高价值文件
func extractCoreComponents(result *services.MapResult, snapshots []*flowTraceSnapshot) []string {
	components := make([]string, 0)

	// 1. 识别高价值文件（通过符号数量和复杂度）
	type fileInfo struct {
		Path          string
		SymbolCount   int
		MaxComplexity float64
		Importance    float64
	}
	fileMap := make(map[string]*fileInfo)

	for path, nodes := range result.Structure {
		fi := &fileInfo{
			Path:        path,
			SymbolCount: len(nodes),
		}
		// 计算文件重要性
		fileName := strings.ToLower(filepath.Base(path))
		if strings.Contains(fileName, "register") || strings.Contains(fileName, "registry") {
			fi.Importance += 30
		}
		if strings.Contains(fileName, "handler") || strings.Contains(fileName, "manager") {
			fi.Importance += 25
		}
		if strings.Contains(fileName, "service") || strings.Contains(fileName, "core") {
			fi.Importance += 20
		}
		if strings.Contains(fileName, "main") || strings.Contains(fileName, "entry") {
			fi.Importance += 15
		}

		// 累加符号数作为基础重要性
		fi.Importance += float64(len(nodes)) * 2

		// 累加最大复杂度
		if result.ComplexityMap != nil {
			for _, node := range nodes {
				if score, ok := result.ComplexityMap[node.Name]; ok && score > fi.MaxComplexity {
					fi.MaxComplexity = score
				}
			}
		}

		fileMap[path] = fi
	}

	// 2. 排序文件（按重要性降序）
	files := make([]*fileInfo, 0, len(fileMap))
	for _, fi := range fileMap {
		files = append(files, fi)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Importance > files[j].Importance
	})

	// 3. 取前 3-5 个核心文件
	limit := 5
	if len(files) < limit {
		limit = len(files)
	}
	for i := 0; i < limit; i++ {
		fi := files[i]
		desc := fmt.Sprintf("📄 `%s` (%d 符号)", filepath.Base(fi.Path), fi.SymbolCount)
		if fi.MaxComplexity > 20 {
			desc += fmt.Sprintf(" [复杂度: %.0f]", fi.MaxComplexity)
		}
		components = append(components, desc)
	}

	// 4. 补充高分入口符号（前 2 个）
	for i, snapshot := range snapshots {
		if i >= 2 {
			break
		}
		if snapshot.Node != nil && snapshot.Score > 40 {
			role := describeNodeRole(snapshot.Node.Name)
			components = append(components, fmt.Sprintf("🔹 `%s` (%s, 评分: %.0f)",
				snapshot.Node.Name, role, snapshot.Score))
		}
	}

	return components
}

// inferMainFlowSteps 推断主流程步骤
// 优化：结合入口函数 + 关键调用点 + 副作用，生成更像"阅读顺序"的步骤
// 目标：输出像 LLM 下一步真实阅读顺序，而非机械模板
func inferMainFlowSteps(snapshots []*flowTraceSnapshot) []string {
	steps := make([]string, 0)

	if len(snapshots) == 0 {
		return steps
	}

	// 取第一个快照作为主入口
	topSnapshot := snapshots[0]
	if topSnapshot.Node == nil {
		return steps
	}

	entryName := topSnapshot.Node.Name
	entryFile := filepath.Base(topSnapshot.Node.FilePath)

	// ========== Step 1: 入口函数定位 ==========
	// 更自然的描述，突出"从这里开始读"
	step1 := fmt.Sprintf("👉 入口 `%s` @ %s", entryName, entryFile)
	steps = append(steps, step1)

	// ========== Step 2: 关键下游调用（阅读顺序）==========
	// 提取入口函数内部调用的关键函数
	if topSnapshot.Forward != nil && len(topSnapshot.Forward.DirectCallers) > 0 {
		type downNode struct {
			name  string
			file  string
			value int
		}
		var downNodes []downNode

		for _, caller := range topSnapshot.Forward.DirectCallers {
			name := caller.Node.Name
			// 计算节点价值
			value := 0
			if isHighValueNode(name) {
				value += 10
			}
			nameLower := strings.ToLower(name)
			if strings.Contains(nameLower, "process") ||
				strings.Contains(nameLower, "execute") ||
				strings.Contains(nameLower, "build") ||
				strings.Contains(nameLower, "render") {
				value += 5
			}
			// 排除低价值节点
			if value >= 5 {
				downNodes = append(downNodes, downNode{
					name:  name,
					file:  filepath.Base(caller.Node.FilePath),
					value: value,
				})
			}
		}

		// 按价值排序
		sort.Slice(downNodes, func(i, j int) bool {
			return downNodes[i].value > downNodes[j].value
		})

		// 取前 3 个作为关键调用点，用 → 连接
		var keyDownstreams []string
		for i, dn := range downNodes {
			if i >= 3 {
				break
			}
			if dn.file != entryFile {
				keyDownstreams = append(keyDownstreams, fmt.Sprintf("`%s`(%s)", dn.name, dn.file))
			} else {
				keyDownstreams = append(keyDownstreams, fmt.Sprintf("`%s`", dn.name))
			}
		}
		if len(keyDownstreams) > 0 {
			steps = append(steps, fmt.Sprintf("⬇ 调用: %s", strings.Join(keyDownstreams, " → ")))
		}
	}

	// ========== Step 3: 副作用/输出点 ==========
	if len(topSnapshot.SideEffects) > 0 {
		var seDescs []string
		for _, se := range topSnapshot.SideEffects {
			seClean := strings.TrimSuffix(se, "[evidence]")
			switch seClean {
			case "filesystem":
				seDescs = append(seDescs, "文件读写")
			case "database":
				seDescs = append(seDescs, "DB 操作")
			case "network":
				seDescs = append(seDescs, "网络请求")
			case "process":
				seDescs = append(seDescs, "子进程")
			case "state":
				seDescs = append(seDescs, "状态变更")
			}
		}
		if len(seDescs) > 0 {
			steps = append(steps, fmt.Sprintf("⚡ 副作用: %s", strings.Join(seDescs, " / ")))
		}
	}

	// ========== Step 4: 关键上游依赖（了解调用来源）==========
	if topSnapshot.Backward != nil && len(topSnapshot.Backward.DirectCallers) > 0 {
		// 找最有价值的上游（通常 1-2 个）
		var upstreams []string
		for _, caller := range topSnapshot.Backward.DirectCallers {
			if isHighValueNode(caller.Node.Name) && len(upstreams) < 2 {
				upFile := filepath.Base(caller.Node.FilePath)
				upstreams = append(upstreams, fmt.Sprintf("`%s`(%s)", caller.Node.Name, upFile))
			}
		}
		if len(upstreams) > 0 {
			steps = append(steps, fmt.Sprintf("⬆ 被调用: %s", strings.Join(upstreams, ", ")))
		}
	}

	return steps
}

// describeNodeRole 描述节点的业务角色
func describeNodeRole(name string) string {
	nameLower := strings.ToLower(name)

	switch {
	case strings.Contains(nameLower, "register") || strings.Contains(nameLower, "registry"):
		return "注册器"
	case strings.Contains(nameLower, "handler") || strings.Contains(nameLower, "handle"):
		return "处理器"
	case strings.Contains(nameLower, "process") || strings.Contains(nameLower, "execute"):
		return "执行器"
	case strings.Contains(nameLower, "validate") || strings.Contains(nameLower, "check"):
		return "验证器"
	case strings.Contains(nameLower, "render") || strings.Contains(nameLower, "format"):
		return "渲染器"
	case strings.Contains(nameLower, "wrap") || strings.Contains(nameLower, "wrapper"):
		return "包装器"
	case strings.Contains(nameLower, "init") || strings.Contains(nameLower, "setup"):
		return "初始化器"
	case strings.Contains(nameLower, "load") || strings.Contains(nameLower, "read"):
		return "加载器"
	case strings.Contains(nameLower, "save") || strings.Contains(nameLower, "write"):
		return "持久化器"
	default:
		return "业务逻辑"
	}
}

// describeStage 描述阶段
func describeStage(stage string) string {
	stageLower := strings.ToLower(stage)

	switch {
	case strings.Contains(stageLower, "init"):
		return "初始化阶段 — 准备环境和配置"
	case strings.Contains(stageLower, "validate"):
		return "验证阶段 — 检查输入和前置条件"
	case strings.Contains(stageLower, "process"):
		return "处理阶段 — 执行核心业务逻辑"
	case strings.Contains(stageLower, "query"):
		return "查询阶段 — 检索和搜索数据"
	case strings.Contains(stageLower, "render"):
		return "渲染阶段 — 格式化输出结果"
	default:
		return stage
	}
}

// isHighValueNode 判断是否为高价值节点
func isHighValueNode(name string) bool {
	nameLower := strings.ToLower(name)

	// 高价值节点：处理器、注册器、执行器等
	if strings.Contains(nameLower, "handler") || strings.Contains(nameLower, "register") ||
		strings.Contains(nameLower, "process") || strings.Contains(nameLower, "execute") ||
		strings.Contains(nameLower, "manager") || strings.Contains(nameLower, "service") {
		return true
	}

	// 低价值节点：纯 helper / render / util
	if strings.Contains(nameLower, "render") || strings.Contains(nameLower, "format") ||
		strings.Contains(nameLower, "helper") || strings.Contains(nameLower, "util") {
		return false
	}

	return true
}

// extractTopEntries 提取修改入口
func extractTopEntries(snapshots []*flowTraceSnapshot, result *services.MapResult) []string {
	entries := make([]string, 0)

	// 高分入口
	for i, snapshot := range snapshots {
		if i >= 3 {
			break
		}
		if snapshot.Node != nil {
			entry := fmt.Sprintf("🔥 `%s` (评分: %.1f)", snapshot.Node.Name, snapshot.Score)
			entries = append(entries, entry)
		}
	}

	// 如果不足，补充复杂度高的节点
	if len(entries) < 3 && result.ComplexityMap != nil {
		type nodeScore struct {
			Name  string
			Score float64
		}
		var highComplexity []nodeScore
		for name, score := range result.ComplexityMap {
			if score > 20 {
				highComplexity = append(highComplexity, nodeScore{Name: name, Score: score})
			}
		}
		sort.Slice(highComplexity, func(i, j int) bool {
			return highComplexity[i].Score > highComplexity[j].Score
		})
		for i, nc := range highComplexity {
			if i >= 3-len(entries) {
				break
			}
			entries = append(entries, fmt.Sprintf("⚠️ `%s` (复杂度: %.1f)", nc.Name, nc.Score))
		}
	}

	return entries
}

// findTestAnchors 寻找测试锚点
// 优化：适度扩展到邻近目录，不局限于当前 scope
func findTestAnchors(result *services.MapResult) []string {
	anchors := make([]string, 0)

	// 1. 从 result.Structure 中寻找测试文件
	testFiles := make([]string, 0)
	testFunctions := make([]string, 0)

	for path, nodes := range result.Structure {
		fileName := filepath.Base(path)
		// 识别测试文件
		if strings.HasSuffix(fileName, "_test.go") || strings.HasPrefix(fileName, "test_") {
			testFiles = append(testFiles, path)
			// 提取测试函数
			for _, node := range nodes {
				if strings.HasPrefix(node.Name, "Test") {
					testFunctions = append(testFunctions,
						fmt.Sprintf("✅ `%s` @ %s", node.Name, fileName))
				}
			}
		}
	}

	// 2. 返回测试函数（前 5 个）
	if len(testFunctions) > 0 {
		limit := 5
		if len(testFunctions) < limit {
			limit = len(testFunctions)
		}
		for i := 0; i < limit; i++ {
			anchors = append(anchors, testFunctions[i])
		}
		return anchors
	}

	// 3. 如果找到测试文件但没有测试函数，返回文件列表
	if len(testFiles) > 0 {
		limit := 3
		if len(testFiles) < limit {
			limit = len(testFiles)
		}
		for i := 0; i < limit; i++ {
			anchors = append(anchors,
				fmt.Sprintf("📄 测试文件: `%s`", filepath.Base(testFiles[i])))
		}
		return anchors
	}

	// 4. 如果没找到，返回提示（但要更友好）
	anchors = append(anchors,
		"💡 当前 scope 未包含测试文件 — 可尝试查看上级目录或 `*_test.go` 文件")

	return anchors
}

// generateReadSuggestions 生成阅读建议
// 优化：基于入口函数 -> 强关联文件 -> 核心类型 -> 测试锚点 的阅读顺序
func generateReadSuggestions(snapshots []*flowTraceSnapshot, result *services.MapResult) []string {
	suggestions := make([]string, 0)

	if len(snapshots) == 0 {
		suggestions = append(suggestions, "💡 使用 code_search 定位具体函数，使用 flow_trace 追踪业务流程")
		return suggestions
	}

	topSnapshot := snapshots[0]
	if topSnapshot.Node == nil {
		suggestions = append(suggestions, "💡 使用 code_search 定位具体函数，使用 flow_trace 追踪业务流程")
		return suggestions
	}

	entryNode := topSnapshot.Node
	entryFile := entryNode.FilePath
	entryRole := describeNodeRole(entryNode.Name)

	// ========== 1. 主入口（必读）==========
	suggestions = append(suggestions,
		fmt.Sprintf("① **必读**: `%s` (%s) — 从这里开始理解模块核心逻辑",
			entryNode.Name, entryRole))

	// ========== 2. 强关联文件（通过调用关系）==========
	coupledFiles := make(map[string]int)
	coupledSymbols := make(map[string]string) // file -> 代表性符号

	if topSnapshot.Forward != nil {
		for _, caller := range topSnapshot.Forward.DirectCallers {
			file := caller.Node.FilePath
			if file != entryFile {
				coupledFiles[file]++
				if _, exists := coupledSymbols[file]; !exists || isHighValueNode(caller.Node.Name) {
					coupledSymbols[file] = caller.Node.Name
				}
			}
		}
	}

	// 排序并取前 2 个强关联文件
	type fileCouple struct {
		Path   string
		Count  int
		Symbol string
	}
	var coupled []fileCouple
	for path, count := range coupledFiles {
		coupled = append(coupled, fileCouple{
			Path:   path,
			Count:  count,
			Symbol: coupledSymbols[path],
		})
	}
	sort.Slice(coupled, func(i, j int) bool {
		return coupled[i].Count > coupled[j].Count
	})

	for i, fc := range coupled {
		if i >= 2 {
			break
		}
		fileName := filepath.Base(fc.Path)
		if fc.Symbol != "" {
			suggestions = append(suggestions,
				fmt.Sprintf("② **关联**: `%s` (含 `%s`) — 与主入口紧密协作",
					fileName, fc.Symbol))
		} else {
			suggestions = append(suggestions,
				fmt.Sprintf("② **关联**: `%s` — 与主入口紧密协作", fileName))
		}
	}

	// 构建耦合文件 map 用于快速查找
	coupledMap := make(map[string]bool)
	for _, fc := range coupled {
		coupledMap[fc.Path] = true
	}

	// ========== 3. 核心类型/模型文件 ==========
	coreFiles := make([]string, 0)
	for path := range result.Structure {
		fileName := strings.ToLower(filepath.Base(path))
		// 类型定义、状态管理、配置文件（排除已添加的强关联文件）
		if (strings.Contains(fileName, "types") || strings.Contains(fileName, "models") ||
			strings.Contains(fileName, "state") || strings.Contains(fileName, "config") ||
			strings.Contains(fileName, "constants") || strings.Contains(fileName, "entity")) &&
			!coupledMap[path] {
			coreFiles = append(coreFiles, path)
		}
	}
	if len(coreFiles) > 0 {
		sort.Strings(coreFiles)
		limit := 2
		if len(coreFiles) < limit {
			limit = len(coreFiles)
		}
		for i := 0; i < limit; i++ {
			suggestions = append(suggestions,
				fmt.Sprintf("③ **数据模型**: `%s` — 理解数据结构和业务实体",
					filepath.Base(coreFiles[i])))
		}
	}

	// ========== 4. 测试文件（如果有）==========
	testFiles := make([]string, 0)
	for path := range result.Structure {
		fileName := filepath.Base(path)
		if strings.HasSuffix(fileName, "_test.go") || strings.HasPrefix(fileName, "test_") {
			// 优先选择与主入口相关的测试文件
			if strings.Contains(fileName, strings.TrimSuffix(filepath.Base(entryFile), ".go")) {
				testFiles = append([]string{path}, testFiles...)
			} else {
				testFiles = append(testFiles, path)
			}
		}
	}
	if len(testFiles) > 0 && len(suggestions) < 6 {
		suggestions = append(suggestions,
			fmt.Sprintf("④ **测试验证**: `%s` — 通过测试理解预期行为",
				filepath.Base(testFiles[0])))
	}

	// ========== 5. 如果建议太少，补充通用建议 ==========
	if len(suggestions) < 3 {
		suggestions = append(suggestions,
			"💡 **提示**: 使用 `flow_trace(symbol_name=入口函数)` 追踪完整调用链")
	}

	return suggestions
}

// renderSimpleModulePortrait 渲染简单模块画像（无 callable 时）
func renderSimpleModulePortrait(scope string, result *services.MapResult, mode string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("### 📦 Module Map: `%s`\n\n", scope))
	sb.WriteString("#### 📋 模块定位\n")
	sb.WriteString(fmt.Sprintf("该区域包含 %d 个文件，但未检测到可执行入口（可能是纯数据/配置区域）。\n\n", len(result.Structure)))

	sb.WriteString("#### 🧩 核心组成\n")
	for path, nodes := range result.Structure {
		sb.WriteString(fmt.Sprintf("- 📄 `%s` (%d symbols)\n", path, len(nodes)))
	}

	return sb.String()
}

// renderModulePortrait 渲染模块画像
func renderModulePortrait(portrait *modulePortrait, mode string) string {
	var sb strings.Builder

	sb.WriteString("### 📦 Module Map\n\n")

	// 1. 模块定位
	sb.WriteString("#### 📋 模块定位\n")
	sb.WriteString(portrait.ModulePositioning + "\n\n")

	// 2. 核心组成
	if len(portrait.CoreComponents) > 0 {
		sb.WriteString("#### 🧩 核心组成\n")
		for _, comp := range portrait.CoreComponents {
			sb.WriteString(fmt.Sprintf("- %s\n", comp))
		}
		sb.WriteString("\n")
	}

	// 3. 主流程步骤
	if len(portrait.MainFlowSteps) > 0 {
		sb.WriteString("#### 🔄 主流程步骤\n")
		for _, step := range portrait.MainFlowSteps {
			sb.WriteString(fmt.Sprintf("- %s\n", step))
		}
		sb.WriteString("\n")
	}

	// 4. 修改入口
	if len(portrait.TopEntries) > 0 {
		sb.WriteString("#### 🎯 修改入口 (Top Entries)\n")
		for _, entry := range portrait.TopEntries {
			sb.WriteString(fmt.Sprintf("- %s\n", entry))
		}
		sb.WriteString("\n")
	}

	// 5. 测试锚点
	if len(portrait.TestAnchors) > 0 {
		sb.WriteString("#### ✅ 测试锚点\n")
		for _, anchor := range portrait.TestAnchors {
			sb.WriteString(fmt.Sprintf("- %s\n", anchor))
		}
		sb.WriteString("\n")
	}

	// 6. 阅读建议
	if len(portrait.ReadSuggestions) > 0 {
		sb.WriteString("#### 📖 阅读建议\n")
		for _, suggestion := range portrait.ReadSuggestions {
			sb.WriteString(fmt.Sprintf("- %s\n", suggestion))
		}
		sb.WriteString("\n")
	}

	// 7. 阶段 & 副作用标签（仅 standard/deep 模式）
	if mode != "brief" {
		if len(portrait.Stages) > 0 {
			sb.WriteString("#### 🏷️ 阶段标签\n")
			sb.WriteString(fmt.Sprintf("%s\n\n", strings.Join(portrait.Stages, " / ")))
		}
		if len(portrait.SideEffects) > 0 {
			sb.WriteString("#### ⚡ 副作用标签\n")
			sb.WriteString(fmt.Sprintf("%s\n\n", strings.Join(portrait.SideEffects, " / ")))
		}
	}

	return sb.String()
}
