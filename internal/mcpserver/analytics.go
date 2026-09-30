package mcpserver

import (
	"context"
	"errors"
	"math"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"iot-platform/internal/analytics"
	"iot-platform/internal/auth"
	"iot-platform/internal/core"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func registerAnalysisTool(s *server.MCPServer, engine *core.Engine) {
	s.AddTool(mcp.NewTool("query_analysis_snapshot", mcp.WithDescription("分页读取本次签名 AI 任务绑定的固定分析版本。summary 是准确汇总；metrics/findings/evidence 为明细；监测工作流另可读 intervals/dependency-groups；规则实验另可读 outcomes/diffs/labels，默认20最多100。hasMore 为真按 nextOffset 继续。不得指定设备、任务、租户或来源查询。"), mcp.WithString("collection", mcp.Description("summary、metrics、findings、evidence；监测另支持 intervals、dependency-groups；规则实验另支持 outcomes、diffs、labels。默认 summary")), mcp.WithNumber("limit", mcp.Description("明细页大小1到100，默认20")), mcp.WithNumber("offset", mcp.Description("明细偏移，默认0"))), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		collection := "summary"
		limit, offset := 20, 0
		var err error
		for key, value := range args {
			switch key {
			case "collection":
				var ok bool
				collection, ok = value.(string)
				if !ok {
					err = errors.New("collection 必须是字符串")
				}
			case "limit", "offset":
				number, ok := value.(float64)
				if !ok || number != math.Trunc(number) || number < 0 || number > 1e7 {
					err = errors.New("分页参数必须是非负整数")
					break
				}
				if key == "limit" {
					limit = int(number)
					if limit < 1 || limit > 100 {
						err = errors.New("limit 必须为1到100")
					}
				} else {
					offset = int(number)
				}
			default:
				err = errors.New("分析工具不接受范围或上游参数")
			}
		}
		var result model.AnalysisAIFacts
		if err == nil {
			result, err = readAnalysisSnapshot(ctx, engine, collection, limit, offset)
		}
		return auditedResult(ctx, engine, "query_analysis_snapshot", args, result, err)
	})
}
func readAnalysisSnapshot(ctx context.Context, engine *core.Engine, collection string, limit, offset int) (model.AnalysisAIFacts, error) {
	if _, err := tenantForTool(ctx, auth.ScopeQueryAnalysisSnapshot, true); err != nil {
		return model.AnalysisAIFacts{}, err
	}
	c, ok := auth.ClaimsFromContext(ctx)
	if !ok || !analytics.IsAnalysisWorkflow(c.Workflow) || c.AnalysisJobID == "" || c.AnalysisSnapshotID == "" || c.AnalysisSnapshotVersion <= 0 || c.AnalysisLeaseToken <= 0 || c.AnalysisAccessVersion == "" || engine.AnalysisAI == nil {
		return model.AnalysisAIFacts{}, errors.New("分析任务或快照未获授权")
	}
	identity := ports.AIRunIdentity{TenantID: c.TenantID, Username: c.Username, ManagedUser: c.ManagedUser, SessionVersion: c.SessionVersion, AccessVersion: c.AnalysisAccessVersion, AnalysisRunID: c.AnalysisRunID, AnalysisSnapshotID: c.AnalysisSnapshotID, AnalysisSnapshotVersion: c.AnalysisSnapshotVersion, AnalysisJobID: c.AnalysisJobID, AnalysisLeaseToken: c.AnalysisLeaseToken, AnalysisHarnessRunID: c.RunID, AnalysisWorkflowID: c.Workflow}
	return engine.AnalysisAI.ReadBoundAnalysisFacts(ctx, identity, collection, limit, offset)
}
