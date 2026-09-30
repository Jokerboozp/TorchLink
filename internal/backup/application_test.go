package backup

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/maintenance"
	"iot-platform/internal/model"
)

func applicationTestDocument(kind, id, run string, version int64, body any) applicationDocument {
	raw, _ := json.Marshal(body)
	return applicationDocument{Tenant: "fixture-tenant", Kind: kind, ID: id, RunID: run, Version: version, DeviceIDs: []string{"device"}, CreatedAt: 10, Body: raw}
}
func applicationTestConfig(id, kind, resource string, body any) []applicationDocument {
	raw, _ := json.Marshal(body)
	hash, _ := analytics.AnalysisHash(json.RawMessage(raw))
	v := model.AnalysisConfigRevision{ID: id, TenantID: "fixture-tenant", Kind: kind, ResourceID: resource, Version: 1, DeviceIDs: []string{"device"}, Scope: "SHARED", Creator: "operator", Body: raw, Hash: hash, CreatedAt: 10}
	d := applicationTestDocument("config", id, "", 1, v)
	d.ApplicationKind = kind
	d.ResourceID = resource
	pointer, _ := analytics.AnalysisHash(struct{ Kind, Resource, Scope, Owner string }{kind, resource, "SHARED", ""})
	return []applicationDocument{d, applicationTestDocument("config-pointer", pointer, "", 1, struct{ ID string }{id})}
}
func applicationTestDocuments() []applicationDocument {
	documents := applicationTestConfig("procedure-revision", "RESPONSE_PROCEDURE", "procedure", model.ResponseProcedure{Name: "procedure"})
	documents = append(documents, applicationTestConfig("execution-revision", "RESPONSE_EXECUTION", "drill", model.ResponseExecution{Name: "drill", Source: "DRILL", Status: "ENDED", ProcedureRevisionID: "procedure-revision"})...)
	run := model.AnalysisRun{ID: "fixed-run", TenantID: "fixture-tenant", Kind: analytics.KindResponse, Creator: "operator", DeviceIDs: []string{"device"}, Start: 100, End: 200, ConfigurationVersion: "fixed", AlgorithmVersion: "v1", Parameters: json.RawMessage(`{"executionRevisionId":"execution-revision"}`), Status: model.AnalysisSucceeded, Version: 2, CreatedAt: 10, UpdatedAt: 20, CompletedAt: 20, SnapshotID: "fixed-snapshot", InputsFrozen: true, InputHashes: []string{"hash"}, Sources: []model.AnalysisSourceCoverage{}, DataCutoff: 200}
	rd := applicationTestDocument("run", run.ID, run.ID, run.Version, run)
	rd.ApplicationKind = run.Kind
	rd.Status = run.Status
	documents = append(documents, rd)
	output := model.AnalysisOutput{ID: "finding", TenantID: run.TenantID, RunID: run.ID, Kind: "findings", DeviceID: "device", Body: json.RawMessage(`{"quality":"confirmed"}`)}
	od := applicationTestDocument("output", output.ID, run.ID, 1, output)
	od.ApplicationKind = output.Kind
	od.DeviceID = output.DeviceID
	documents = append(documents, od)
	snap := model.AnalysisSnapshot{ID: run.SnapshotID, TenantID: run.TenantID, RunID: run.ID, Version: 1, DeviceIDs: run.DeviceIDs, Start: run.Start, End: run.End, DataCutoff: run.DataCutoff, InputHashes: run.InputHashes, Sources: run.Sources, Statistics: json.RawMessage(`{"count":1}`)}
	snap.FactsHash, _ = analytics.AnalysisHash(struct {
		Snapshot model.AnalysisSnapshot
		Facts    []json.RawMessage
	}{snap, []json.RawMessage{od.Body}})
	snap.CreatedAt = 20
	documents = append(documents, applicationTestDocument("snapshot", snap.ID, run.ID, 1, snap))
	result := model.AnalysisAIResult{Summary: "固定事实解读", ObservedBottlenecks: []model.AnalysisAIStatement{{Text: "检查事实", FactIDs: []string{output.ID}, DeviceIDs: []string{"device"}}}, Coverage: model.AnalysisAICoverage{SummaryProvided: true, OutputCount: 1, TotalOutputs: 1}}
	interpretation, _ := json.Marshal(map[string]any{"summary": result.Summary, "observedBottlenecks": result.ObservedBottlenecks, "evidenceGaps": []model.AnalysisAIStatement{}, "improvementSuggestions": []model.AnalysisAIStatement{}, "limitations": []model.AnalysisAIStatement{}, "coverage": result.Coverage})
	job := model.AnalysisAIRevision{ID: "completed-ai", TenantID: run.TenantID, RunID: run.ID, SnapshotID: snap.ID, SnapshotVersion: 1, WorkflowID: analytics.WorkflowResponse, Kind: run.Kind, DeviceIDs: run.DeviceIDs, Status: model.AnalysisSucceeded, Version: 3, CreatedAt: 10, CompletedAt: 20, Interpretation: interpretation, FactIDs: []string{output.ID}, SentFactIDs: []string{output.ID}}
	jd := applicationTestDocument("ai", job.ID, run.ID, job.Version, job)
	jd.ApplicationKind = job.WorkflowID
	jd.Status = job.Status
	documents = append(documents, jd)
	job.ID = "pending-ai"
	job.Status = model.AnalysisRunning
	job.Version = 2
	job.LeaseOwner = "previous-worker"
	job.LeaseToken = 5
	job.LeaseExpiresAt = 9999999999999
	job.Deadline = 9999999999999
	job.Interpretation = nil
	job.FactIDs = nil
	job.CompletedAt = 0
	jd = applicationTestDocument("ai", job.ID, run.ID, job.Version, job)
	jd.ApplicationKind = job.WorkflowID
	jd.Status = job.Status
	documents = append(documents, jd)
	pending := model.AnalysisRun{ID: "pending-run", TenantID: run.TenantID, Kind: analytics.KindDataQuality, Creator: "operator", DeviceIDs: run.DeviceIDs, Start: 100, End: 200, ConfigurationVersion: "fixed", AlgorithmVersion: "v1", Parameters: json.RawMessage(`{}`), Status: model.AnalysisRunning, Version: 4, LeaseOwner: "previous-worker", LeaseToken: 11, LeaseExpiresAt: 9999999999999, StartedAt: 100}
	pd := applicationTestDocument("run", pending.ID, pending.ID, pending.Version, pending)
	pd.Status = pending.Status
	pd.ApplicationKind = pending.Kind
	documents = append(documents, pd)
	return documents
}
func TestApplicationGraphRejectsHashPointerVersionAndAIReferenceCorruption(t *testing.T) {
	if err := validateApplicationDocuments(applicationTestDocuments()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"body-hash", "config-envelope-version", "pointer-missing", "pointer-version", "run-version", "snapshot-digest", "output-filter-kind", "AI-workflow-kind", "unsent-ai-reference", "AI-object-scope", "fixed-config-ref"} {
		t.Run(name, func(t *testing.T) {
			docs := applicationTestDocuments()
			switch name {
			case "body-hash":
				v, _ := decodeApplication[model.AnalysisConfigRevision](docs[0])
				v.Body = json.RawMessage(`{"name":"changed"}`)
				docs[0].Body, _ = json.Marshal(v)
			case "config-envelope-version":
				docs[0].Version++
			case "pointer-missing":
				docs = slices.Delete(docs, 1, 2)
			case "pointer-version":
				docs[1].Version++
			case "run-version":
				docs[4].Version++
			case "snapshot-digest":
				v, _ := decodeApplication[model.AnalysisSnapshot](docs[6])
				v.FactsHash = "false"
				docs[6].Body, _ = json.Marshal(v)
			case "output-filter-kind":
				docs[5].ApplicationKind = "input-manifest"
			case "AI-workflow-kind":
				v, _ := decodeApplication[model.AnalysisAIRevision](docs[7])
				v.Kind = analytics.KindMonitoring
				docs[7].Body, _ = json.Marshal(v)
			case "unsent-ai-reference":
				v, _ := decodeApplication[model.AnalysisAIRevision](docs[7])
				v.SentFactIDs = []string{"fixed-snapshot/summary"}
				docs[7].Body, _ = json.Marshal(v)
			case "AI-object-scope":
				v, _ := decodeApplication[model.AnalysisAIRevision](docs[7])
				var result model.AnalysisAIResult
				_ = json.Unmarshal(v.Interpretation, &result)
				result.ObservedBottlenecks[0].DeviceIDs = []string{"other-device"}
				v.Interpretation, _ = json.Marshal(result)
				docs[7].Body, _ = json.Marshal(v)
			case "fixed-config-ref":
				v, _ := decodeApplication[model.AnalysisRun](docs[4])
				v.Parameters = json.RawMessage(`{"executionRevisionId":"missing"}`)
				docs[4].Body, _ = json.Marshal(v)
			}
			if err := validateApplicationDocuments(docs); err == nil {
				t.Fatal("corrupt immutable graph accepted")
			}
		})
	}
}
func TestApplicationSchemaIsCompleteExactWhitelist(t *testing.T) {
	schema := knowledgeSchema{}
	for _, name := range applicationTables() {
		spec := applicationSpecs[name]
		table := knowledgeTable{Name: name, PrimaryKey: spec.PrimaryKey}
		for _, column := range spec.Columns {
			table.Columns = append(table.Columns, column)
		}
		schema.Tables = append(schema.Tables, table)
	}
	if err := validateApplicationSchema(schema); err != nil {
		t.Fatal(err)
	}
	schema.Tables[0].Columns[0].Type = "text);DROP SCHEMA public CASCADE;--"
	if validateApplicationSchema(schema) == nil {
		t.Fatal("untrusted executable type accepted")
	}
}

