package tools

import (
	"fmt"
	"mcp-server-go/internal/services"
	"path"
	"path/filepath"
	"strings"
)

func normalizeProjectRelativePath(projectRoot, raw, fieldName string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}

	if filepath.IsAbs(value) || (len(value) >= 2 && value[1] == ':') {
		return "", fmt.Errorf(
			"%s 参数应为项目内相对路径，而不是绝对路径。\n   你传入的是: `%s`\n   当前项目根: `%s`",
			fieldName, value, projectRoot,
		)
	}

	normalized := strings.ReplaceAll(value, "\\", "/")
	cleaned := path.Clean(normalized)
	cleaned = strings.TrimPrefix(cleaned, "./")

	if cleaned == "." || cleaned == "" {
		return "", nil
	}

	if strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", fmt.Errorf(
			"%s 参数不能越出当前项目目录。\n   你传入的是: `%s`\n   当前项目根: `%s`",
			fieldName, value, projectRoot,
		)
	}

	return cleaned, nil
}

func warmIndexForPath(ai interface {
	IndexScope(string, string) (*services.IndexResult, error)
	EnsureFreshIndex(string) (*services.IndexResult, error)
}, projectRoot, scope string) error {
	if strings.TrimSpace(scope) != "" {
		_, err := ai.IndexScope(projectRoot, scope)
		return err
	}
	_, err := ai.EnsureFreshIndex(projectRoot)
	return err
}
