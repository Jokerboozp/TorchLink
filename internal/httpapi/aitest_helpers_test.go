package httpapi

import (
	"context"
	"encoding/json"

	"iot-platform/internal/aitest"
	"iot-platform/internal/auth"
	"iot-platform/internal/core"
	"iot-platform/internal/ports"
)

const testAnalysisAnswer = `{"summary":"研判完成","possibleReasons":["现场存在烟雾"],"suggestions":["核实现场"],"riskLevel":"HIGH","confidence":0.9}`

// installEndpointWorkflows answers every business workflow through the Harness
// fake: rule drafts and protocol drafts reuse protocolEndpointAI's content.
func installEndpointWorkflows(engine *core.Engine) *aitest.Workflows {
	workflows := &aitest.Workflows{Answer: func(req ports.AIWorkflowRequest) (string, error) {
		switch req.WorkflowID {
		case core.WorkflowRuleDraft:
			rule, err := protocolEndpointAI{}.RuleDraft(context.Background(), "", "")
			if err != nil {
				return "", err
			}
			body, err := json.Marshal(rule)
			return string(body), err
		case core.WorkflowProtocolAssist:
			return protocolEndpointAI{}.GenerateJSON(context.Background(), "", "", "")
		case core.WorkflowHealthInspection, core.WorkflowOpsReport:
			return "巡检建议已生成", nil
		}
		return testAnalysisAnswer, nil
	}}
	engine.AIWorkflows, engine.HarnessTokens = workflows, aitest.Tokens()
	return workflows
}

// testRunIdentity is an unmanaged caller allowed every Harness tool scope.
func testRunIdentity(username string) ports.AIRunIdentity {
	return ports.AIRunIdentity{Username: username, Scopes: auth.HarnessReadScopes()}
}
