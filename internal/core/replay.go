package core

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (e *Engine) StartReplay(ctx context.Context, req model.ReplayRequest) (model.ReplayRequest, error) {
	if req.TenantID == "" || req.Start <= 0 || req.End <= req.Start {
		return req, fmt.Errorf("tenantId and a valid start/end range are required")
	}
	switch req.Mode {
	case "DRY_RUN", "REINGEST", "DIFF":
	default:
		return req, fmt.Errorf("mode must be DRY_RUN, REINGEST or DIFF")
	}
	if req.RatePerSecond <= 0 {
		req.RatePerSecond = 100
	}
	req.ID = id("replay")
	req.Status = "PENDING"
	req.CreatedAt = e.Clock.Now().UnixMilli()
	if err := e.Repo.SaveReplay(ctx, req); err != nil {
		return req, err
	}
	_ = e.Repo.SaveAudit(ctx, model.AuditLog{ID: id("audit"), TenantID: req.TenantID, Actor: req.CreatedBy, Action: "replay.create", TargetType: "replay", TargetID: req.ID, Details: map[string]any{"mode": req.Mode, "start": req.Start, "end": req.End}, CreatedAt: req.CreatedAt})
	go e.runReplay(context.Background(), req)
	return req, nil
}
func (e *Engine) runReplay(ctx context.Context, req model.ReplayRequest) {
	req.Status = "RUNNING"
	_ = e.Repo.UpdateReplay(ctx, req)
	ticker := time.NewTicker(time.Second / time.Duration(req.RatePerSecond))
	defer ticker.Stop()
	offset := 0
	req.DiffSummary = map[string]int{"unchanged": 0, "changed": 0, "missing": 0, "errors": 0}
	for {
		indexes, err := e.Repo.ListRawIndexes(ctx, ports.RawFilter{TenantID: req.TenantID, ProductID: req.ProductID, DeviceID: req.DeviceID, Start: req.Start, End: req.End, Limit: 500, Offset: offset})
		if err != nil {
			e.Log.Error("list replay archive indexes", "replayId", req.ID, "error", err)
			req.Status = "FAILED"
			req.Failed++
			break
		}
		if len(indexes) == 0 {
			req.Status = "COMPLETED"
			break
		}
		for _, idx := range indexes {
			<-ticker.C
			raw, err := e.GetRaw(ctx, idx)
			if err != nil {
				e.Log.Error("read replay archive", "replayId", req.ID, "messageId", idx.MessageID, "bucket", idx.ObjectBucket, "key", idx.ObjectKey, "error", err)
				req.Failed++
				continue
			}
			raw.ParserVersion = req.ParserVersion
			switch req.Mode {
			case "REINGEST":
				b, _ := json.Marshal(raw)
				err = e.Bus.Publish(ctx, model.TopicRaw, raw.DeviceID, b)
			case "DRY_RUN":
				_, err = e.parseReplay(ctx, raw, req.ParserVersion)
			case "DIFF":
				var current *model.StandardMessage
				current, err = e.parseReplay(ctx, raw, req.ParserVersion)
				diff := model.ReplayDiff{RawMessageID: raw.MessageID, Current: current}
				if err == nil {
					previous, previousErr := e.Repo.GetStandardMessageByRaw(ctx, req.TenantID, raw.MessageID)
					if previousErr != nil {
						diff.Status = "MISSING"
						req.DiffSummary["missing"]++
					} else {
						diff.Previous = &previous
						if equivalentMessage(previous, *current) {
							diff.Status = "UNCHANGED"
							req.DiffSummary["unchanged"]++
						} else {
							diff.Status = "CHANGED"
							req.DiffSummary["changed"]++
						}
					}
				}
				if err != nil {
					diff.Status = "ERROR"
					diff.Error = err.Error()
					req.DiffSummary["errors"]++
				}
				if len(req.Diffs) < 1000 && diff.Status != "UNCHANGED" {
					req.Diffs = append(req.Diffs, diff)
				}
			}
			if err != nil {
				e.Log.Error("process replay message", "replayId", req.ID, "messageId", idx.MessageID, "mode", req.Mode, "error", err)
				req.Failed++
			} else {
				req.Processed++
			}
		}
		offset += len(indexes)
		_ = e.Repo.UpdateReplay(ctx, req)
		if len(indexes) < 500 {
			req.Status = "COMPLETED"
			break
		}
	}
	req.CompletedAt = e.Clock.Now().UnixMilli()
	_ = e.Repo.UpdateReplay(ctx, req)
}

func (e *Engine) parseReplay(ctx context.Context, raw model.RawMessage, version string) (*model.StandardMessage, error) {
	protocolID, releaseVersion := raw.ProtocolID, raw.ProtocolVersion
	if binding, bindingErr := e.Repo.GetProductProtocolBinding(ctx, raw.TenantID, raw.ProductID); bindingErr == nil {
		if protocolID == "" {
			protocolID = binding.ProtocolID
		}
		if releaseVersion == "" {
			releaseVersion = binding.Version
		}
	}
	if version != "" && protocolID != "" {
		releaseVersion = version
	}
	if protocolID != "" && releaseVersion != "" {
		release, releaseErr := e.Repo.GetProtocolRelease(ctx, raw.TenantID, protocolID, releaseVersion)
		if releaseErr != nil {
			return nil, releaseErr
		}
		if release.Status == "REVOKED" {
			return nil, fmt.Errorf("protocol release %s@%s is revoked", protocolID, releaseVersion)
		}
		raw.ProtocolID, raw.ProtocolVersion, raw.PointTableVersion = protocolID, releaseVersion, release.PointTableVersion
		return e.Parsers.ParseWithConfig(release.ParserType, release.Config, raw)
	}
	product, err := e.Repo.GetProduct(ctx, raw.TenantID, raw.ProductID)
	if err == nil && product.ProtocolPackageID != "" {
		pkg, pkgErr := e.Repo.GetProtocolPackage(ctx, raw.TenantID, product.ProtocolPackageID)
		if pkgErr == nil {
			return e.Parsers.ParseVersionWithConfig(pkg.ParserType, version, pkg.Config, raw)
		}
	}
	if version != "" {
		return nil, fmt.Errorf("cannot select parser version %s without a product protocol package", version)
	}
	return e.Parsers.Parse(raw)
}
func equivalentMessage(a, b model.StandardMessage) bool {
	return a.MessageType == b.MessageType && equivalentMap(a.Properties, b.Properties) && equivalentMap(a.Event, b.Event) && equivalentTags(a.Tags, b.Tags)
}
func equivalentMap(a, b map[string]any) bool {
	return len(a) == 0 && len(b) == 0 || reflect.DeepEqual(a, b)
}
func equivalentTags(a, b map[string]string) bool {
	return len(a) == 0 && len(b) == 0 || reflect.DeepEqual(a, b)
}
