package duty

import (
	"context"
	"sort"
	"strconv"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func (s *Service) readOperation(ctx context.Context, a Actor, c Command) (any, error) {
	var out any
	err := s.Store.DutyRead(ctx, a.TenantID, func(tx ports.DutyTx) error {
		switch c.Operation {
		case "current":
			runs, e := listAll(tx, model.DutyFilter{Kind: model.DutyRunKind})
			if e != nil {
				return e
			}
			current := []model.DutyDocument{}
			for _, d := range runs {
				r, _ := model.DutyBody[Run](d)
				if r.Status != "ENDED" && contains(r.MemberIDs, a.Username) && s.readable(tx, a, d) {
					current = append(current, d)
				}
			}
			rosters, e := listAll(tx, model.DutyFilter{Kind: model.DutyRosterKind})
			if e != nil {
				return e
			}
			planned := []model.DutyDocument{}
			for _, d := range rosters {
				r, _ := model.DutyBody[Roster](d)
				if r.Status == "PUBLISHED" && r.EndAt > s.now() && contains(r.MemberIDs, a.Username) && s.readable(tx, a, d) {
					planned = append(planned, d)
				}
			}
			sort.Slice(planned, func(i, j int) bool {
				ri, _ := model.DutyBody[Roster](planned[i])
				rj, _ := model.DutyBody[Roster](planned[j])
				return ri.StartAt < rj.StartAt
			})
			out = map[string]any{"items": current, "rosters": planned}
			return nil
		case "delta":
			doc, h, e := read[Handover](tx, model.DutyHandoverKind, c.ID)
			if e != nil {
				return e
			}
			if !s.readable(tx, a, doc) {
				return ErrForbidden
			}
			_, rev, e := read[Revision](tx, model.DutyRevisionKind, h.CurrentRevisionID)
			if e != nil {
				return e
			}
			snapshot, records, items, e := s.latestFacts(tx, h)
			if e != nil {
				return e
			}
			events, e := eventsAll(tx, model.DutyFilter{DeviceIDs: h.DeviceIDs})
			if e != nil {
				return e
			}
			changed := []model.DutyBusinessEvent{}
			for _, ev := range events {
				if ev.Source != "duty" && !contains(rev.KnownEventIDs, ev.ID) {
					changed = append(changed, ev)
				}
			}
			total := len(changed)
			if len(changed) > 100 {
				changed = changed[len(changed)-100:]
			}
			hash, e := factsHash(tx, h, snapshot, records, items)
			if e != nil {
				return e
			}
			out = map[string]any{"snapshot": snapshot, "snapshotHash": hash, "changed": hash != rev.SnapshotHash, "events": changed, "eventTotal": total, "records": records, "items": items, "cutoffAt": snapshot.CutoffAt}
			return nil
		case "events":
			doc, e := tx.Get(model.DutyItemKind, c.ID)
			if e != nil {
				return e
			}
			if !s.readable(tx, a, doc) {
				return ErrForbidden
			}
			docs, e := listAll(tx, model.DutyFilter{Kind: model.DutyItemEventKind})
			if e != nil {
				return e
			}
			matched := []model.DutyDocument{}
			for _, d := range docs {
				v, _ := model.DutyBody[ItemEvent](d)
				if v.ItemID == c.ID {
					matched = append(matched, d)
				}
			}
			out = Result{Items: matched, Total: len(matched)}
			return nil
		case "query":
			f, e := decode[model.DutyFilter](c.Body)
			if e != nil {
				return e
			}
			if f.StationID != "" {
				doc, e := tx.Get(model.DutyStationKind, f.StationID)
				if e != nil {
					return e
				}
				if !s.readable(tx, a, doc) {
					return ErrForbidden
				}
				_, st, _ := read[Station](tx, model.DutyStationKind, f.StationID)
				f.DeviceIDs = st.DeviceIDs
			} else if !a.AllDevices && !a.Admin {
				f.DeviceIDs = a.AllowedDeviceIDs
			}
			if f.RunID != "" {
				doc, e := tx.Get(model.DutyRunKind, f.RunID)
				if e != nil {
					return e
				}
				if !s.readable(tx, a, doc) {
					return ErrForbidden
				}
			}
			limit, offset := f.Limit, f.Offset
			if limit <= 0 {
				limit = 20
			}
			if limit > 100 {
				limit = 100
			}
			if offset < 0 {
				offset = 0
			}
			events, e := eventsAll(tx, f)
			if e != nil {
				return e
			}
			visible := []model.DutyBusinessEvent{}
			for _, ev := range events {
				if ev.Source == "duty" {
					if s.readable(tx, a, model.DutyDocument{ID: ev.ResourceID, Body: ev.Body}) {
						visible = append(visible, ev)
					}
					continue
				}
				if ev.DeviceID != "" {
					if a.covers([]string{ev.DeviceID}) {
						visible = append(visible, ev)
					}
				} else if ev.StationID != "" {
					doc, e := tx.Get(model.DutyStationKind, ev.StationID)
					if e == nil && s.readable(tx, a, doc) {
						visible = append(visible, ev)
					}
				} else if a.Admin || a.can("history") {
					visible = append(visible, ev)
				}
			}
			total := len(visible)
			if offset >= total {
				visible = []model.DutyBusinessEvent{}
			} else {
				end := offset + limit
				if end > total {
					end = total
				}
				visible = visible[offset:end]
			}
			out = map[string]any{"items": visible, "total": total}
			return nil
		}
		return invalid("未知只读值班操作")
	})
	return out, err
}

// RefreshReminders is idempotent and can run on every replica. It creates
// durable notifications without starting AI or changing actual responsibility.
func (s *Service) RefreshReminders(ctx context.Context, tenant string) error {
	return s.Store.DutyTransaction(ctx, tenant, func(tx ports.DutyTx) error {
		stations, e := listAll(tx, model.DutyFilter{Kind: model.DutyStationKind})
		if e != nil {
			return e
		}
		now := s.now()
		for _, doc := range stations {
			st, _ := model.DutyBody[Station](doc)
			if !st.Enabled {
				continue
			}
			rosters, e := listAll(tx, model.DutyFilter{Kind: model.DutyRosterKind, StationID: doc.ID})
			if e != nil {
				return e
			}
			covered := false
			for _, d := range rosters {
				r, _ := model.DutyBody[Roster](d)
				if r.Status != "PUBLISHED" {
					continue
				}
				if r.StartAt <= now && r.EndAt > now {
					covered = true
				}
				if r.EndAt >= now && r.EndAt-now <= int64(st.ReminderMinutes)*time.Minute.Milliseconds() {
					if e = s.notify(tx, r.LeaderID, doc.ID, "", "HANDOVER_SOON", d.ID, "临近交班，请整理本班记录"); e != nil {
						return e
					}
				}
			}
			if !covered {
				bucket := time.UnixMilli(now).UTC().Format("2006-01-02T15")
				if e = s.notifyKey(tx, st.SupervisorID, doc.ID, "", "ROSTER_GAP", doc.ID, "岗位当前存在排班缺口", doc.ID+":"+bucket); e != nil {
					return e
				}
			}
			active, e := listAll(tx, model.DutyFilter{Kind: model.DutyRunKind, StationID: doc.ID, Status: "ACTIVE"})
			if e != nil {
				return e
			}
			if len(active) == 0 {
				bucket := time.UnixMilli(now).UTC().Format("2006-01-02T15")
				if e = s.notifyKey(tx, st.SupervisorID, doc.ID, "", "STATION_VACANT", doc.ID, "岗位尚无实际在岗班次，请核实值守安排", doc.ID+":"+bucket); e != nil {
					return e
				}
			}
			pending, e := listAll(tx, model.DutyFilter{Kind: model.DutyHandoverKind, StationID: doc.ID, Status: "SUBMITTED"})
			if e != nil {
				return e
			}
			for _, d := range pending {
				h, _ := model.DutyBody[Handover](d)
				_, next, e := read[Roster](tx, model.DutyRosterKind, h.NextRosterID)
				if e != nil {
					return e
				}
				if now >= next.StartAt {
					if e = s.notify(tx, st.SupervisorID, doc.ID, h.RunID, "RECEIVER_LATE", d.ID, "交接超过计划接班时间仍未接收，请安排续班或替补"); e != nil {
						return e
					}
				}
			}
			items, e := listAll(tx, model.DutyFilter{Kind: model.DutyItemKind, StationID: doc.ID})
			if e != nil {
				return e
			}
			for _, d := range items {
				it, _ := model.DutyBody[Item](d)
				if it.Status == "DONE" || it.Status == "CANCELLED" || it.DueAt == 0 {
					continue
				}
				typ, title := "ITEM_DUE", "跟进事项即将到期"
				if it.DueAt < now {
					typ = "ITEM_OVERDUE"
					title = "跟进事项已逾期"
				} else if it.DueAt-now > 15*time.Minute.Milliseconds() {
					continue
				}
				if e = s.notifyKey(tx, it.OwnerID, it.StationID, it.RunID, typ, d.ID, title, d.ID+":"+strconv.FormatInt(d.Version, 10)); e != nil {
					return e
				}
			}
		}
		return s.lateAmendments(tx, tenant)
	})
}