func applicationMaintenanceDocuments() []applicationDocument {
	docs := applicationTestConfig("asset-revision", maintenance.AssetKind, "asset", model.AssetInstance{DeviceID: "device", PhysicalID: "fixture-physical"})
	docs = append(docs, applicationTestConfig("work-revision", maintenance.InterventionKind, "work", model.MaintenanceIntervention{AssetRevisionID: "asset-revision", Status: "COMPLETED", StartedAt: 140, EndedAt: 150})...)
	docs = append(docs, applicationTestConfig("actual-cost-revision", maintenance.CostKind, "actual-cost", model.MaintenanceCost{Type: "ACTUAL", SourceKind: maintenance.AssetKind, SourceID: "asset", Currency: "CNY", Material: stringPointer("100.00")})...)
	quality := model.AnalysisRun{ID: "source-quality-run", TenantID: "fixture-tenant", Kind: analytics.KindDataQuality, DeviceIDs: []string{"device"}, Start: 100, End: 200, Status: model.AnalysisSucceeded, Version: 2, SnapshotID: "source-quality-snapshot", InputsFrozen: true, InputHashes: []string{"quality-input"}, Sources: []model.AnalysisSourceCoverage{}, DataCutoff: 200, Parameters: json.RawMessage(`{}`)}
	qd := applicationTestDocument("run", quality.ID, quality.ID, quality.Version, quality)
	qd.ApplicationKind, qd.Status = quality.Kind, quality.Status
	qs := model.AnalysisSnapshot{ID: quality.SnapshotID, TenantID: quality.TenantID, RunID: quality.ID, Version: 1, DeviceIDs: quality.DeviceIDs, Start: quality.Start, End: quality.End, InputHashes: quality.InputHashes, Sources: quality.Sources, DataCutoff: quality.DataCutoff, Statistics: json.RawMessage(`{"count":1}`)}
	qs.FactsHash, _ = analytics.AnalysisHash(struct {
		Snapshot model.AnalysisSnapshot
		Facts    []json.RawMessage
	}{qs, []json.RawMessage{}})
	docs = append(docs, qd, applicationTestDocument("snapshot", qs.ID, quality.ID, 1, qs))
	config := func(id string) model.AnalysisConfigRevision {
		for _, d := range docs {
			if d.Kind == "config" && d.ID == id {
				v, _ := decodeApplication[model.AnalysisConfigRevision](d)
				return v
			}
		}
		panic("fixture configuration missing")
	}
	required := []string{analytics.FinanceReadOperation, analytics.QualityReadPermission}
	p := model.MaintenanceRunParameters{UseFinance: true, Observation: &model.MaintenanceObservationParameters{InterventionRevisionID: "work-revision", BeforeAssetRevisionID: "asset-revision", AfterAssetRevisionID: "asset-revision", ComparisonType: "SAME_INSTANCE_REPAIR", Before: model.FactRange{Start: 100, End: 140}, After: model.FactRange{Start: 150, End: 200}, QualityRunIDs: []string{quality.ID}}}
	b := restoredMaintenanceBusiness{Parameters: p, Configs: []model.AnalysisConfigRevision{config("work-revision"), config("asset-revision")}, Snapshots: []model.AnalysisSnapshot{qs}, RequiredPermissions: required}
	m := restoredMaintenanceManifest{Version: "maintenance-facts-v1", Business: b, DataCutoff: 200, Sources: []model.AnalysisSourceCoverage{}, Costs: []model.AnalysisConfigRevision{config("actual-cost-revision")}}
	docs, observation := appendApplicationMaintenanceRun(docs, "financial-observation", analytics.KindMaintenance, m)
	var observed model.AnalysisSnapshot
	for _, d := range docs {
		if d.Kind == "snapshot" && d.RunID == observation.ID {
			observed, _ = decodeApplication[model.AnalysisSnapshot](d)
		}
	}
	scenario := model.InvestmentScenario{Name: "fixed funded scenario", UseFinance: true, RequiredPermissions: required, Currency: "CNY", Budget: stringPointer("200.00"), PlanningStart: 100, PlanningEnd: 200, Candidates: []model.InvestmentCandidate{{ID: "candidate", AssetRevisionID: "asset-revision", AssetID: "asset", ObservationRunID: observation.ID}}}
	docs = append(docs, applicationTestConfig("scenario-revision", maintenance.ScenarioKind, "scenario", scenario)...)
	b = restoredMaintenanceBusiness{Parameters: model.MaintenanceRunParameters{UseFinance: true, ScenarioRevisionID: "scenario-revision"}, Configs: []model.AnalysisConfigRevision{config("scenario-revision"), config("asset-revision")}, Snapshots: []model.AnalysisSnapshot{observed}, RequiredPermissions: required}
	docs, _ = appendApplicationMaintenanceRun(docs, "financial-investment", analytics.KindInvestment, restoredMaintenanceManifest{Version: "maintenance-facts-v1", Business: b, DataCutoff: 200, Sources: []model.AnalysisSourceCoverage{}})
	return docs
}

