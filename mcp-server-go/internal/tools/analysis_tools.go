package tools

import (
	"context"
	"fmt"
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

	s.AddTool(mcp.NewTool("flow_trace",
		mcp.WithDescription(`flow_trace - 业务流程追踪（文件/函数）

用途：
  用于理解业务逻辑主链路。与 code_impact 不同，它输出可读的“入口-上游-下游”流程摘要。

输入：
  - symbol_name / file_path（二选一）
  - 若两者都提供，优先使用 symbol_name
  - scope（可选，建议在大项目中填写）
  - direction: backward/forward/both（默认 both）
  - mode: brief/standard/deep（默认 brief，渐进披露）
  - max_nodes: 输出节点上限（默认 40）

输出：
  - 入口点
  - 上游调用链摘要
  - 下游依赖链摘要
  - 风险与下一步建议

示例：
  flow_trace(symbol_name="run_indexer", scope="mcp-server-go/internal/services", direction="both")
  flow_trace(file_path="mcp-server-go/internal/tools/analysis_tools.go", direction="forward", max_nodes=30)

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

		if strings.TrimSpace(args.SymbolName) == "" && strings.TrimSpace(args.FilePath) == "" {
			return mcp.NewToolResultError("flow_trace 需要 symbol_name 或 file_path（至少一个）"), nil
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
				return mcp.NewToolResultError(fmt.Sprintf("未找到符号: %s", args.SymbolName)), nil
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
				mcpDataDir := filepath.Join(sm.ProjectRoot, ".mcp-data")
				_ = os.MkdirAll(mcpDataDir, 0755)
				outputPath := filepath.Join(mcpDataDir, "project_map_structure.md")
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
			mcpDataDir := filepath.Join(sm.ProjectRoot, ".mcp-data")
			_ = os.MkdirAll(mcpDataDir, 0755)

			// 按模式固定命名，每次直接覆盖（不保留历史版本）
			filename := fmt.Sprintf("project_map_%s.md", level)
			outputPath := filepath.Join(mcpDataDir, filename)

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
