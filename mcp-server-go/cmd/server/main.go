package main

import (
	"fmt"
	"os"

	"mcp-server-go/internal/core"
	"mcp-server-go/internal/services"
	"mcp-server-go/internal/tools"

	"github.com/mark3labs/mcp-go/server"
)

var version = "dev"

func init() {
	os.Setenv("LANG", "zh_CN.UTF-8")
	os.Setenv("LC_ALL", "zh_CN.UTF-8")
}

func main() {
	sm := &tools.SessionManager{Version: version}
	ai := services.NewASTIndexer()

	// 🚀 [LifeCycle] 探测并尝试自动绑定项目
	projectRoot := core.DetectProjectRoot()
	if projectRoot != "" {
		fmt.Fprintf(os.Stderr, "[MCP-Go] 已锁定项目根目录: %s\n", projectRoot)
		m, err := core.NewMemoryLayer(projectRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[MCP-Go][ERROR] 记忆层初始化受阻: %v\n", err)
		} else {
			sm.Memory = m
			sm.ProjectRoot = projectRoot
			fmt.Fprintf(os.Stderr, "[MCP-Go] 记忆层（SSOT）与项目上下文已就绪。\n")

		}
	} else {
		fmt.Fprintf(os.Stderr, "[MCP-Go][WARN] 无法探测项目根目录，请检查环境变量或在项目目录下运行。\n")
	}

	// 注：HUD 自动启动已移至 initialize_project 工具，不再在 server 启动时触发

	// 启动 MCP Server (StdIO)
	s := server.NewMCPServer(
		"MyProjectManager-Go",
		version,
	)
	tools.RegisterSystemTools(s, sm, ai)       // 系统初始化
	tools.RegisterMemoryTools(s, sm)           // 备忘与检索
	tools.RegisterSearchTools(s, sm, ai)       // 项目地图与搜索
	tools.RegisterIntelligenceTools(s, sm, ai) // 任务分析与事实存档
	tools.RegisterAnalysisTools(s, sm, ai)     // 影响分析工具
	tools.RegisterTaskTools(s, sm)             // 任务管理工具
	tools.RegisterEnhanceTools(s, sm)          // 增强工具 (persona)

	fmt.Fprintf(os.Stderr, "[MCP-Go] MyProjectManager 正在启动...\n")

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "服务运行错误: %v\n", err)
		os.Exit(1)
	}
}
