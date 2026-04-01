package tools

import (
	"fmt"
	"mcp-server-go/internal/services"
	"path/filepath"
	"sort"
	"strings"
)

// MapRenderer 负责将 MapResult 渲染为Markdown
type MapRenderer struct {
	Result *services.MapResult
	Root   string // 项目根路径，用于计算相对路径
}

func NewMapRenderer(result *services.MapResult, root string) *MapRenderer {
	return &MapRenderer{
		Result: result,
		Root:   root,
	}
}

// DirNode 目录树节点（用于Overview）
type DirNode struct {
	Path          string
	Files         int
	Symbols       int
	AvgComplexity float64
	Children      map[string]*DirNode
	Level         int
}

// RenderOverview 渲染结构视图
func (mr *MapRenderer) RenderOverview() string {
	var sb strings.Builder
	stats := mr.Result.Statistics

	sb.WriteString(fmt.Sprintf("### 🗺️ 项目地图 (Structure)\n\n"))
	sb.WriteString(fmt.Sprintf("**📊 统计**: %d 文件 | %d 符号\n\n", stats.TotalFiles, stats.TotalSymbols))

	// 1. 复杂度统计摘要
	if mr.Result.ComplexityMap != nil && len(mr.Result.ComplexityMap) > 0 {
		mr.renderComplexitySummary(&sb)
	}

	// 2. 构建目录树
	root := mr.buildDirTree()

	// 3. 自适应展开渲染
	sb.WriteString("**📁 目录结构** (按复杂度排序):\n")
	mr.renderAdaptive(&sb, root)

	return sb.String()
}

func (mr *MapRenderer) renderComplexitySummary(sb *strings.Builder) {
	var highCount, medCount, lowCount int
	var totalScore float64
	type ComplexSymbol struct {
		Name  string
		Score float64
	}
	var topSymbols []ComplexSymbol

	for name, score := range mr.Result.ComplexityMap {
		if score >= 50 {
			highCount++
		} else if score >= 20 {
			medCount++
		} else {
			lowCount++
		}
		totalScore += score
		topSymbols = append(topSymbols, ComplexSymbol{Name: name, Score: score})
	}

	avgScore := totalScore / float64(len(mr.Result.ComplexityMap))

	sb.WriteString(fmt.Sprintf("**🔥 复杂度**: High: %d | Med: %d | Low: %d | Avg: %.1f\n\n",
		highCount, medCount, lowCount, avgScore))

	// Top 5
	sort.Slice(topSymbols, func(i, j int) bool {
		return topSymbols[i].Score > topSymbols[j].Score
	})
	if len(topSymbols) > 0 {
		sb.WriteString("**🎯 Top复杂符号**:\n")
		limit := 5
		if len(topSymbols) < limit {
			limit = len(topSymbols)
		}
		for i := 0; i < limit; i++ {
			s := topSymbols[i]
			level := mr.getLevelTag(s.Score)
			sb.WriteString(fmt.Sprintf("  %d. `%s` %s\n", i+1, s.Name, level))
		}
		sb.WriteString("\n")
	}
}