func stringPointer(v string) *string { return &v }

func appendApplicationMaintenanceRun(docs []applicationDocument, id, kind string, manifest restoredMaintenanceManifest) ([]applicationDocument, model.AnalysisRun) {
	refs := []string{}
	for _, v := range manifest.Business.Configs {
		refs = append(refs, fmt.Sprintf("config:%s:%s", v.ID, v.Hash))
	}
	for _, v := range manifest.Business.Snapshots {
		refs = append(refs, fmt.Sprintf("snapshot:%s:%d:%s", v.ID, v.Version, v.FactsHash))
	}
	slices.Sort(refs)
	refs = slices.Compact(refs)
	manifest.Business.SourceConfigurationHash, _ = analytics.AnalysisHash([]any{manifest.Business.Parameters, refs, manifest.Business.RequiredPermissions})
	manifestHash, _ := analytics.AnalysisHash(manifest)
	p, _ := json.Marshal(manifest.Business.Parameters)
	run := model.AnalysisRun{ID: id, TenantID: "fixture-tenant", Kind: kind, Creator: "operator", DeviceIDs: []string{"device"}, Start: 100, End: 200, ConfigurationVersion: manifest.Business.SourceConfigurationHash, RequiredPermissions: manifest.Business.RequiredPermissions, Parameters: p, Status: model.AnalysisSucceeded, Version: 3, SnapshotID: id + ":snapshot", InputsFrozen: true, InputHashes: []string{manifest.Business.SourceConfigurationHash, manifestHash}, Sources: manifest.Sources, DataCutoff: manifest.DataCutoff}
	rd := applicationTestDocument("run", id, id, run.Version, run)
	rd.ApplicationKind, rd.Status = kind, run.Status
	body, _ := json.Marshal(manifest)
	output := model.AnalysisOutput{ID: id + ":input-manifest", TenantID: run.TenantID, RunID: id, Kind: "input-manifest", Body: body}
	od := applicationTestDocument("output", output.ID, id, 1, output)
	od.ApplicationKind = output.Kind
	funds := model.AnalysisOutput{ID: id + ":budget", TenantID: run.TenantID, RunID: id, Kind: "budget-lines", Body: json.RawMessage(`{"amount":"100.00"}`)}
	fd := applicationTestDocument("output", funds.ID, id, 1, funds)
	fd.ApplicationKind = funds.Kind
	snapshot := model.AnalysisSnapshot{ID: run.SnapshotID, TenantID: run.TenantID, RunID: id, Version: 1, DeviceIDs: run.DeviceIDs, Start: run.Start, End: run.End, DataCutoff: run.DataCutoff, InputHashes: run.InputHashes, Sources: run.Sources, Statistics: json.RawMessage(`{"useFinance":true}`)}
	facts := []json.RawMessage{od.Body, fd.Body}
	slices.SortFunc(facts, func(a, b json.RawMessage) int {
		x, _ := analytics.AnalysisHash(a)
		y, _ := analytics.AnalysisHash(b)
		return strings.Compare(x, y)
	})
	snapshot.FactsHash, _ = analytics.AnalysisHash(struct {
		Snapshot model.AnalysisSnapshot
		Facts    []json.RawMessage
	}{snapshot, facts})
	return append(docs, rd, od, fd, applicationTestDocument("snapshot", snapshot.ID, id, 1, snapshot)), run
}

