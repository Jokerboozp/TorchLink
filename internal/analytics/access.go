package analytics

import (
	"context"
	"errors"
	"slices"
)

const (
	KindDataQuality = "DATA_QUALITY"
	KindMonitoring  = "MONITORING_GAPS"
	KindRuleLab     = "RULE_LAB"
	KindResponse    = "RESPONSE_REVIEW"
	KindMaintenance = "MAINTENANCE_OUTCOME"
	KindInvestment  = "MAINTENANCE_INVESTMENT"
)

var ErrForbidden = errors.New("无分析权限或设备范围已变化")
var ErrUnsupported = errors.New("此分析应用尚未启用")

type Actor struct {
	TenantID       string
	Username       string
	Managed        bool
	SessionVersion int64
	AccessVersion  string
	AllDevices     bool
	DeviceIDs      []string
	Permissions    []string
}

type ResolveActor func(context.Context, Actor) (Actor, error)
type ValidateDevice func(context.Context, string, string) error

func Menu(kind string) string {
	switch kind {
	case KindDataQuality:
		return "dataQuality"
	case KindMonitoring:
		return "monitoringGaps"
	case KindRuleLab:
		return "ruleLab"
	case KindResponse:
		return "response"
	case KindMaintenance, KindInvestment:
		return "maintenance"
	}
	return ""
}

func Prefix(kind string) string {
	switch kind {
	case KindDataQuality:
		return "/api/v1/data-quality"
	case KindMonitoring:
		return "/api/v1/monitoring-gaps"
	case KindRuleLab:
		return "/api/v1/rule-lab"
	case KindResponse:
		return "/api/v1/response-runs"
	case KindMaintenance:
		return "/api/v1/maintenance-observations"
	case KindInvestment:
		return "/api/v1/investment-scenarios"
	}
	return ""
}

// RunCollection identifies fixed analysis revisions. Business executions and
// plans use separate resources, so their permissions reference real routes.
func RunCollection(kind string) string {
	switch kind {
	case KindResponse:
		return "/api/v1/response-evaluations"
	case KindMaintenance:
		return "/api/v1/maintenance-observations"
	case KindInvestment:
		return "/api/v1/investment-evaluations"
	default:
		return Prefix(kind) + "/runs"
	}
}

func CreateOperation(kind string) string {
	switch kind {
	case KindRuleLab:
		return "POST /api/v1/rule-lab/experiments/:id/runs"
	case KindResponse:
		return "POST /api/v1/response-runs/:id/evaluations"
	case KindMaintenance:
		return "POST /api/v1/maintenance-records/:id/observations"
	case KindInvestment:
		return "POST /api/v1/investment-scenarios/:id/evaluate"
	default:
		return "POST " + RunCollection(kind)
	}
}

func ValidCreationOperation(kind, operation string) bool {
	return operation == CreateOperation(kind) || kind == KindRuleLab && operation == "POST /api/v1/rule-lab/datasets"
}

func RunCreationOperation(runKind, operation string) string {
	if ValidCreationOperation(runKind, operation) {
		return operation
	}
	return CreateOperation(runKind)
}

func AIStartOperation(kind string) string {
	switch kind {
	case KindRuleLab:
		return "POST /api/v1/rule-lab/experiments/:id/ai-jobs"
	case KindResponse:
		return "POST /api/v1/response-runs/:id/ai-jobs"
	case KindInvestment:
		return "POST /api/v1/investment-scenarios/:id/ai-jobs"
	default:
		return "POST " + RunCollection(kind) + "/:id/ai-jobs"
	}
}

func AIStopOperation(kind string) string {
	return AIStartOperation(kind) + "/:jobId/stop"
}

func (a Actor) Allows(kind, operation string, ids []string) bool {
	menu := Menu(kind)
	if a.TenantID == "" || a.Username == "" || menu == "" || len(ids) == 0 {
		return false
	}
	if !slices.Contains(a.Permissions, "*") {
		if !slices.Contains(a.Permissions, "menu:devices") || !slices.Contains(a.Permissions, "menu:"+menu) {
			return false
		}
		if operation != "" && !slices.Contains(a.Permissions, operation) {
			return false
		}
	}
	if !a.AllDevices {
		for _, id := range ids {
			if !slices.Contains(a.DeviceIDs, id) {
				return false
			}
		}
	}
	return true
}
