package response

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type rawEvidenceCatalog interface {
	GetRawIndex(context.Context, string, string) (model.RawArchiveIndex, error)
}

// BusinessEvidence resolves only persisted platform facts. In particular,
// device event time is never substituted for a missing platform receipt time.
func (s *Service) BusinessEvidence(ctx context.Context, a analytics.Actor, requested model.ResponseEvidenceReference) (model.ResponseEvidenceReference, error) {
	kind := s.EvidenceKind
	if kind == "" {
		kind = analytics.KindResponse
	}
	if kind != analytics.KindResponse && kind != analytics.KindMaintenance {
		return model.ResponseEvidenceReference{}, analytics.ErrForbidden
	}
	current, err := s.Analysis.Current(ctx, a)
	if err != nil || !current.Allows(kind, "", []string{requested.DeviceID}) {
		return model.ResponseEvidenceReference{}, analytics.ErrForbidden
	}
	if requested.SourceID == "" {
		return model.ResponseEvidenceReference{}, invalid("请指定具体生产记录")
	}
	if requested.Kind == "ATTACHMENT" {
		return s.AttachmentEvidence(ctx, current, requested)
	}
	requiredMenu := "menu:alarms"
	if requested.Kind == "RAW_MESSAGE" {
		requiredMenu = "menu:raw"
	}
	if !slices.Contains(current.Permissions, "*") && !slices.Contains(current.Permissions, requiredMenu) {
		return model.ResponseEvidenceReference{}, analytics.ErrForbidden
	}
	ref := model.ResponseEvidenceReference{Kind: requested.Kind, SourceID: requested.SourceID, DeviceID: requested.DeviceID, Classification: "PRODUCTION"}
	switch requested.Kind {
	case "ALARM":
		if s.Catalog == nil {
			return ref, analytics.ErrUnsupported
		}
		alarm, err := s.Catalog.GetAlarm(ctx, current.TenantID, requested.SourceID)
		if err != nil {
			return ref, err
		}
		if alarm.DeviceID != requested.DeviceID || alarm.TenantID != current.TenantID {
			return ref, analytics.ErrForbidden
		}
		ref.ID, ref.ResourceID, ref.Description = "alarm/"+alarm.ID, alarm.ID, "获授权的生产告警记录"
		return ref, nil
	case "RAW_MESSAGE":
		catalog, ok := s.Catalog.(rawEvidenceCatalog)
		if !ok {
			return ref, analytics.ErrUnsupported
		}
		index, err := catalog.GetRawIndex(ctx, current.TenantID, requested.SourceID)
		if err != nil {
			return ref, err
		}
		if index.TenantID != current.TenantID || index.DeviceID != requested.DeviceID {
			return ref, analytics.ErrForbidden
		}
		ref.ID, ref.ResourceID, ref.Description = "raw/"+index.MessageID, index.MessageID, "具体生产原始报文归档索引"
		ref.ReceivedAt, ref.RecordedAt = index.ReceivedAt, index.ArchivedAt
		return ref, nil
	case "ALARM_REPORT", "ALARM_LIFECYCLE":
		if s.Facts == nil {
			return ref, analytics.ErrUnsupported
		}
		found := false
		err = s.Facts.AnalyticsFactsRead(ctx, current.TenantID, func(reader ports.AnalyticsFactReader) error {
			query := model.FactQuery{DeviceIDs: []string{requested.DeviceID}, Start: 0, End: s.now() + 1, Limit: 1000}
			for count := 0; count < s.Analysis.Limits.RecordLimit; {
				var page model.FactPage[model.BusinessEventFact]
				var err error
				if requested.Kind == "ALARM_REPORT" {
					page, err = reader.ListAlarmReportEvents(query)
				} else {
					page, err = reader.ListAlarmLifecycleEvents(query)
				}
				if err != nil {
					return err
				}
				for _, event := range page.Items {
					if event.SourceEventID != requested.SourceID {
						continue
					}
					if event.DeviceID != requested.DeviceID {
						return analytics.ErrForbidden
					}
					if requested.Kind == "ALARM_LIFECYCLE" && !slices.Contains([]string{"ALARM_ACKNOWLEDGED", "ALARM_RECOVERED", "ALARM_CLOSED"}, event.Type) {
						return invalid("不是已提交的系统操作节点")
					}
					ref.ID, ref.ResourceID, ref.EventType = "event/"+event.SourceEventID, event.ResourceID, event.Type
					ref.OccurredAt, ref.RecordedAt = event.OccurredAt, event.RecordedAt
					ref.Description = fmt.Sprintf("生产事务事件 %s；仅证明记录的系统操作", event.Type)
					found = true
					return nil
				}
				count += len(page.Items)
				if page.Cursor == "" {
					return nil
				}
				query.Cursor = page.Cursor
			}
			return invalid("具体生产证据超过读取保护范围")
		})
		if err != nil {
			return ref, err
		}
		if !found {
			return ref, model.ErrNotFound
		}
		if requested.Kind == "ALARM_REPORT" {
			ref.ReceivedAt, err = s.reportReceipt(ctx, current, ref)
			if err != nil {
				return ref, err
			}
		}
		return ref, nil
	default:
		return ref, invalid("生产证据类型无效")
	}
}

// reportReceipt uses a specific committed report's embedded standard message
// and its archive identity. Aggregate alarm timestamps and newer reports can
// never supply the receipt time of the selected report.
func (s *Service) reportReceipt(ctx context.Context, a analytics.Actor, ref model.ResponseEvidenceReference) (int64, error) {
	journal, ok := s.Catalog.(ports.DutyStore)
	if !ok {
		return 0, nil
	}
	archive, ok := s.Catalog.(rawEvidenceCatalog)
	if !ok {
		return 0, nil
	}
	var message model.StandardMessage
	err := journal.DutyRead(ctx, a.TenantID, func(tx ports.DutyTx) error {
		for offset := 0; offset < s.Analysis.Limits.RecordLimit; {
			page, total, err := tx.Events(model.DutyFilter{DeviceIDs: []string{ref.DeviceID}, Limit: 100, Offset: offset})
			if err != nil {
				return err
			}
			for _, event := range page {
				if event.ID != ref.SourceID {
					continue
				}
				if event.ResourceID != ref.ResourceID || event.DeviceID != ref.DeviceID || !slices.Contains([]string{"ALARM_CREATED", "ALARM_REPORTED"}, event.Type) {
					return invalid("单次告警来源不一致")
				}
				var alarm model.Alarm
				if json.Unmarshal(event.Body, &alarm) != nil {
					return nil
				}
				body, err := json.Marshal(alarm.Details["message"])
				if err != nil {
					return err
				}
				if json.Unmarshal(body, &message) != nil || message.MessageID == "" || message.MessageID != alarm.TriggerID || message.TenantID != a.TenantID || message.DeviceID != ref.DeviceID {
					message = model.StandardMessage{}
				}
				return nil
			}
			offset += len(page)
			if offset >= total || len(page) == 0 {
				return nil
			}
		}
		return invalid("单次报警接收时钟证据超过读取保护范围")
	})
	if err != nil {
		return 0, err
	}
	if message.RawMessageID == "" {
		return 0, nil
	}
	index, err := archive.GetRawIndex(ctx, a.TenantID, message.RawMessageID)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	} // expired source remains unknown in the report
	if index.TenantID != a.TenantID || index.DeviceID != ref.DeviceID || index.ProductID != message.ProductID {
		return 0, invalid("单次上报原始归档身份不匹配")
	}
	return index.ReceivedAt, nil
}