func TestApplicationFixedMaintenanceManifestPreventsPermissionDowngrade(t *testing.T) {
	if err := validateApplicationDocuments(applicationMaintenanceDocuments()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"clear-both-finance-fields", "clear-inherited-quality", "source-configuration-hash", "source-config-body", "source-scenario-permissions", "manifest-body"} {
		t.Run(name, func(t *testing.T) {
			docs := applicationMaintenanceDocuments()
			for i, d := range docs {
				if d.Kind == "run" && d.ID == "financial-observation" {
					run, _ := decodeApplication[model.AnalysisRun](d)
					switch name {
					case "clear-both-finance-fields":
						var p model.MaintenanceRunParameters
						_ = json.Unmarshal(run.Parameters, &p)
						p.UseFinance = false
						run.Parameters, _ = json.Marshal(p)
						run.RequiredPermissions = []string{analytics.QualityReadPermission}
					case "clear-inherited-quality":
						run.RequiredPermissions = []string{analytics.FinanceReadOperation}
					case "source-configuration-hash":
						run.ConfigurationVersion = "changed"
					}
					docs[i].Body, _ = json.Marshal(run)
				}
				if d.Kind == "config" && ((d.ID == "asset-revision" && name == "source-config-body") || (d.ID == "scenario-revision" && name == "source-scenario-permissions")) {
					config, _ := decodeApplication[model.AnalysisConfigRevision](d)
					if name == "source-config-body" {
						config.Body = json.RawMessage(`{"deviceId":"device","physicalId":"changed"}`)
					} else {
						var scenario model.InvestmentScenario
						_ = json.Unmarshal(config.Body, &scenario)
						scenario.RequiredPermissions = nil
						config.Body, _ = json.Marshal(scenario)
					}
					config.Hash, _ = analytics.AnalysisHash(config.Body)
					docs[i].Body, _ = json.Marshal(config)
				}
				if d.Kind == "output" && d.ID == "financial-observation:input-manifest" && name == "manifest-body" {
					output, _ := decodeApplication[model.AnalysisOutput](d)
					var m restoredMaintenanceManifest
					_ = json.Unmarshal(output.Body, &m)
					m.Business.RequiredPermissions = nil
					output.Body, _ = json.Marshal(m)
					docs[i].Body, _ = json.Marshal(output)
				}
			}
			if err := validateApplicationDocuments(docs); err == nil {
				t.Fatal("fixed monetary/quality results lost their original permission binding")
			}
		})
	}
}