func (mr *MapRenderer) buildDirTree() *DirNode {
	root := &DirNode{
		Path:     "(root)",
		Children: make(map[string]*DirNode),
	}

	// 1. 先把所有文件归位
	// fileMap: dir -> {files, symbols, totalComplexity}
	type dirStatData struct {
		files int
		syms  int
		comp  float64
	}
	stats := make(map[string]*dirStatData)

	for path, nodes := range mr.Result.Structure {
		fileComp := 0.0
		if mr.Result.ComplexityMap != nil {
			for _, n := range nodes {
				if s, ok := mr.Result.ComplexityMap[n.Name]; ok {
					fileComp += s
				}
			}
		}

		dir := filepath.Dir(path)
		dir = strings.ReplaceAll(dir, "\\", "/")
		if dir == "." {
			dir = ""
		}

		if stats[dir] == nil {
			stats[dir] = &dirStatData{}
		}
		stats[dir].files++
		stats[dir].syms += len(nodes)
		stats[dir].comp += fileComp
	}

	// 2. 构建节点
	var getOrCreate func(string) *DirNode
	getOrCreate = func(path string) *DirNode {
		if path == "" {
			return root
		}
		parentPath := filepath.Dir(path)
		parentPath = strings.ReplaceAll(parentPath, "\\", "/")
		if parentPath == "." {
			parentPath = ""
		}

		name := filepath.Base(path)
		parent := getOrCreate(parentPath)

		if parent.Children[name] == nil {
			parent.Children[name] = &DirNode{
				Path:     name, // 这里只存目录名
				Children: make(map[string]*DirNode),
				Level:    parent.Level + 1,
			}
		}
		return parent.Children[name]
	}

	for dirPath, data := range stats {
		node := getOrCreate(dirPath)
		node.Files = data.files
		node.Symbols = data.syms
		node.AvgComplexity = data.comp //这是这个目录下文件的总复杂度
	}

	// 3. 递归汇总（子目录数据加到父目录）
	var aggregate func(*DirNode) (int, int, float64)
	aggregate = func(n *DirNode) (int, int, float64) {
		f, s, c := n.Files, n.Symbols, n.AvgComplexity
		for _, child := range n.Children {
			cf, cs, cc := aggregate(child)
			f += cf
			s += cs
			c += cc
		}
		// 更新累计值
		n.Files = f
		n.Symbols = s
		n.AvgComplexity = c // 这里存总分
		return f, s, c
	}
	aggregate(root)

	return root
}

func (mr *MapRenderer) renderAdaptive(sb *strings.Builder, root *DirNode) {
	// 1. 获取第1层目录
	level1 := getSortedChildren(root)
	n := len(level1)

	// 2. 决定策略
	type Strategy struct {
		ShowLimit int // 显示前N个
		ExpandL3  int // 前M个展开到L3
		ExpandL2  int // 前K个展开到L2
		// 剩余的只显示L1
	}

	var s Strategy
	if n <= 20 {
		s = Strategy{n, n, n}
	} else if n <= 40 {
		s = Strategy{25, 8, 18}
	} else {
		s = Strategy{20, 6, 12}
	}

	compTag := func(totalComp float64, count int) string {
		if count == 0 {
			return ""
		}
		avg := totalComp / float64(count)
		if avg >= 10 {
			return fmt.Sprintf(" [Avg:%.1f]", avg)
		}
		return ""
	}

	// 3. 渲染
	for i, node := range level1 {
		if i >= s.ShowLimit {
			sb.WriteString(fmt.Sprintf("- ... (还有 %d 个低复杂度目录)\n", n-i))
			break
		}

		// 渲染L1
		tag := compTag(node.AvgComplexity, node.Symbols)
		sb.WriteString(fmt.Sprintf("- **%s/** (%d files, %d syms)%s\n", node.Path, node.Files, node.Symbols, tag))

		// 决定是否展开L2
		if i < s.ExpandL2 {
			l2Nodes := getSortedChildren(node)
			for j, l2 := range l2Nodes {
				// L2限制：如果是L3展开模式，L2全显示；否则Top 3/5
				l2Limit := 100 // 无限制
				if i >= s.ExpandL3 {
					l2Limit = 3
				} // Mid组限制L2数量

				if j >= l2Limit {
					sb.WriteString(fmt.Sprintf("  - ... (%d more)\n", len(l2Nodes)-j))
					break
				}

				tag2 := compTag(l2.AvgComplexity, l2.Symbols)
				sb.WriteString(fmt.Sprintf("  - %s/ (%d files)%s\n", l2.Path, l2.Files, tag2))

				// 决定是否展开L3
				if i < s.ExpandL3 {
					l3Nodes := getSortedChildren(l2)
					for k, l3 := range l3Nodes {
						if k >= 5 { // L3始终限制Top 5，避免太深
							sb.WriteString(fmt.Sprintf("    - ... (%d more)\n", len(l3Nodes)-k))
							break
						}
						// L3只显示简略信息
						sb.WriteString(fmt.Sprintf("    - %s/ (%d files)\n", l3.Path, l3.Files))
					}
				}
			}
		}
	}
}

