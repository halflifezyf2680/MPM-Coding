package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// SystemHookArgs 统一 Hook 工具参数
// 说明：条件必填（取决于 mode）无法在 jsonschema 中表达，需在 handler 中校验。
type SystemHookArgs struct {
	Mode string `json:"mode" jsonschema:"required,enum=create,enum=list,enum=release,description=操作模式"`

	// create
	Description    string `json:"description" jsonschema:"description=待办事项描述 (create)"`
	Priority       string `json:"priority" jsonschema:"default=medium,enum=high,enum=medium,enum=low,description=优先级 (create)"`
	TaskID         string `json:"task_id" jsonschema:"description=关联的任务 ID (create)"`
	Tag            string `json:"tag" jsonschema:"description=可选标签 (create)"`
	ExpiresInHours int    `json:"expires_in_hours" jsonschema:"default=0,description=过期时间(小时), 0表示不过期 (create)"`

	// list
	Status string `json:"status" jsonschema:"default=open,enum=open,enum=closed,description=状态筛选 (list)"`

	// release
	HookID        string `json:"hook_id" jsonschema:"description=Hook 编号 (release), 例如 #001"`
	ResultSummary string `json:"result_summary" jsonschema:"description=完成总结 (release)"`
}

// TaskChainArgs 任务链参数
type TaskChainArgs struct {
	Mode        string      `json:"mode" jsonschema:"required,enum=init,enum=resume,enum=start,enum=complete,enum=spawn,enum=complete_sub,enum=finish,enum=status,enum=protocol,description=操作模式"`
	TaskID      string      `json:"task_id" jsonschema:"required,description=任务ID"`
	Description string      `json:"description" jsonschema:"description=任务描述 (init模式)"`
	Protocol    string      `json:"protocol" jsonschema:"description=协议名称 (init模式，如 develop/debug/refactor，不传则默认 linear)"`
	PhaseID     string      `json:"phase_id" jsonschema:"description=阶段ID (start/complete/spawn/complete_sub模式)"`
	Result      string      `json:"result" jsonschema:"description=gate结果 pass/fail (complete gate模式) 或子任务结果 (complete_sub模式)"`
	Summary     string      `json:"summary" jsonschema:"description=步骤/阶段/子任务总结 (complete/complete_sub模式)"`
	SubID       string      `json:"sub_id" jsonschema:"description=子任务ID (complete_sub模式)"`
	SubTasks    interface{} `json:"sub_tasks" jsonschema:"description=子任务列表 (spawn模式)"`
	Phases      interface{} `json:"phases" jsonschema:"description=手动定义阶段列表 (init模式)"`
}

// RegisterTaskTools 注册任务管理工具
func RegisterTaskTools(s *server.MCPServer, sm *SessionManager) {
	// Hook (统一工具)
	s.AddTool(mcp.NewTool("system_hook",
		mcp.WithDescription(`system_hook - 系统钩子 (挂起/列表/释放)

用途：
  当任务被阻塞（缺信息/待确认/无法继续）时，用 Hook 挂起当前进度；之后可随时列出/释放并记录结果。

参数：
  mode (必填):
    - create: 创建并挂起 Hook
    - list: 列出 Hook
    - release: 释放并闭合 Hook

  create 模式参数：
    - description (必填)
    - priority (默认: medium) high/medium/low
    - task_id (可选)
    - tag (可选)
    - expires_in_hours (默认: 0)

  list 模式参数：
    - status (默认: open) open/closed

  release 模式参数：
    - hook_id (必填) 例如 "#001"
    - result_summary (可选)

示例：
  system_hook(mode="create", description="等待用户提供 API 密钥", priority="high")
  system_hook(mode="list", status="open")
  system_hook(mode="release", hook_id="#001", result_summary="API 密钥已配置并测试通过")

触发词：
  "mpm hook", "mpm hooks", "mpm 挂起", "mpm 待办", "mpm 待办列表", "mpm 释放", "mpm 完成"`),
		mcp.WithInputSchema[SystemHookArgs](),
	), wrapSystemHook(sm))

	// Task Chain - 状态机任务链
	s.AddTool(mcp.NewTool("task_chain",
		mcp.WithDescription(`task_chain - 任务链执行器 (协议状态机模式)

用途：
  管理多步任务的流转。采用协议状态机模式，支持门控(gate)、循环(loop)、条件分支和跨会话持久化。

参数：
  mode (必填):
    - init: 初始化协议任务链（需要 task_id + description，可选 protocol 或 phases）
    - start: 开始一个阶段（需要 task_id + phase_id）
    - complete: 完成一个阶段（需要 task_id + phase_id + summary，gate 需加 result）
    - spawn: 在 loop 阶段生成子任务（需要 task_id + phase_id + sub_tasks）
    - complete_sub: 完成子任务（需要 task_id + phase_id + sub_id + summary，可选 result）
    - status: 查看任务状态（自动识别协议并从 DB 加载进度）
    - resume: 恢复/续传任务
    - finish: 彻底完成并关闭任务链
    - protocol: 列出可用协议

说明：
  - 默认使用 linear 协议（线性执行）。
  - 大工程推荐使用 develop 协议，利用 loop 阶段拆解子任务。

触发词：
  "mpm 任务链", "mpm 续传", "mpm chain"`),
		mcp.WithInputSchema[TaskChainArgs](),
	), wrapTaskChain(sm))
}

