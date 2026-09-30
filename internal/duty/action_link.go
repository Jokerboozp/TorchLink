package duty

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// FollowUpSource is constructed by the source application's authorized
// service, never decoded directly from a browser. The duty item owns only the
// current shift's follow-up; completing it cannot verify the source action.
type FollowUpSource struct {
	Kind      string
	ID        string
	Version   int64
	DeviceIDs []string
	Title     string
	DueAt     int64
}

func (s *Service) LinkFollowUp(ctx context.Context, a Actor, source FollowUpSource, runID, ownerID, nextAction string) (model.DutyActionLink, error) {
	var out model.DutyActionLink
	if s.Store == nil || s.Resolve == nil {
		return out, invalid("值班持久化或身份解析不可用")
	}
	current, err := s.Resolve(ctx, a.TenantID, a.Username)
	if err != nil || !current.Enabled || !current.menu() || !current.can("item") || !current.covers(source.DeviceIDs) {
		return out, ErrForbidden
	}
	if !slices.Contains([]string{"CORRECTIVE_ACTION", "MAINTENANCE_RECORD"}, source.Kind) || source.ID == "" || source.Version < 1 || len(source.DeviceIDs) == 0 || source.Title == "" || runID == "" || nextAction == "" {
		return out, invalid("主业务对象或当班跟进内容无效")
	}
	body, _ := json.Marshal(Item{RunID: runID, OwnerID: ownerID, Title: "当班跟进：" + source.Title, SourceID: source.Kind + "/" + source.ID, NextAction: nextAction, DueAt: source.DueAt})
	command := Command{Kind: model.DutyItemKind, Operation: "create", Body: body}
	actors, err := s.accountMap(ctx, current, command)
	if err != nil {
		return out, err
	}
	id := "source-link-" + digest([]string{source.Kind, source.ID, runID})
	err = s.Store.DutyTransaction(ctx, a.TenantID, func(tx ports.DutyTx) error {
		runDoc, run, err := read[Run](tx, model.DutyRunKind, runID)
		if err != nil {
			return err
		}
		if !runMember(current, run) || !current.covers(run.DeviceIDs) || run.Status != "ACTIVE" {
			return ErrForbidden
		}
		for _, device := range source.DeviceIDs {
			if !contains(run.DeviceIDs, device) {
				return ErrForbidden
			}
		}
		if doc, err := tx.Get(model.DutyActionLinkKind, id); err == nil {
			out, err = model.DutyBody[model.DutyActionLink](doc)
			return err
		} else if !errors.Is(err, model.ErrNotFound) {
			return err
		}
		// Reuse the normal item transaction, membership, history and notification.
		value, err := s.item(tx, current, command, actors)
		if err != nil {
			return err
		}
		item, ok := value.(model.DutyDocument)
		if !ok {
			return invalid("值班跟进保存结果无效")
		}
		out = model.DutyActionLink{SourceKind: source.Kind, SourceResourceID: source.ID, RunID: runDoc.ID, StationID: run.StationID, DeviceIDs: slices.Clone(source.DeviceIDs), DutyItemID: item.ID, SourceVersion: source.Version, LinkedBy: current.Username, LinkedAt: s.now()}
		_, err = tx.Put(model.NewDutyDocument(model.DutyActionLinkKind, id, out), 0)
		return err
	})
	return out, err
}

// FollowUpLinks applies the existing duty visibility checks to every record
// before pagination or totals. The caller separately authorizes the source.
func (s *Service) FollowUpLinks(ctx context.Context, a Actor, sourceKind, sourceID string) ([]model.DutyActionLink, error) {
	if s.Store == nil || s.Resolve == nil {
		return nil, invalid("值班持久化或身份解析不可用")
	}
	current, err := s.Resolve(ctx, a.TenantID, a.Username)
	if err != nil || !current.Enabled || !current.menu() {
		return nil, ErrForbidden
	}
	result := []model.DutyActionLink{}
	err = s.Store.DutyRead(ctx, a.TenantID, func(tx ports.DutyTx) error {
		for offset := 0; offset < 10000; {
			page, total, err := tx.List(model.DutyFilter{Kind: model.DutyActionLinkKind, Limit: 100, Offset: offset})
			if err != nil {
				return err
			}
			for _, doc := range page {
				link, err := model.DutyBody[model.DutyActionLink](doc)
				if err != nil {
					return err
				}
				if link.SourceKind == sourceKind && link.SourceResourceID == sourceID && s.readable(tx, current, doc) {
					result = append(result, link)
				}
			}
			offset += len(page)
			if offset >= total || len(page) == 0 {
				return nil
			}
		}
		return invalid("当班关联超过读取保护范围")
	})
	return result, err
}
