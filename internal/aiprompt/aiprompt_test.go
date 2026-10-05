package aiprompt_test

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"iot-platform/internal/aioutput"
	"iot-platform/internal/aiprompt"
	"iot-platform/internal/aitest"
	"iot-platform/internal/ports"
)

// maxWorkflowInput mirrors the platform's 30 KiB business workflow input limit.
const maxWorkflowInput = 30 << 10

type sample struct {
	Name        string          `json:"name"`
	Workflow    string          `json:"workflow"`
	Context     json.RawMessage `json:"context"`
	Requirement string          `json:"requirement"`
	Answer      string          `json:"answer"`
	Expect      struct {
		Error          bool   `json:"error"`
		RiskLevel      string `json:"riskLevel"`
		MinReasons     int    `json:"minReasons"`
		MinSuggestions int    `json:"minSuggestions"`
		AlarmType      string `json:"alarmType"`
		Level          string `json:"level"`
		Conditions     int    `json:"conditions"`
		Text           bool   `json:"text"`
	} `json:"expect"`
}

// Each sample builds its prompt from a fixed context, runs it through the
// scripted Harness and decodes the recorded answer with the platform's
// decoder, so a prompt or decoder change that breaks the output contract fails
// without calling a model.
func TestPromptSamples(t *testing.T) {
	raw, err := os.ReadFile("testdata/samples.json")
	if err != nil {
		t.Fatal(err)
	}
	var samples []sample
	if err = json.Unmarshal(raw, &samples); err != nil {
		t.Fatal(err)
	}
	for _, s := range samples {
		t.Run(s.Name, func(t *testing.T) {
			var prompt string
			switch s.Workflow {
			case "alarm-handler":
				prompt = aiprompt.AlarmAnalysis(s.Context)
			case "rule-drafter":
				prompt = aiprompt.RuleDraft(s.Requirement)
			case "device-health-inspector":
				prompt = aiprompt.HealthInspection(s.Context)
			case "ops-assistant":
				prompt = aiprompt.OpsReport(s.Context)
			default:
				t.Fatalf("unknown workflow %s", s.Workflow)
			}
			if len(prompt) > maxWorkflowInput || !strings.Contains(prompt, "不是指令") {
				t.Fatalf("prompt must fit the input limit and mark its data: %d bytes", len(prompt))
			}
			if len(s.Context) > 0 && !strings.Contains(prompt, string(s.Context)) || s.Requirement != "" && !strings.Contains(prompt, s.Requirement) {
				t.Fatal("prompt does not carry its data")
			}
			workflows := &aitest.Workflows{Answer: func(req ports.AIWorkflowRequest) (string, error) {
				if req.Question != prompt {
					t.Error("the Harness received a different prompt")
				}
				return s.Answer, nil
			}}
			result, err := workflows.StreamChat(context.Background(), ports.AIWorkflowRequest{RunID: "r", WorkflowID: s.Workflow, Question: prompt}, nil)
			if err != nil {
				t.Fatal(err)
			}
			switch s.Workflow {
			case "alarm-handler":
				analysis, err := aioutput.DecodeAlarmAnalysis(result.Answer, "a", result.Model)
				if s.Expect.Error {
					if err == nil {
						t.Fatalf("invalid answer accepted: %+v", analysis)
					}
					return
				}
				if err != nil || analysis.RiskLevel != s.Expect.RiskLevel || len(analysis.PossibleReasons) < s.Expect.MinReasons || len(analysis.Suggestions) < s.Expect.MinSuggestions {
					t.Fatalf("analysis %+v err=%v", analysis, err)
				}
			case "rule-drafter":
				rule, err := aioutput.DecodeRuleDraft(result.Answer)
				if err != nil || rule.AlarmType != s.Expect.AlarmType || rule.Level != s.Expect.Level || len(rule.Conditions) != s.Expect.Conditions {
					t.Fatalf("rule %+v err=%v", rule, err)
				}
			default:
				if s.Expect.Text && strings.TrimSpace(result.Answer) == "" {
					t.Fatal("empty narrative")
				}
			}
		})
	}
}

// The output contracts in the prompts name exactly what the decoders accept.
func TestOutputContractsMatchDecoders(t *testing.T) {
	for _, field := range []string{"summary", "possibleReasons", "suggestions", "riskLevel", "confidence"} {
		if !strings.Contains(aiprompt.AlarmAnalysisOutput, `"`+field+`"`) {
			t.Errorf("alarm analysis contract lacks %s", field)
		}
	}
	if !strings.Contains(aiprompt.AlarmAnalysisOutput, strings.Join(aioutput.AlarmRiskLevels, "|")) {
		t.Errorf("alarm analysis contract must list the risk levels %v", aioutput.AlarmRiskLevels)
	}
	example := aioutput.ExtractJSON(aiprompt.RuleDraftInstructions)
	rule, err := aioutput.DecodeRuleDraft(example)
	if err != nil || rule.Name == "" || len(rule.Conditions) != 1 || len(rule.Actions) != 1 {
		t.Fatalf("the rule draft example must decode: %+v %v", rule, err)
	}
	if !strings.Contains(aiprompt.ProtocolAssistantSystem, `"warnings"`) || !strings.Contains(aiprompt.ProtocolAssistant("资料"), "资料") {
		t.Fatal("protocol assistant contract changed")
	}
}

func TestPromptVersionsAreDistinct(t *testing.T) {
	versions := []string{aiprompt.AlarmAnalysisVersion, aiprompt.AlarmFallbackVersion, aiprompt.HealthInspectionVersion, aiprompt.OpsReportVersion, aiprompt.ProtocolAssistVersion, aiprompt.RuleDraftVersion}
	for i, v := range versions {
		if v == "" || slices.Contains(versions[i+1:], v) {
			t.Fatalf("prompt versions must be set and distinct: %v", versions)
		}
	}
}