func getSortedChildren(n *DirNode) []*DirNode {
	var children []*DirNode
	for _, c := range n.Children {
		children = append(children, c)
	}
	// 按平均复杂度降序 (总分/符号数)
	sort.Slice(children, func(i, j int) bool {
		avg1 := 0.0
		if children[i].Symbols > 0 {
			avg1 = children[i].AvgComplexity / float64(children[i].Symbols)
		}
		avg2 := 0.0
		if children[j].Symbols > 0 {
			avg2 = children[j].AvgComplexity / float64(children[j].Symbols)
		}
		return avg1 > avg2
	})
	return children
}

func (mr *MapRenderer) getLevelTag(score float64) string {
	if score >= 50 {
		return fmt.Sprintf("[HIGH:%.1f]", score)
	}
	if score >= 20 {
		return fmt.Sprintf("[MED:%.1f]", score)
	}
	return fmt.Sprintf("[LOW:%.1f]", score)
}

// RenderStandard 渲染符号视图
// 策略：智能折叠，Top 10 详细展开
func (mr *MapRenderer) RenderStandard() string {
	var sb strings.Builder
	sb.WriteString("### 🗺️ 项目地图 (Symbols)\n\n")

	// 统计摘要
	stats := mr.Result.Statistics
	sb.WriteString(fmt.Sprintf("**📊 范围统计**: %d files | %d symbols\n", stats.TotalFiles, stats.TotalSymbols))

	if mr.Result.ComplexityMap != nil {
		var high, med, low int
		var total float64
		count := 0
		for _, s := range mr.Result.ComplexityMap {
			if s >= 50 {
				high++
			} else if s >= 20 {
				med++
			} else {
				low++
			}
			total += s
			count++
		}
		if count > 0 {
			sb.WriteString(fmt.Sprintf("**🔥 复杂度**: High: %d | Med: %d | Low: %d | Avg: %.1f\n\n", high, med, low, total/float64(count)))
		}
	} else {
		sb.WriteString("\n")
	}

	mr.renderWithMode(&sb, "Standard", true)
	return sb.String()
}

// FileInfo 文件信息用于排序
type FileInfo struct {
	Path      string
	Name      string
	Nodes     []services.Node
	AvgComp   float64
	NodeCount int
}