func wrapSystemHook(sm *SessionManager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args SystemHookArgs
		if err := request.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("参数错误: %v", err)), nil
		}

		if sm.Memory == nil {
			return mcp.NewToolResultError("记忆层尚未初始化"), nil
		}

		switch strings.ToLower(strings.TrimSpace(args.Mode)) {
		case "create":
			if strings.TrimSpace(args.Description) == "" {
				return mcp.NewToolResultError("参数错误: mode=create 时 description 为必填"), nil
			}
			if strings.TrimSpace(args.Priority) == "" {
				args.Priority = "medium"
			}

			id, err := sm.Memory.CreateHook(ctx, args.Description, args.Priority, args.Tag, args.TaskID, args.ExpiresInHours)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("创建 Hook 失败: %v", err)), nil
			}

			return mcp.NewToolResultText(fmt.Sprintf(
				"📌 Hook 已创建 (ID: %s)\n\n**描述**: %s\n**优先级**: %s\n\n> 使用 `system_hook(mode=\"release\", hook_id=\"%s\")` 释放此 Hook。",
				id, args.Description, args.Priority, id,
			)), nil

		case "list":
			if strings.TrimSpace(args.Status) == "" {
				args.Status = "open"
			}

			hooks, err := sm.Memory.ListHooks(ctx, args.Status)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("查询 Hook 失败: %v", err)), nil
			}
			if len(hooks) == 0 {
				return mcp.NewToolResultText(fmt.Sprintf("暂无 %s 状态的 Hook。", args.Status)), nil
			}

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("### 📋 Hook 列表 (%s)\n\n", args.Status))
			for _, h := range hooks {
				expiration := ""
				if h.ExpiresAt.Valid {
					if time.Now().After(h.ExpiresAt.Time) {
						expiration = " (EXPIRED)"
					} else {
						expiration = fmt.Sprintf(" (Exp: %s)", h.ExpiresAt.Time.Format("01-02 15:04"))
					}
				}
				taskDraft := ""
				if h.RelatedTaskID != "" {
					taskDraft = fmt.Sprintf(" [Task: %s]", h.RelatedTaskID)
				}
				displayID := h.Summary
				if displayID == "" {
					displayID = h.HookID
				}
				sb.WriteString(fmt.Sprintf("- **%s** (ID: %s) [%s]%s %s%s\n", displayID, h.HookID, h.Priority, taskDraft, h.Description, expiration))
			}

			return mcp.NewToolResultText(sb.String()), nil

		case "release":
			if strings.TrimSpace(args.HookID) == "" {
				return mcp.NewToolResultError("参数错误: mode=release 时 hook_id 为必填"), nil
			}
			if err := sm.Memory.ReleaseHook(ctx, args.HookID, args.ResultSummary); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("释放 Hook 失败: %v", err)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("✅ Hook %s 已释放。\n\n**结果摘要**: %s", args.HookID, args.ResultSummary)), nil
		default:
			return mcp.NewToolResultError("参数错误: mode 仅支持 create/list/release"), nil
		}
	}
}

