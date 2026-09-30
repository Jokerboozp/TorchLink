package response

import (
	"iot-platform/internal/model"
	"testing"
)

func procedureFixture() model.ResponseProcedure {
	return model.ResponseProcedure{Name: "处置核对", Scenario: "隔离演练", Steps: []model.ResponseStep{
		{ID: "ack", Name: "平台确认", Role: "值班员", Required: true, RequiredEvidence: []string{"ALARM_LIFECYCLE"}, ClockStart: "PLATFORM_RECEIVED", TargetMs: 2000},
		{ID: "arrival", Name: "现场到达", Role: "检查员", Required: true, Prerequisites: []string{"ack"}, RequiredEvidence: []string{"MANUAL_RECORD"}, ClockStart: "ack", TargetMs: 10000},
	}}
}
func executionFixture() model.ResponseExecution {
	return model.ResponseExecution{Source: "REAL_CASE", ProcedureRevisionID: "p-v1", Procedure: procedureFixture(), StartedAt: 1000, PlatformReceivedAt: 1000, Milestones: []model.ResponseMilestone{{ID: "ack-1", StepID: "ack", Source: "SYSTEM", Status: "CONFIRMED", OccurredAt: 2500, RecordedAt: 3000, Evidence: []model.ResponseEvidenceReference{{ID: "e1", Kind: "ALARM_LIFECYCLE", SourceID: "actual-ack", DeviceID: "d1", Description: "实际确认事务事件"}}}}}
}

func TestAcknowledgementDoesNotProveArrival(t *testing.T) {
	r, err := Evaluate(executionFixture(), "exec-v1", 8000)
	if err != nil {
		t.Fatal(err)
	}
	if r.RequiredSteps != 2 || r.CompletedRequiredSteps != 1 || r.CompletionRate == nil || *r.CompletionRate != .5 || r.EvidenceCoverage == nil || *r.EvidenceCoverage != .5 {
		t.Fatalf("unexpected denominators: %+v", r)
	}
	ack, arrival := r.Steps[0], r.Steps[1]
	if ack.DurationMs == nil || *ack.DurationMs != 1500 || ack.TargetResult != "WITHIN_UNIT_TARGET" {
		t.Fatalf("ack duration: %+v", ack)
	}
	if arrival.Status != "NOT_RECORDED" || arrival.DurationMs != nil || arrival.WaitingMs == nil || *arrival.WaitingMs != 5500 {
		t.Fatalf("arrival falsely inferred: %+v", arrival)
	}
}

func TestLateCorrectionsCreateNewFactsWithoutChangingFrozenCutoff(t *testing.T) {
	exec := executionFixture()
	exec.Milestones = append(exec.Milestones, model.ResponseMilestone{ID: "ack-corrected", CorrectsID: "ack-1", StepID: "ack", Status: "DISPUTED", OccurredAt: 1800, RecordedAt: 9000, Source: "MANUAL", Explanation: "时钟有争议"})
	old, err := Evaluate(exec, "exec-v2", 8000)
	if err != nil {
		t.Fatal(err)
	}
	if old.CompletedRequiredSteps != 1 || old.Steps[0].MilestoneID != "ack-1" {
		t.Fatalf("late record leaked into cutoff: %+v", old)
	}
	newer, err := Evaluate(exec, "exec-v2", 10000)
	if err != nil {
		t.Fatal(err)
	}
	if newer.CompletedRequiredSteps != 0 || newer.Steps[0].Status != "DISPUTED" || newer.Steps[1].StartAt != 0 {
		t.Fatalf("disputed correction used as confirmed fact: %+v", newer)
	}
}

func TestNegativeElapsedTimeAndMissingEvidenceStayUnconfirmed(t *testing.T) {
	exec := executionFixture()
	exec.PlatformReceivedAt = 2800
	r, err := Evaluate(exec, "exec-v1", 8000)
	if err != nil {
		t.Fatal(err)
	}
	if r.Steps[0].Status != "TIME_CONFLICT" || r.Steps[0].DurationMs != nil || r.CompletedRequiredSteps != 0 {
		t.Fatalf("negative elapsed repaired: %+v", r)
	}
	exec = executionFixture()
	exec.Milestones[0].Evidence = nil
	r, err = Evaluate(exec, "exec-v1", 8000)
	if err != nil {
		t.Fatal(err)
	}
	if r.Steps[0].Status != "EVIDENCE_MISSING" || r.CompletedRequiredSteps != 0 || r.ConfirmedEvidence != 0 {
		t.Fatalf("evidence-free confirmation counted: %+v", r)
	}
}

