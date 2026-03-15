package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DataDirName is the canonical data directory name for MPM
	DataDirName = ".mpm-data"
	// LegacyDataDirName is the legacy data directory name (for migration)
	LegacyDataDirName = ".mcp-data"
	// ProjectConfigFile is the project configuration filename
	ProjectConfigFile = "project_config.json"

	// Descendant scan bounds
	maxDescendantDepth       = 3
	maxDescendantDirsVisited = 200
)

// skipDirsInDescendantScan are directories to skip during descendant scan
var skipDirsInDescendantScan = map[string]bool{
	"node_modules":  true,
	"target":        true,
	"vendor":        true,
	".git":          true,
	".mpm-data":     true,
	".mcp-data":     true,
	".idea":         true,
	".vscode":       true,
	".mvn":          true,
	"__pycache__":   true,
	".tox":          true,
	".nox":          true,
	".pytest_cache": true,
	"dist":          true,
	"build":         true,
	"out":           true,
	"bin":           true,
	"pkg":           true,
}

// ProjectConfig represents the project configuration
type ProjectConfig struct {
	ProjectRoot   string `json:"project_root"`
	InitializedAt string `json:"initialized_at"`
}

// DetectProjectRoot 探测当前活跃项目路径
// 优先级：向上搜索锚点 > 向下有界扫描 > 环境变量 > CWD 项目标识
func DetectProjectRoot() string {
	cwd, _ := os.Getwd()

	// 1. 最高优先级：从 CWD 向上搜索 .mpm-data/project_config.json（项目锚点）
	if root := findProjectRootByAnchor(cwd); root != "" {
		return root
	}

	// 2. 向下有界扫描：在子目录中搜索锚点（CWD可能是workspace聚合目录）
	if root := findProjectRootByDescendantScan(cwd); root != "" {
		return root
	}

	// 3. 其次信任 IDE 显式提供的环境变量
	envKeys := []string{"MPM_PROJECT_ROOT", "WORKSPACE_FOLDER", "SESSION_DIR", "VSCODE_CWD", "INIT_CWD"}
	for _, k := range envKeys {
		val := strings.TrimSpace(os.Getenv(k))
		if val != "" {
			abs, err := filepath.Abs(val)
			if err == nil && ValidateProjectPath(abs) {
				return abs
			}
		}
	}

	// 4. 最后选择当前工作目录 (CWD)，但必须通过项目根门槛检查
	if cwd != "" {
		abs, err := filepath.Abs(cwd)
		if err == nil && ValidateProjectPath(abs) && looksLikeProjectRoot(abs) {
			return abs
		}
	}

	return ""
}

// findProjectRootByAnchor 从指定路径向上搜索 .mpm-data/project_config.json
// 如果找到，返回包含该目录的父目录作为项目根
func findProjectRootByAnchor(startPath string) string {
	current, err := filepath.Abs(startPath)
	if err != nil {
		return ""
	}

	for {
		// 优先检查新目录名
		configPath := filepath.Join(current, DataDirName, ProjectConfigFile)
		if _, err := os.Stat(configPath); err == nil {
			// 验证配置文件
			if root := validateProjectConfig(configPath); root != "" {
				return root
			}
		}

		// 兼容旧目录名（仅当新目录不存在时）
		legacyConfigPath := filepath.Join(current, LegacyDataDirName, ProjectConfigFile)
		if _, err := os.Stat(legacyConfigPath); err == nil {
			if root := validateProjectConfig(legacyConfigPath); root != "" {
				return root
			}
		}

		// 向上一级
		parent := filepath.Dir(current)
		if parent == current {
			// 已到达根目录
			break
		}
		current = parent
	}

	return ""
}

// findProjectRootByDescendantScan 在子目录中有界搜索项目锚点
// 约束：最大深度3层，最多访问200个目录，跳过重量级目录
// 如果找到唯一一个有效项目则返回；多个时返回空并警告
func findProjectRootByDescendantScan(startPath string) string {
	absStart, err := filepath.Abs(startPath)
	if err != nil {
		return ""
	}

	var candidates []string
	visitedCount := 0

	// BFS遍历，带深度限制
	type dirEntry struct {
		path  string
		depth int
	}
	queue := []dirEntry{{path: absStart, depth: 0}}

	for len(queue) > 0 && visitedCount < maxDescendantDirsVisited {
		current := queue[0]
		queue = queue[1:]

		// 检查是否超出深度限制
		if current.depth > maxDescendantDepth {
			continue
		}

		visitedCount++
		if visitedCount > maxDescendantDirsVisited {
			break
		}

		// 检查当前目录是否包含项目锚点
		configPath := filepath.Join(current.path, DataDirName, ProjectConfigFile)
		if root := validateProjectConfig(configPath); root != "" {
			candidates = append(candidates, root)
			// 找到锚点后不再向该目录的子目录搜索
			continue
		}

		// 兼容旧目录名
		legacyConfigPath := filepath.Join(current.path, LegacyDataDirName, ProjectConfigFile)
		if root := validateProjectConfig(legacyConfigPath); root != "" {
			candidates = append(candidates, root)
			continue
		}

		// 读取子目录并加入队列
		entries, err := os.ReadDir(current.path)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			// 跳过隐藏目录和重量级目录
			if strings.HasPrefix(name, ".") && name != "." && name != ".." {
				// 允许 .mpm-data/.mcp-data 作为锚点目录，但它们已经在skipDirsInDescendantScan中
			}
			if skipDirsInDescendantScan[name] {
				continue
			}

			childPath := filepath.Join(current.path, name)
			queue = append(queue, dirEntry{path: childPath, depth: current.depth + 1})
		}
	}

	// 处理搜索结果
	switch len(candidates) {
	case 0:
		return ""
	case 1:
		return candidates[0]
	default:
		// 多个候选，拒绝猜测，输出警告
		fmt.Fprintf(os.Stderr, "[MPM][WARN] 在子目录中发现多个项目锚点，请显式指定 project_root：\n")
		for i, c := range candidates {
			fmt.Fprintf(os.Stderr, "  [%d] %s\n", i+1, c)
		}
		return ""
	}
}

