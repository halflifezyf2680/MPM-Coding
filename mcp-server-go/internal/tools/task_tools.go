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

// truncateRunes 按字符数截断字符串
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
