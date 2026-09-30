package alarmgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

const reminderScanLimit = 2000

type reminderCandidate struct {
	source   model.GovernanceDocument
	reminder model.GovernanceReminder
}

func reminderMessage(category string) string {
	switch category {
	case "MEASURE_OVERDUE":
		return "治理措施已超过计划期限"
	case "ACCEPTANCE_PENDING":
		return "治理措施已实施，等待必要验收"
	case "OBSERVATION_INSUFFICIENT":
		return "本轮观察资料不足，需要补充证据或明确后续责任"
	case "POST_COMPLETION_REPORT":
		return "本轮完成后收到新的报警上报，请核实是否复发"
	}
	return "治理事项需要跟进"
}
func (s *Service) reminderActorTx(tx ports.AlarmGovernanceTx, a Actor) (Actor, error) {
	if s.ResolveTx != nil {
		fresh, err := s.ResolveTx(tx, a)
		if err != nil || fresh.TenantID != a.TenantID || fresh.Username != a.Username || a.Managed && fresh.SessionVersion != a.SessionVersion {
			return a, ErrForbidden
		}
		a = fresh
	}
	if !a.menu() || !sourceMenus(a, model.GovernanceReminderKind) {
		return a, ErrForbidden
	}
	return a, nil
}

// RefreshUserReminders is a bounded user-scoped refresh. It evaluates existing
// deterministic task conditions and never sends a model call or changes alarm
// state. Exceeding the bound returns an explicit error instead of a partial scan.
func (s *Service) RefreshUserReminders(ctx context.Context, a Actor) (created int, err error) {
	a, err = s.actor(ctx, a, "")
	if err != nil {
		return
	}
	if !sourceMenus(a, model.GovernanceReminderKind) {
		return 0, ErrForbidden
	}
	candidates := []reminderCandidate{}
	now := s.Now().UnixMilli()
	err = s.Store.GovernanceRead(ctx, a.TenantID, func(tx ports.AlarmGovernanceTx) error {
		var e error
		a, e = s.reminderActorTx(tx, a)
		if e != nil {
			return e
		}
		count, factsScanned := 0, 0
		add := func(source model.GovernanceDocument, category string, dueAt int64, observationID string) {
			if observationID != "" {
				for i := range candidates {
					c := &candidates[i]
					if c.source.Kind == source.Kind && c.source.ID == source.ID && c.reminder.Category == category && c.reminder.RoundID == source.RoundID {
						c.reminder.ObservationIDs = unique(append(c.reminder.ObservationIDs, observationID))
						c.reminder.Count = len(c.reminder.ObservationIDs)
						c.reminder.FactsHash = model.GovernanceHash(c.reminder.ObservationIDs)
						return
					}
				}
			}
			v := model.GovernanceReminder{UserID: a.Username, CaseID: source.CaseID, RoundID: source.RoundID, Category: category, TaskKind: source.Kind, TaskID: source.ID, TaskVersion: source.Version, ObservationID: observationID, ScopeKey: model.GovernanceHash(unique(source.DeviceIDs)), DueAt: dueAt, Status: "UNREAD", Count: 1, CreatedAt: now}
			if observationID != "" {
				v.ObservationIDs = []string{observationID}
				v.FactsHash = model.GovernanceHash(v.ObservationIDs)
			}
			candidates = append(candidates, reminderCandidate{source: source, reminder: v})
		}
		for _, kind := range []string{model.GovernanceMeasureKind, model.GovernanceReviewKind, model.GovernanceCaseKind} {
			f := model.GovernanceFilter{Kind: kind, AllDevices: a.AllDevices, DeviceIDs: a.DeviceIDs, Limit: 100}
			for {
				page, total, e := tx.List(f)
				if e != nil {
					return e
				}
				for _, d := range page {
					count++
					if count > reminderScanLimit {
						return invalid("提醒候选超过单次核查上限，请按事项筛选")
					}
					switch kind {
					case model.GovernanceMeasureKind:
						_, v, e := read[model.ImprovementMeasure](tx, kind, d.ID)
						if e != nil {
							return e
						}
						if v.OwnerUserID != a.Username {
							continue
						}
						_, round, e := read[model.GovernanceRound](tx, model.GovernanceRoundKind, d.RoundID)
						if e != nil {
							return e
						}
						if round.Status != "ACTIVE" {
							continue
						}
						if (v.Status == "PLANNED" || v.Status == "IN_PROGRESS") && v.DueAt > 0 && v.DueAt < now {
							add(d, "MEASURE_OVERDUE", v.DueAt, "")
						}
						if v.Status == "IMPLEMENTED" && v.RequiresAcceptance {
							add(d, "ACCEPTANCE_PENDING", v.DueAt, "")
						}
					case model.GovernanceReviewKind:
						v, e := model.GovernanceBody[model.ObservationReview](d)
						if e != nil {
							return e
						}
						if v.Status != "CONFIRMED" || v.Conclusion != "INSUFFICIENT_DATA" {
							continue
						}
						_, c, e := read[model.GovernanceCase](tx, model.GovernanceCaseKind, d.CaseID)
						if e != nil {
							return e
						}
						if v.FollowupOwnerUserID == a.Username || v.FollowupOwnerUserID == "" && c.OwnerUserID == a.Username {
							add(d, "OBSERVATION_INSUFFICIENT", 0, "")
						}
					case model.GovernanceCaseKind:
						c, e := model.GovernanceBody[model.GovernanceCase](d)
						if e != nil {
							return e
						}
						if c.Status != "COMPLETED" || c.OwnerUserID != a.Username {
							continue
						}
						_, round, e := read[model.GovernanceRound](tx, model.GovernanceRoundKind, c.CurrentRoundID)
						if e != nil {
							return e
						}
						if round.EndedAt == nil || round.Status != "COMPLETED" {
							continue
						}
						reader, ok := tx.(ports.AlarmObservationReader)
						if !ok {
							return invalid("治理事实读取不可用")
						}
						f := ports.AlarmObservationFilter{DeviceIDs: d.DeviceIDs, DeviceID: c.DeviceID, ComponentID: c.ComponentID, AlarmType: c.AlarmType, OriginKind: c.OriginKind, SignalKey: c.SignalKey, TimeBasis: "RECORDED_AT", Start: *round.EndedAt + 1, End: now + 1, Limit: 200}
						for {
							facts, e := reader.ListAlarmObservations(f)
							if e != nil {
								return e
							}
							for _, o := range facts {
								factsScanned++
								if factsScanned > reminderScanLimit {
									return invalid("完成后上报超过单次核查上限")
								}
								if o.Acceptance == "ACCEPTED" && (o.FactKind == "ASSERT" || o.FactKind == "REPORT") {
									source := d
									source.CaseID, source.RoundID = d.ID, c.CurrentRoundID
									add(source, "POST_COMPLETION_REPORT", 0, o.ID)
								}
							}
							if len(facts) < f.Limit {
								break
							}
							last := facts[len(facts)-1]
							f.Cursor = fmt.Sprintf("%d:%s", last.TimeAt(f.TimeBasis), last.ID)
						}
					}
				}
				f.Offset += len(page)
				if f.Offset >= total {
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	// Original source checks can access other repositories and stay outside the
	// storage transaction. Only minimal task identity is persisted.
	authorized := []reminderCandidate{}
	for _, c := range candidates {
		if _, e := s.Get(ctx, a, c.source.Kind, c.source.ID); e == nil {
			authorized = append(authorized, c)
		} else if !errors.Is(e, ErrForbidden) && !errors.Is(e, ErrSourceUnavailable) && !errors.Is(e, model.ErrNotFound) {
			return 0, e
		}
	}
	err = s.Store.GovernanceTransaction(ctx, a.TenantID, func(tx ports.AlarmGovernanceTx) error {
		created = 0
		fresh, e := s.reminderActorTx(tx, a)
		if e != nil {
			return e
		}
		for _, c := range authorized {
			source, e := tx.Get(c.source.Kind, c.source.ID)
			if e != nil {
				return e
			}
			if source.Version != c.source.Version {
				continue
			}
			if e = s.scope(source, fresh); e != nil {
				continue
			}
			if relevant, e := reminderRelevant(tx, fresh, source, c.reminder, now); e != nil {
				return e
			} else if !relevant {
				continue
			}
			id := "grem_" + model.GovernanceHash([]string{fresh.Username, c.reminder.CaseID, c.reminder.RoundID, c.reminder.Category, c.reminder.TaskKind, c.reminder.TaskID, fmt.Sprint(c.reminder.TaskVersion), c.reminder.ScopeKey})[:32]
			if prior, e := tx.Get(model.GovernanceReminderKind, id); e == nil {
				before, e := model.GovernanceBody[model.GovernanceReminder](prior)
				if e != nil {
					return e
				}
				if before.FactsHash == c.reminder.FactsHash {
					continue
				}
				if c.reminder.Category == "POST_COMPLETION_REPORT" {
					c.reminder.ObservationIDs = unique(append(append([]string{}, before.ObservationIDs...), c.reminder.ObservationIDs...))
					c.reminder.Count = len(c.reminder.ObservationIDs)
					c.reminder.FactsHash = model.GovernanceHash(c.reminder.ObservationIDs)
					if before.FactsHash == c.reminder.FactsHash {
						continue
					}
				}
				// One current reminder aggregates the fixed member set. New facts
				// need attention; earlier read/handled values remain in history.
				c.reminder.CreatedAt = before.CreatedAt
				prior.Status = "UNREAD"
				if _, e = put(tx, prior, c.reminder, prior.Version); e != nil {
					return e
				}
				continue
			} else if !errors.Is(e, model.ErrNotFound) {
				return e
			}
			_, e = put(tx, model.GovernanceDocument{Kind: model.GovernanceReminderKind, ID: id, CaseID: c.reminder.CaseID, RoundID: c.reminder.RoundID, OwnerUserID: fresh.Username, Status: "UNREAD", DeviceIDs: source.DeviceIDs, CreatedBy: "SYSTEM", OccurredAt: now}, c.reminder, 0)
			if e != nil {
				return e
			}
			created++
		}
		return nil
	})
	return
}

// A task's surrounding round can change without changing the task itself.
// Reevaluate that dependency before persisting, listing or handling a reminder.
func reminderRelevant(tx ports.AlarmGovernanceTx, a Actor, source model.GovernanceDocument, v model.GovernanceReminder, now int64) (bool, error) {
	if source.Version != v.TaskVersion || v.UserID != a.Username {
		return false, nil
	}
	switch v.Category {
	case "MEASURE_OVERDUE", "ACCEPTANCE_PENDING":
		measure, err := model.GovernanceBody[model.ImprovementMeasure](source)
		if err != nil {
			return false, err
		}
		_, round, err := read[model.GovernanceRound](tx, model.GovernanceRoundKind, v.RoundID)
		if err != nil {
			return false, err
		}
		if round.Status != "ACTIVE" || measure.OwnerUserID != a.Username {
			return false, nil
		}
		if v.Category == "MEASURE_OVERDUE" {
			return (measure.Status == "PLANNED" || measure.Status == "IN_PROGRESS") && measure.DueAt > 0 && measure.DueAt < now, nil
		}
		return measure.Status == "IMPLEMENTED" && measure.RequiresAcceptance, nil
	case "OBSERVATION_INSUFFICIENT":
		review, err := model.GovernanceBody[model.ObservationReview](source)
		if err != nil {
			return false, err
		}
		_, c, err := read[model.GovernanceCase](tx, model.GovernanceCaseKind, v.CaseID)
		if err != nil {
			return false, err
		}
		return review.Status == "CONFIRMED" && review.Conclusion == "INSUFFICIENT_DATA" && (review.FollowupOwnerUserID == a.Username || review.FollowupOwnerUserID == "" && c.OwnerUserID == a.Username), nil
	case "POST_COMPLETION_REPORT":
		c, err := model.GovernanceBody[model.GovernanceCase](source)
		if err != nil {
			return false, err
		}
		if c.Status != "COMPLETED" || c.CurrentRoundID != v.RoundID || c.OwnerUserID != a.Username {
			return false, nil
		}
		_, round, err := read[model.GovernanceRound](tx, model.GovernanceRoundKind, v.RoundID)
		if err != nil {
			return false, err
		}
		reader, ok := tx.(ports.AlarmObservationReader)
		if !ok {
			return false, invalid("治理事实读取不可用")
		}
		if round.Status != "COMPLETED" || round.EndedAt == nil || len(v.ObservationIDs) == 0 || v.Count != len(v.ObservationIDs) || v.FactsHash != model.GovernanceHash(unique(v.ObservationIDs)) {
			return false, nil
		}
		for _, id := range v.ObservationIDs {
			o, err := reader.GetAlarmObservation(id)
			if errors.Is(err, model.ErrNotFound) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			if o.RecordedAt <= *round.EndedAt || o.Acceptance != "ACCEPTED" || (o.FactKind != "ASSERT" && o.FactKind != "REPORT") || o.DeviceID != c.DeviceID || o.ComponentID != c.ComponentID || o.AlarmType != c.AlarmType || o.OriginKind != c.OriginKind || o.SignalKey != c.SignalKey {
				return false, nil
			}
		}
		return true, nil
	}
	return false, nil
}

func (s *Service) UserReminders(ctx context.Context, a Actor, status string, limit, offset int) (out []model.GovernanceDocument, total int, err error) {
	a, err = s.actor(ctx, a, "")
	if err != nil {
		return
	}
	if status != "" && !slices.Contains([]string{"UNREAD", "READ", "HANDLED"}, status) {
		return nil, 0, invalid("提醒状态无效")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	candidates := []model.GovernanceDocument{}
	err = s.Store.GovernanceRead(ctx, a.TenantID, func(tx ports.AlarmGovernanceTx) error {
		fresh, e := s.reminderActorTx(tx, a)
		if e != nil {
			return e
		}
		a = fresh
		f := model.GovernanceFilter{Kind: model.GovernanceReminderKind, OwnerUserID: a.Username, Status: status, DeviceIDs: a.DeviceIDs, AllDevices: a.AllDevices, Limit: 100}
		for {
			page, n, e := tx.List(f)
			if e != nil {
				return e
			}
			for _, d := range page {
				v, e := model.GovernanceBody[model.GovernanceReminder](d)
				if e != nil {
					return e
				}
				source, e := tx.Get(v.TaskKind, v.TaskID)
				if errors.Is(e, model.ErrNotFound) {
					continue
				}
				if e != nil {
					return e
				}
				relevant, e := reminderRelevant(tx, fresh, source, v, s.Now().UnixMilli())
				if e != nil {
					return e
				}
				if relevant {
					candidates = append(candidates, d)
				}
			}
			f.Offset += len(page)
			if f.Offset >= n {
				break
			}
			if f.Offset > 10000 {
				return invalid("提醒数量过多，请按状态筛选")
			}
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	visible := []model.GovernanceDocument{}
	for _, d := range candidates {
		v, e := model.GovernanceBody[model.GovernanceReminder](d)
		if e != nil {
			return nil, 0, e
		}
		if v.UserID != a.Username {
			continue
		}
		source, e := s.Get(ctx, a, v.TaskKind, v.TaskID)
		if e != nil {
			if errors.Is(e, ErrForbidden) || errors.Is(e, ErrSourceUnavailable) || errors.Is(e, model.ErrNotFound) {
				continue
			}
			return nil, 0, e
		}
		if source.Version != v.TaskVersion {
			continue
		}
		v.Message = reminderMessage(v.Category)
		d.Body, _ = json.Marshal(v)
		visible = append(visible, d)
	}
	total = len(visible)
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []model.GovernanceDocument{}, total, nil
	}
	return visible[offset:min(total, offset+limit)], total, nil
}

func (s *Service) UpdateReminder(ctx context.Context, a Actor, id, status string, expected int64, idempotencyKey string) (out model.GovernanceDocument, err error) {
	a, err = s.actor(ctx, a, "record")
	if err != nil {
		return
	}
	if !slices.Contains([]string{"READ", "HANDLED"}, status) || len(strings.TrimSpace(idempotencyKey)) < 1 || len(idempotencyKey) > 200 {
		return out, invalid("提醒动作及幂等键无效")
	}
	existing, err := s.Get(ctx, a, model.GovernanceReminderKind, id)
	if err != nil {
		return out, err
	}
	v, err := model.GovernanceBody[model.GovernanceReminder](existing)
	if err != nil {
		return out, err
	}
	if v.UserID != a.Username {
		return out, ErrForbidden
	}
	source, err := s.Get(ctx, a, v.TaskKind, v.TaskID)
	if err != nil {
		return out, err
	}
	receiptID := "grem_receipt_" + model.GovernanceHash([]string{a.Username, id, status, idempotencyKey})[:32]
	hash := model.GovernanceHash([]any{id, status, expected})
	err = s.Store.GovernanceTransaction(ctx, a.TenantID, func(tx ports.AlarmGovernanceTx) error {
		fresh, e := s.reminderActorTx(tx, a)
		if e != nil {
			return e
		}
		if !fresh.can("record") {
			return ErrForbidden
		}
		current, e := tx.Get(v.TaskKind, v.TaskID)
		if e != nil {
			return e
		}
		if current.Version != source.Version || current.Version != v.TaskVersion {
			return model.ErrGovernanceConflict
		}
		if e = s.scope(current, fresh); e != nil {
			return e
		}
		if relevant, e := reminderRelevant(tx, fresh, current, v, s.Now().UnixMilli()); e != nil {
			return e
		} else if !relevant {
			return model.ErrGovernanceConflict
		}
		if prior, e := tx.Get(model.GovernanceReceiptKind, receiptID); e == nil {
			r, e := model.GovernanceBody[receipt](prior)
			if e != nil {
				return e
			}
			if r.Hash != hash {
				return model.ErrGovernanceConflict
			}
			out = r.Result
			return nil
		} else if !errors.Is(e, model.ErrNotFound) {
			return e
		}
		d, value, e := read[model.GovernanceReminder](tx, model.GovernanceReminderKind, id)
		if e != nil {
			return e
		}
		if value.UserID != fresh.Username {
			return ErrForbidden
		}
		if d.Version != expected {
			return model.ErrGovernanceConflict
		}
		if value.Status == "HANDLED" && status == "READ" {
			return invalid("已处理提醒不能改回未处理")
		}
		value.Status = status
		now := s.Now().UnixMilli()
		if value.ReadAt == 0 {
			value.ReadAt = now
		}
		if status == "HANDLED" {
			value.HandledAt = now
		}
		d.Status = status
		out, e = put(tx, d, value, d.Version)
		if e != nil {
			return e
		}
		_, e = put(tx, model.GovernanceDocument{Kind: model.GovernanceReceiptKind, ID: receiptID, DeviceIDs: d.DeviceIDs, CreatedBy: fresh.Username, OccurredAt: now}, receipt{Hash: hash, Result: out}, 0)
		return e
	})
	if err == nil {
		_, err = s.Get(ctx, a, v.TaskKind, v.TaskID)
	}
	return
}