// renderWithMode 统一的渲染逻辑
func (mr *MapRenderer) renderWithMode(sb *strings.Builder, mode string, truncate bool) {
	// 1. 整理数据：按目录分组，组内按复杂度排序
	dirGroups := make(map[string][]*FileInfo)

	for path, nodes := range mr.Result.Structure {
		dir := filepath.Dir(path)
		dir = strings.ReplaceAll(dir, "\\", "/")
		if dir == "." {
			dir = "(root)"
		}

		fInfo := &FileInfo{
			Path:      path,
			Name:      filepath.Base(path),
			Nodes:     nodes,
			NodeCount: len(nodes),
		}

		// 计算复杂度
		totalComp := 0.0
		valideNodes := 0
		if mr.Result.ComplexityMap != nil {
			for _, n := range nodes {
				if s, ok := mr.Result.ComplexityMap[n.Name]; ok {
					totalComp += s
					valideNodes++
				}
			}
		}
		fInfo.AvgComp = 0
		if valideNodes > 0 {
			fInfo.AvgComp = totalComp / float64(valideNodes)
		}

		dirGroups[dir] = append(dirGroups[dir], fInfo)
	}

	// 2. 排序目录
	var dirs []string
	for d := range dirGroups {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	// 3. 渲染
	for _, dir := range dirs {
		files := dirGroups[dir]

		// 组内排序：按平均复杂度降序
		sort.Slice(files, func(i, j int) bool {
			return files[i].AvgComp > files[j].AvgComp
		})

		sb.WriteString(fmt.Sprintf("\n📂 **%s/**\n", dir))

		// 自适应折叠策略 (仅Standard模式)
		topLimit := 10
		summaryLimit := 30

		if mode != "Standard" {
			topLimit = 10000 // Full模式不限制
			summaryLimit = 10000
		}

		for i, f := range files {
			// 超出摘要限制：折叠
			if i >= summaryLimit {
				sb.WriteString(fmt.Sprintf("  - ... (还有 %d 个低复杂度文件)\n", len(files)-i))
				break
			}

			// Top 30-50: 摘要模式
			if i >= topLimit {
				compTag := ""
				if f.AvgComp >= 10 {
					compTag = fmt.Sprintf(" [Avg:%.1f]", f.AvgComp)
				}
				sb.WriteString(fmt.Sprintf("  📄 **%s** (%d)%s\n", f.Name, f.NodeCount, compTag))
				continue
			}

			// Top 30: 详细模式
			fileTag := ""
			if f.AvgComp >= 10 {
				fileTag = fmt.Sprintf(" [Avg:%.1f]", f.AvgComp)
			}
			sb.WriteString(fmt.Sprintf("  📄 **%s** (%d)%s\n", f.Name, f.NodeCount, fileTag))

			// 渲染符号 (按复杂度排序)
			sort.Slice(f.Nodes, func(i, j int) bool {
				score1 := 0.0
				score2 := 0.0
				if mr.Result.ComplexityMap != nil {
					score1 = mr.Result.ComplexityMap[f.Nodes[i].Name]
					score2 = mr.Result.ComplexityMap[f.Nodes[j].Name]
				}
				// 复杂度高的排前面；如果相同，按行号
				if score1 != score2 {
					return score1 > score2
				}
				return f.Nodes[i].LineStart < f.Nodes[j].LineStart
			})

			for _, node := range f.Nodes {
				mr.renderNode(sb, node, "    ", truncate) // 4空格缩进
			}
		}
	}
}

func (mr *MapRenderer) renderNode(sb *strings.Builder, node services.Node, indent string, truncate bool) {
	icon := "🔹"
	if node.NodeType == "class" || node.NodeType == "struct" {
		icon = "🔷"
	} else if node.NodeType == "function" {
		icon = "ƒ "
	} else if node.NodeType == "layout" {
		icon = "▣ "
	} else if node.NodeType == "slot" {
		icon = "⬚ "
	} else if node.NodeType == "component" || node.NodeType == "template" {
		icon = "◇ "
	} else if node.NodeType == "selector" {
		icon = "🎨"
	} else if node.NodeType == "keyframes" {
		icon = "◈ "
	}

	// 简化显示：只显示名称，这比截断的长签名更清晰
	// 复杂的参数列表和接收者留给 code_search 去看
	desc := node.Name

	// 🆕 根据复杂度添加文本标记（LLM可读）
	complexityMarker := ""
	if mr.Result.ComplexityMap != nil {
		if score, exists := mr.Result.ComplexityMap[node.Name]; exists {
			if score >= 50 {
				complexityMarker = fmt.Sprintf(" [HIGH:%.1f]", score)
			} else if score >= 20 {
				complexityMarker = fmt.Sprintf(" [MED:%.1f]", score)
			} else if score > 0 {
				complexityMarker = fmt.Sprintf(" [LOW:%.1f]", score)
			}
		}
	}

	sb.WriteString(fmt.Sprintf("%s%s `%s` L%d%s\n", indent, icon, desc, node.LineStart, complexityMarker))
}