func TestAmbiguousNodeAndCyclicProcedureAreRejected(t *testing.T) {
	exec := executionFixture()
	duplicate := exec.Milestones[0]
	duplicate.ID = "unlinked-second"
	exec.Milestones = append(exec.Milestones, duplicate)
	r, err := Evaluate(exec, "exec-v1", 8000)
	if err != nil {
		t.Fatal(err)
	}
	if r.Steps[0].Status != "DISPUTED" || r.CompletedRequiredSteps != 0 {
		t.Fatalf("ambiguous selected node: %+v", r)
	}
	p := procedureFixture()
	p.Steps[0].Prerequisites = []string{"arrival"}
	if ValidateProcedure(p) == nil {
		t.Fatal("accepted cyclic prerequisites")
	}
}

func TestUnconfiguredTargetAndZeroRequiredDenominatorsAreNotSuccess(t *testing.T) {
	exec := executionFixture()
	for i := range exec.Procedure.Steps {
		exec.Procedure.Steps[i].Required = false
		exec.Procedure.Steps[i].TargetMs = 0
	}
	r, err := Evaluate(exec, "exec-v1", 8000)
	if err != nil {
		t.Fatal(err)
	}
	if r.CompletionRate != nil || r.EvidenceCoverage != nil || r.Steps[0].TargetResult != "NOT_EVALUATED" {
		t.Fatalf("invented rate or regulatory target: %+v", r)
	}
}

func TestConfirmedChildRequiresQualifiedPredecessorAndRequiredEvidence(t *testing.T) {
	for _, parent := range []string{"MISSING", "UNVERIFIED", "DISPUTED", "NO_EVIDENCE"} {
		t.Run(parent, func(t *testing.T) {
			exec := executionFixture()
			exec.Procedure.Steps[1].ClockStart = "RUN_START"
			child := model.ResponseMilestone{ID: "child", StepID: "arrival", Status: "CONFIRMED", OccurredAt: 4000, RecordedAt: 5000}
			switch parent {
			case "MISSING":
				exec.Milestones = nil
			case "NO_EVIDENCE":
				exec.Milestones[0].Evidence = nil
			default:
				exec.Milestones[0].Status = parent
			}
			exec.Milestones = append(exec.Milestones, child)
			r, e := Evaluate(exec, "execution", 8000)
			if e != nil {
				t.Fatal(e)
			}
			v := r.Steps[1]
			if r.CompletedRequiredSteps != 0 || r.CompletionRate == nil || *r.CompletionRate != 0 || v.Status != "PREREQUISITE_UNCONFIRMED" || len(v.MissingEvidence) != 1 || v.WaitingMs == nil || *v.WaitingMs != 7000 {
				t.Fatal(r)
			}
		})
	}
}

func TestUncompletedRecordedNodesShowWaitingWithoutInventingCompletion(t *testing.T) {
	for _, status := range []string{"UNVERIFIED", "DISPUTED", "EVIDENCE_MISSING", "TIME_CONFLICT"} {
		t.Run(status, func(t *testing.T) {
			exec := executionFixture()
			switch status {
			case "EVIDENCE_MISSING":
				exec.Milestones[0].Evidence = nil
			case "TIME_CONFLICT":
				exec.PlatformReceivedAt = 2800
			default:
				exec.Milestones[0].Status = status
			}
			r, e := Evaluate(exec, "execution", 8000)
			if e != nil {
				t.Fatal(e)
			}
			v := r.Steps[0]
			if v.Status != status || v.WaitingMs == nil || *v.WaitingMs != 8000-exec.PlatformReceivedAt || r.CompletedRequiredSteps != 0 {
				t.Fatal(r)
			}
		})
	}
	// A confirmed actual operation may complete while its unavailable receipt
	// clock remains unknown; its elapsed duration and waiting remain uncomputed.
	exec := executionFixture()
	exec.PlatformReceivedAt = 0
	r, e := Evaluate(exec, "execution", 8000)
	if e != nil || r.CompletedRequiredSteps != 1 || r.Steps[0].DurationMs != nil || r.Steps[0].WaitingMs != nil {
		t.Fatal(r, e)
	}
}
