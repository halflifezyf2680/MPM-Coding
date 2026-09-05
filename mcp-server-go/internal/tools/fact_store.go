package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// fact 表（项目根 known-facts.md）：项目经验事实的唯一 SSOT。
// 开放 markdown 表格（状态/类型/内容/scope 四列），人可读、git 可提交、外部脚本可维护。
// 读取每次直读文件——写入下一轮即生效（热加载），无索引无缓存。
// 状态流转只有人工/脚本/gate-fail 提示三条路径，无自动演化。

const (
	factFileName        = "known-facts.md"
	factHeader          = "# Known Facts\n\n| 状态 | 类型 | 内容 | scope |\n|---|---|---|---|\n"
	factActiveThreshold = 20 // active 行数超过时提示维护者剪枝
	factLineMaxRunes    = 120
)

type factRow struct {
	Status string // active / candidate / rejected
	Type   string // 铁律 / 避坑 / pitfall / success_pattern / observation ...
	Text   string
	Scope  string // project / path:xxx / symbol:xxx
}

// parseFactTable 解析 fact 文件的 markdown 表格。
// 容错：表头、分隔行、列数不足、状态非法的行一律跳过并收集警告，不阻塞调用方。
func parseFactTable(content string) (rows []factRow, warnings []string) {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cols := strings.Split(strings.Trim(trimmed, "|"), "|")
		if len(cols) < 4 {
			continue
		}
		for i := range cols {
			cols[i] = strings.TrimSpace(cols[i])
		}
		if strings.Contains(cols[0], "状态") || strings.HasPrefix(cols[1], "-") {
			continue // 表头或分隔行
		}
		status := strings.ToLower(cols[0])
		switch status {
		case "active", "candidate", "rejected":
		default:
			warnings = append(warnings, fmt.Sprintf("非法状态 %q，跳过该行", cols[0]))
			continue
		}
		rows = append(rows, factRow{Status: status, Type: cols[1], Text: cols[2], Scope: cols[3]})
	}
	return rows, warnings
}

// loadFactRows 读项目根 fact 文件。文件不存在返回空结果（静默，非错误）。
func loadFactRows(projectRoot string) (rows []factRow, warnings []string, path string, exists bool) {
	path = filepath.Join(projectRoot, factFileName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil, path, false
	}
	if err != nil {
		return nil, []string{fmt.Sprintf("读取 %s 失败: %v", factFileName, err)}, path, true
	}
	rows, warnings = parseFactTable(string(data))
	return rows, warnings, path, true
}

// appendFactRow 原子追加一行到 fact 文件；文件不存在则创建（带表头）。
// 内容列中的竖线被替换，防止破坏表格结构。
func appendFactRow(projectRoot, status, factType, text, scope string) error {
	path := filepath.Join(projectRoot, factFileName)
	text = strings.ReplaceAll(strings.TrimSpace(text), "|", "∣")
	scope = strings.ReplaceAll(strings.TrimSpace(scope), "|", "∣")
	line := fmt.Sprintf("| %s | %s | %s | %s |", status, factType, text, scope)

	existing, _ := os.ReadFile(path)
	content := string(existing)
	if content == "" {
		content = factHeader
	} else if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += line + "\n"

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("重命名失败: %w", err)
	}
	return nil
}

// appendFactObservations 将 memo/gate-fail 沉淀的可复用观察写入 fact 表（candidate）。
func appendFactObservations(projectRoot string, observations []string) (int, error) {
	written := 0
	for _, obs := range observations {
		obs = strings.TrimSpace(obs)
		if obs == "" {
			continue
		}
		if err := appendFactRow(projectRoot, "candidate", "observation", obs, "project"); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

// factTypePriority 铁律/避坑类经验优先于一般成功模式。
func factTypePriority(t string) int {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "铁律", "避坑", "pitfall", "rule":
		return 3
	case "success_pattern", "经验":
		return 2
	default:
		return 1
	}
}

// renderRecallFacts 为 system_recall 产出 fact 段的精选输出：
// 只取 active；类型优先、scope/内容命中加权，表内序为基序；每条一行；
// 尾部附完整表路径（LLM 需要细节时自行 Read）；active 超阈值附维护警告。
// 无命中返回空字符串（调用方不输出 fact 段）。
func renderRecallFacts(projectRoot, keywords string, limit int) string {
	rows, warnings, path, exists := loadFactRows(projectRoot)
	if !exists {
		return ""
	}

	var active []factRow
	for _, r := range rows {
		if r.Status == "active" {
			active = append(active, r)
		}
	}
	if len(active) == 0 {
		return ""
	}

	words := strings.Fields(strings.ToLower(strings.ReplaceAll(keywords, ",", " ")))
	type scored struct {
		row   factRow
		score int
		idx   int
	}
	var scoredRows []scored
	for i, r := range active {
		haystack := strings.ToLower(r.Type + " " + r.Text + " " + r.Scope)
		hits := 0
		for _, w := range words {
			if len(w) < 2 {
				continue
			}
			if strings.Contains(haystack, w) {
				hits++
			}
		}
		if hits == 0 && len(words) > 0 {
			continue // 宽进后按命中截断：无命中的不进精选
		}
		scoredRows = append(scoredRows, scored{row: r, score: factTypePriority(r.Type)*10 + hits, idx: i})
	}
	if len(scoredRows) == 0 {
		return ""
	}
	sort.SliceStable(scoredRows, func(i, j int) bool {
		return scoredRows[i].score > scoredRows[j].score
	})
	if limit <= 0 || limit > 10 {
		limit = 10
	}
	if len(scoredRows) > limit {
		scoredRows = scoredRows[:limit]
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## 📌 Known Facts (%d)\n\n", len(scoredRows)))
	for _, s := range scoredRows {
		text := s.row.Text
		if r := []rune(text); len(r) > factLineMaxRunes {
			text = string(r[:factLineMaxRunes]) + "…"
		}
		if s.row.Scope != "" && s.row.Scope != "project" {
			sb.WriteString(fmt.Sprintf("- **[%s]** %s _(scope: %s)_\n", s.row.Type, text, s.row.Scope))
		} else {
			sb.WriteString(fmt.Sprintf("- **[%s]** %s\n", s.row.Type, text))
		}
	}
	sb.WriteString(fmt.Sprintf("\n完整事实表: %s（可 Read 展开细节，翻状态控制生效）\n", path))
	if len(active) > factActiveThreshold {
		sb.WriteString(fmt.Sprintf("⚠️ active 事实 %d 条已超过 %d，建议人工去重/压缩。\n", len(active), factActiveThreshold))
	}
	for _, w := range warnings {
		sb.WriteString("⚠️ " + w + "\n")
	}
	return sb.String()
}