func wrapTaskChain(sm *SessionManager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args TaskChainArgs
		if err := request.BindArguments(&args); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("参数错误: %v", err)), nil
		}

		switch args.Mode {
		case "init":
			return initTaskChainV3(ctx, sm, args)
		case "spawn":
			return spawnSubTasksV3(ctx, sm, args)
		case "complete_sub":
			return completeSubTaskV3(ctx, sm, args)
		case "protocol":
			return mcp.NewToolResultText(renderProtocolList()), nil
		case "start":
			return startPhaseV3(ctx, sm, args)
		case "complete":
			return completePhaseV3(ctx, sm, args)
		case "status", "resume":
			return resumeTaskChainV3(ctx, sm, args.TaskID)
		case "finish":
			_, _ = finishChainV3(ctx, sm, args.TaskID)
			return mcp.NewToolResultText(fmt.Sprintf("\n══════════════════════════════════════════════════════════════\n                    【任务链完成】%s\n══════════════════════════════════════════════════════════════\n\n任务已标记为完成。\n\n下一步建议：\n  → 调用 memo 工具记录最终结果\n  → 向用户汇报任务完成\n", args.TaskID)), nil
		default:
			return mcp.NewToolResultError(fmt.Sprintf("未知模式: %s", args.Mode)), nil
		}
	}
}

func continueExecution() (*mcp.CallToolResult, error) {
	directive := `
══════════════════════════════════════════════════════════════
                    【执行指令】上下文已恢复
══════════════════════════════════════════════════════════════

请回顾上方对话中的【行动纲领】，判断当前进度，然后：

1️⃣ 如果有步骤尚未完成：
   → 调用对应的专家工具执行下一步

2️⃣ 如果所有步骤已完成：
   → 调用 memo 工具记录最终结果
   → 向用户汇报任务完成

	3️⃣ 如果遇到问题无法继续：
	   → 调用 system_hook(mode="create") 挂起任务

══════════════════════════════════════════════════════════════
`
	return mcp.NewToolResultText("⚡ Context Recovered! " + directive), nil
}

// enhanceStepDescription 轻量意图解析：根据关键词补充执行细节
func enhanceStepDescription(name string, step map[string]interface{}) string {
	lowerName := strings.ToLower(name)

	// project_map 模式推断
	if strings.Contains(lowerName, "扫描") || strings.Contains(lowerName, "map") || strings.Contains(lowerName, "结构") {
		if strings.Contains(lowerName, "核对") || strings.Contains(lowerName, "审核") || strings.Contains(lowerName, "对比") || strings.Contains(lowerName, "对齐") {
			// 需要查看完整代码内容
			return name + " (用 full 模式查看完整代码)"
		}
		if strings.Contains(lowerName, "浏览") || strings.Contains(lowerName, "快速") {
			// 只需要概览
			return name + " (用 overview 模式)"
		}
		// 默认用 standard
		return name + " (用 standard 模式)"
	}

	// code_search 精度推断
	if strings.Contains(lowerName, "搜索") || strings.Contains(lowerName, "定位") || strings.Contains(lowerName, "查找") {
		if strings.Contains(lowerName, "函数") || strings.Contains(lowerName, "类") {
			return name + " (设置 search_type=function)"
		}
		if strings.Contains(lowerName, "类") {
			return name + " (设置 search_type=class)"
		}
	}

	// code_impact 方向推断
	if strings.Contains(lowerName, "影响") || strings.Contains(lowerName, "依赖") {
		if strings.Contains(lowerName, "谁调用了") || strings.Contains(lowerName, "被哪里") {
			return name + " (设置 direction=backward)"
		}
		if strings.Contains(lowerName, "调用了谁") || strings.Contains(lowerName, "会影响") {
			return name + " (设置 direction=forward)"
		}
	}

	// 默认返回原名称
	return name
}

func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "..."
}