// validateProjectConfig 验证并读取项目配置
func validateProjectConfig(configPath string) string {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}

	var config ProjectConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return ""
	}

	// 验证配置中的路径存在且合法
	if config.ProjectRoot != "" {
		abs, err := filepath.Abs(config.ProjectRoot)
		if err == nil && ValidateProjectPath(abs) {
			return abs
		}
	}

	return ""
}

// looksLikeProjectRoot 检查路径是否看起来像真正的项目根目录
// 仅当存在项目标识文件时才返回 true，避免在 MCP server 安装目录等位置误判
func looksLikeProjectRoot(path string) bool {
	// 项目标识文件/目录清单（包含新旧数据目录名）
	projectMarkers := []string{
		DataDirName,       // MPM 新数据目录
		LegacyDataDirName, // MPM 旧数据目录（兼容）
		".git",            // Git 项目
		"go.mod",          // Go 项目
		"package.json",    // Node.js 项目
		"pyproject.toml",  // Python 项目
		"Cargo.toml",      // Rust 项目
		"pom.xml",         // Java Maven 项目
		"build.gradle",    // Java Gradle 项目
		".svn",            // SVN 项目
		".hg",             // Mercurial 项目
	}

	for _, marker := range projectMarkers {
		markerPath := filepath.Join(path, marker)
		if _, err := os.Stat(markerPath); err == nil {
			return true
		}
	}

	return false
}

// GetDataDir 获取项目的数据目录路径（返回绝对路径）
// 如果 legacy 目录存在但新目录不存在，会自动迁移
func GetDataDir(projectRoot string) (string, error) {
	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", err
	}

	newDir := filepath.Join(absRoot, DataDirName)
	legacyDir := filepath.Join(absRoot, LegacyDataDirName)

	// 检查新目录是否存在
	if _, err := os.Stat(newDir); err == nil {
		return newDir, nil
	}

	// 检查旧目录是否存在，需要迁移
	if _, err := os.Stat(legacyDir); err == nil {
		// 执行迁移
		if err := migrateDataDir(legacyDir, newDir); err != nil {
			// 迁移失败，返回旧目录（降级）
			fmt.Fprintf(os.Stderr, "[MPM][WARN] 迁移 %s -> %s 失败: %v，继续使用旧目录\n", legacyDir, newDir, err)
			return legacyDir, nil
		}
		fmt.Fprintf(os.Stderr, "[MPM][INFO] 已迁移 %s -> %s\n", legacyDir, newDir)
		return newDir, nil
	}

	// 都不存在，创建新目录
	if err := os.MkdirAll(newDir, 0755); err != nil {
		return "", err
	}
	return newDir, nil
}

// migrateDataDir 迁移旧数据目录到新目录
// 在 Windows 上使用 rename 可能失败（跨卷/锁定），改用 copy + 保留旧目录
func migrateDataDir(src, dst string) error {
	// 目标目录的父目录必须存在
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	// 尝试直接重命名（最快，同一文件系统）
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	// rename 失败，使用复制方式
	if err := copyDir(src, dst); err != nil {
		return err
	}

	// 复制成功后保留旧目录，添加 README 说明
	readmePath := filepath.Join(src, "MIGRATED.txt")
	readmeContent := fmt.Sprintf(`MPM Data Directory Migration Notice
===================================
Date: %s

This directory has been migrated to: %s

The new canonical data directory name is ".mpm-data/".
You can safely delete this legacy ".mcp-data/" directory after verifying the migration.
`, time.Now().Format(time.RFC3339), dst)
	_ = os.WriteFile(readmePath, []byte(readmeContent), 0644)

	return nil
}

// copyDir 递归复制目录
func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			// 复制文件
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dstPath, data, 0644); err != nil {
				return err
			}
		}
	}

	return nil
}

// ValidateProjectPath 验证路径是否安全且合法
func ValidateProjectPath(path string) bool {
	if path == "" || path == "Unknown" {
		return false
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return false
	}

	// 0. 基础过滤：绝对禁止直接在盘符根目录 (如 D:\, C:\) 创建项目
	// 如果路径等于其体积标识（如 C:\），则拒绝
	if abs == filepath.VolumeName(abs)+string(filepath.Separator) {
		return false
	}

	pLow := strings.ToLower(abs)

	// 1. 严格系统禁区
	systemTraps := []string{"c:\\windows", "system32", "program files", "programdata", "users\\all users"}
	for _, trap := range systemTraps {
		if strings.Contains(pLow, trap) {
			return false
		}
	}

	// 2. 严格排除 IDE 运行时目录、应用数据区、临时目录等容器环境
	ideTraps := []string{"antigravity", "kiro", "vscode", "cursor", "claude", "windsurf", "pycharm", "intellij"}
	for _, trap := range ideTraps {
		if strings.Contains(pLow, trap) {
			return false
		}
	}

	sensitiveDirs := []string{"appdata", "local/programs", "local\\programs", "application data", "temp", "prefetch"}
	for _, sd := range sensitiveDirs {
		if strings.Contains(pLow, sd) {
			// 除非明确包含 .git 标记
			if _, err := os.Stat(filepath.Join(abs, ".git")); err != nil {
				return false
			}
		}
	}

	return true
}
