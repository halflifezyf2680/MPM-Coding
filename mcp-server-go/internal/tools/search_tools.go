package tools

import (
	"context"
	"fmt"
	"mcp-server-go/internal/services"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// SearchArgs 搜索参数
type SearchArgs struct {
	Query      string `json:"query" jsonschema:"required,description=搜索关键词"`
	Scope      string `json:"scope" jsonschema:"description=限定范围"`
	SearchType string `json:"search_type" jsonschema:"default=any,enum=any,enum=function,enum=class,description=符号类型过滤"`
}

// RegisterSearchTools 注册搜索工具
func RegisterSearchTools(s *server.MCPServer, sm *SessionManager, ai *services.ASTIndexer) {

	s.AddTool(mcp.NewTool("code_search",
		mcp.WithDescription(`code_search - 代码符号定位 (比 grep 更懂代码)

用途：
  【精确定位】当你只知道名字（函数名/类名），但不知道它在哪个文件时，别用 grep，用我。

参数速查：
  query       (必填) 符号名，如 "SessionManager"、"handleRequest"
  scope       (可选) 限定目录，如 "internal/core"
  search_type (可选) any|function|class（默认 any）

⚠️ 注意：query 是符号名，不要写自然语言。

调用示例：
  { "query": "SessionManager" }
  { "query": "handleRequest", "scope": "internal/services", "search_type": "function" }

触发词：
  "mpm 搜索", "mpm 定位", "mpm 符号", "mpm find"`),
		mcp.WithInputSchema[SearchArgs](),
	), wrapSearch(sm, ai))
}

func wrapSearch(sm *SessionManager, ai *services.ASTIndexer) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if sm.ProjectRoot == "" {
			return mcp.NewToolResultError("项目尚未初始化，请先执行 initialize_project。"), nil
		}
			sm.ensureFresh()

		var args SearchArgs
		if err := request.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("参数格式错误: %v", err)), nil
		}

		scope, err := normalizeProjectRelativePath(sm.ProjectRoot, args.Scope, "scope")
		if err != nil {
			return mcp.NewToolResultError("❌ " + err.Error()), nil
		}

		var warmupWarning string
		if err := warmIndexForPath(ai, sm.ProjectRoot, scope); err != nil {
			warmupWarning = fmt.Sprintf("⚠️ 索引预热失败，以下结果可能基于旧索引：%v\n\n", err)
		}

		// 1. AST Search (Core Strategy)
		astResult, err := ai.SearchSymbolWithScope(sm.ProjectRoot, args.Query, scope)
		if err != nil {
			// Log error but continue to grep if possible
		}

		// 1.1 Scope Filtering (Client-side enforcement)
		if astResult != nil && astResult.FoundSymbol != nil && scope != "" {
			path := strings.ReplaceAll(astResult.FoundSymbol.FilePath, "\\", "/")
			if !strings.Contains(path, scope) {
				astResult.FoundSymbol = nil
			}
		}

		// 1.5 Type Filtering
		if astResult != nil && args.SearchType != "" && args.SearchType != "any" {
			wantType := args.SearchType // function or class

			// Filter FoundSymbol
			if astResult.FoundSymbol != nil {
				t := astResult.FoundSymbol.NodeType
				match := false
				if wantType == "function" && (t == "function" || t == "method") {
					match = true
				} else if wantType == "class" && (t == "class" || t == "struct" || t == "interface" || t == "component" || t == "template" || t == "layout" || t == "slot" || t == "selector" || t == "keyframes") {
					match = true
				}
				if !match {
					astResult.FoundSymbol = nil
				}
			}

			// Filter Candidates
			var kept []services.CandidateMatch
			for _, c := range astResult.Candidates {
				t := c.Node.NodeType
				match := false
				if wantType == "function" && (t == "function" || t == "method") {
					match = true
				} else if wantType == "class" && (t == "class" || t == "struct" || t == "interface" || t == "component" || t == "template" || t == "layout" || t == "slot" || t == "selector" || t == "keyframes") {
					match = true
				}
				if match {
					kept = append(kept, c)
				}
			}
			astResult.Candidates = kept
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("### 关于「%s」的搜索结果\n\n", args.Query))
		if warmupWarning != "" {
			sb.WriteString(warmupWarning)
		}

		// 2. Decide if Grep is needed
		// Fallback trigger: No Exact Match found in AST (after filtering)
		useGrep := astResult == nil || astResult.FoundSymbol == nil

		// 如果 AST 找到了最佳匹配，展示详情
		if astResult != nil && astResult.FoundSymbol != nil {
			sb.WriteString(fmt.Sprintf("✅ **最佳匹配** (%s):\n", astResult.MatchType))
			node := astResult.FoundSymbol

			// Rust 索引器已存储绝对路径，直接使用
			sb.WriteString(fmt.Sprintf("- **%s** `%s` @ `%s` L%d-%d\n",
				node.NodeType, node.Name, node.FilePath, node.LineStart, node.LineEnd))
			sb.WriteString(fmt.Sprintf("  ID: `%s`\n", node.ID))

			if node.Signature != "" {
				sb.WriteString(fmt.Sprintf("  签名: `%s`\n", node.Signature))
			}

			// 调用关系（它调用的函数）
			if len(node.Calls) > 0 {
				sb.WriteString(fmt.Sprintf("  调用: %d 个函数\n", len(node.Calls)))
				for i, call := range node.Calls {
					if i >= 5 {
						sb.WriteString(fmt.Sprintf("    ... (还有 %d 个)\n", len(node.Calls)-5))
						break
					}
					sb.WriteString(fmt.Sprintf("    - `%s`\n", call))
				}
			}

			// 调用者（谁在调用它）
			if len(astResult.RelatedNodes) > 0 {
				sb.WriteString(fmt.Sprintf("  被调用: %d 处\n", len(astResult.RelatedNodes)))
				for i, caller := range astResult.RelatedNodes {
					if i >= 5 {
						sb.WriteString(fmt.Sprintf("    ... (还有 %d 处)\n", len(astResult.RelatedNodes)-5))
						break
					}
					// Rust 索引器已存储绝对路径，直接使用
					sb.WriteString(fmt.Sprintf("    - [%s] `%s` @ `%s` L%d\n",
						caller.CallType, caller.Node.Name, caller.Node.FilePath, caller.Node.LineStart))
				}
			}

			sb.WriteString("\n")
		}

		// 候选列表（即使有最佳匹配也展示，帮助用户发现其他选项）
		if astResult != nil && len(astResult.Candidates) > 0 {
			// 过滤掉已在 found_symbol 中展示的
			var filteredCandidates []services.CandidateMatch
			if astResult.FoundSymbol != nil {
				for _, c := range astResult.Candidates {
					if c.Node.ID != astResult.FoundSymbol.ID {
						filteredCandidates = append(filteredCandidates, c)
					}
				}
			} else {
				filteredCandidates = astResult.Candidates
			}

			if len(filteredCandidates) > 0 {
				sb.WriteString(fmt.Sprintf("🔍 **其他候选** (AST, 共 %d 个):\n", len(filteredCandidates)))
				for i, c := range filteredCandidates {
					if i >= 5 {
						sb.WriteString(fmt.Sprintf("  ... (还有 %d 个)\n", len(filteredCandidates)-5))
						break
					}
					// Rust 索引器已存储绝对路径，直接使用
					sb.WriteString(fmt.Sprintf("- [%s] `%s` @ `%s` (score: %.2f)\n",
						c.Node.NodeType, c.Node.Name, c.Node.FilePath, c.Score))
				}
				sb.WriteString("\n")
			}
		}

		// 3. Ripgrep Fallback (Text Search & Deep Context)
		if useGrep {
			rg := services.NewRipgrepEngine()

			// 智能检测是否包含路径分隔符，如果有，只搜那个文件或目录
			searchRoot := sm.ProjectRoot
			if scope != "" {
				searchRoot = filepath.Join(sm.ProjectRoot, scope)
			}

			matches, err := rg.Search(ctx, services.SearchOptions{
				Query:         args.Query,
				RootPath:      searchRoot,
				CaseSensitive: false, // 默认不区分大小写
				WordMatch:     false,
				MaxCount:      20, // 限制数量以防爆炸
				ContextLines:  0,
			})

			if err == nil && len(matches) > 0 {
				sb.WriteString(fmt.Sprintf("🕵️ **文本搜索结果** (Ripgrep found %d matches):\n", len(matches)))

				// Group by File
				grouped := make(map[string][]services.TextMatch)
				for _, m := range matches {
					grouped[m.FilePath] = append(grouped[m.FilePath], m)
				}

				// Deep Context Analysis (Limited to top 10 unique files to save time)
				filesProcessed := 0
				for path, fileMatches := range grouped {
					if filesProcessed >= 10 {
						sb.WriteString(fmt.Sprintf("... (剩余 %d 个文件的匹配已省略)\n", len(grouped)-filesProcessed))
						break
					}

					sb.WriteString(fmt.Sprintf("📄 **%s**\n", path))

					for i, m := range fileMatches {
						if i >= 3 {
							sb.WriteString(fmt.Sprintf("  ... (本文件还有 %d 处匹配)\n", len(fileMatches)-i))
							break
						}

						// 🧠 Deep Context: 反查所属符号
						// 性能优化：只查第一个匹配的Context，或者每行都查？
						// 查每行有助于定位 "Where is it used?"
						// 但 exec 开销大。仅对前几行反查。
						contextInfo := ""
						if i < 3 {
							owner, _ := ai.GetSymbolAtLine(sm.ProjectRoot, path, m.LineNumber)
							if owner != nil {
								contextInfo = fmt.Sprintf("in `%s` (%s)", owner.Name, owner.NodeType)
							} else {
								contextInfo = "(global)"
							}
						}

						cleanContent := strings.TrimSpace(m.Content)
						if len(cleanContent) > 80 {
							cleanContent = cleanContent[:80] + "..."
						}

						sb.WriteString(fmt.Sprintf("  L%d: `%s` %s\n", m.LineNumber, cleanContent, contextInfo))
					}
					filesProcessed++
				}
				sb.WriteString("\n")
			} else {
				if len(matches) == 0 && (astResult == nil || (astResult.FoundSymbol == nil && len(astResult.Candidates) == 0)) {
					sb.WriteString(fmt.Sprintf("⚠️ **未找到「%s」** → 换词重试（同义词/缩写/驼峰变体），或用 `project_map` 先看结构\n", args.Query))
				}
			}
		}

		return mcp.NewToolResultText(sb.String()), nil
	}
}
