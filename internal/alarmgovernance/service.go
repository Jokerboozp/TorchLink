// Package alarmgovernance owns human governance records. It never mutates
// production alarms, rules, device state or ingestion queues.
package alarmgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"slices"
	"sort"
	"strings"
	"time"
)

var ErrForbidden = errors.New("无权访问治理资源")

type Actor struct {
	TenantID       string
	Username       string
	Permissions    []string
	DeviceIDs      []string
	AllDevices     bool
	Managed        bool
	SessionVersion int64
	AccessVersion  string
}
type ResolveActor func(context.Context, Actor) (Actor, error)
type Service struct {
	ResolveTx       func(ports.AlarmGovernanceTx, Actor) (Actor, error)
	Store           ports.AlarmGovernanceStore
	Resolve         ResolveActor
	CheckDevice     func(context.Context, string, string) error
	Observations    ports.AlarmObservationStore
	ValidateReview  func(context.Context, string, model.ObservationReview) error
	AuthorizeSource func(context.Context, Actor, model.GovernanceDocument) error
	Now             func() time.Time
}

func New(store ports.AlarmGovernanceStore, resolve ResolveActor, check func(context.Context, string, string) error, observations ports.AlarmObservationStore) *Service {
	return &Service{Store: store, Resolve: resolve, CheckDevice: check, Observations: observations, Now: time.Now}
}

type Command struct {
	Kind            string
	ID              string
	RoundID         string
	CaseID          string
	Operation       string
	ExpectedVersion int64
	IdempotencyKey  string
	Body            json.RawMessage
}
type commandFields struct {
	ReviewerUserID      string   `json:"reviewerUserId"`
	FollowupOwnerUserID string   `json:"followupOwnerUserId"`
	ExpectedVersion     int64    `json:"expectedVersion"`
	IdempotencyKey      string   `json:"idempotencyKey"`
	Reason              string   `json:"reason"`
	OwnerUserID         string   `json:"ownerUserId"`
	ReviewID            string   `json:"reviewId"`
	ObservationIDs      []string `json:"observationIds"`
	DataRevision        int64    `json:"dataRevision"`
	AnalysisSnapshotID  string   `json:"analysisSnapshotId"`
	FactsHash           string   `json:"factsHash"`
}
type receipt struct {
	Hash   string                   `json:"hash"`
	Result model.GovernanceDocument `json:"result"`
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", model.ErrGovernanceInvalid, fmt.Sprintf(format, args...))
}
func (a Actor) can(action string) bool {
	return slices.Contains(a.Permissions, "*") || slices.Contains(a.Permissions, "action:alarmGovernance:"+action)
}
func (a Actor) menu() bool {
	return slices.Contains(a.Permissions, "*") || slices.Contains(a.Permissions, "menu:alarmGovernance")
}
func (a Actor) covers(ids []string) bool {
	if a.AllDevices {
		return true
	}
	for _, id := range ids {
		if !slices.Contains(a.DeviceIDs, id) {
			return false
		}
	}
	return true
}
func Permission(kind, op string) string {
	if configKind(kind) {
		if op == "publish" || op == "retire" {
			return "publish"
		}
		return "templates"
	}
	switch kind {
	case model.GovernanceCaseKind:
		if op == "complete" {
			return "complete"
		}
		if op == "reopen" {
			return "reopen"
		}
		return "cases"
	case model.GovernanceCauseKind:
		return "cause"
	case model.GovernanceMeasureKind:
		if op == "verify" {
			return "acceptance"
		}
		return "measures"
	case model.GovernancePlanKind, model.GovernanceReviewKind:
		return "observation"
	case model.GovernanceReportKind:
		return "export"
	}
	return "record"
}
func configKind(kind string) bool {
	return slices.Contains([]string{model.GovernanceTemplateKind, model.GovernanceSceneKind, model.GovernanceProfileKind}, kind)
}
func sourceMenus(a Actor, kind string) bool {
	return configKind(kind) || slices.Contains(a.Permissions, "*") || slices.Contains(a.Permissions, "menu:devices") && slices.Contains(a.Permissions, "menu:alarms")
}
func (s *Service) actor(ctx context.Context, a Actor, action string) (Actor, error) {
	var err error
	if s.Resolve != nil {
		fresh, e := s.Resolve(ctx, a)
		err = e
		if err == nil {
			if fresh.TenantID != a.TenantID || fresh.Username != a.Username || a.Managed && fresh.SessionVersion != a.SessionVersion {
				return a, ErrForbidden
			}
			a = fresh
		}
	}
	if err != nil || a.TenantID == "" || a.Username == "" || !a.menu() || action != "" && !a.can(action) {
		return a, ErrForbidden
	}
	return a, nil
}
func unique(ids []string) []string {
	out := []string{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}
func put(tx ports.AlarmGovernanceTx, d model.GovernanceDocument, body any, expected int64) (model.GovernanceDocument, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return d, err
	}
	d.Body = raw
	return tx.Put(d, expected)
}
func read[T any](tx ports.AlarmGovernanceTx, kind, id string) (model.GovernanceDocument, T, error) {
	d, e := tx.Get(kind, id)
	var b T
	if e == nil {
		b, e = model.GovernanceBody[T](d)
	}
	return d, b, e
}
func decode[T any](raw json.RawMessage) (v T, err error) {
	err = json.Unmarshal(raw, &v)
	if err != nil {
		err = invalid("请求正文格式错误")
	}
	return
}
func (s *Service) checkIDs(ctx context.Context, a Actor, ids []string) error {
	if len(ids) == 0 || len(ids) > 100 || !a.covers(ids) {
		return ErrForbidden
	}
	for _, id := range ids {
		if s.CheckDevice != nil {
			if err := s.CheckDevice(ctx, a.TenantID, id); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Service) scope(d model.GovernanceDocument, a Actor) error {
	if !a.covers(d.DeviceIDs) {
		return ErrForbidden
	}
	if len(d.DeviceIDs) == 0 && !configKind(d.Kind) && d.Kind != model.GovernanceReceiptKind {
		return ErrForbidden
	}
	return nil
}
func (s *Service) EnsureBuiltins(ctx context.Context, tenant string) error {
	return s.Store.GovernanceTransaction(ctx, tenant, func(tx ports.AlarmGovernanceTx) error {
		for _, d := range Builtins() {
			if _, err := tx.Get(d.Kind, d.ID); errors.Is(err, model.ErrNotFound) {
				if _, err = tx.Put(d, 0); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
		}
		return nil
	})
}
func (s *Service) Get(ctx context.Context, a Actor, kind, id string) (out model.GovernanceDocument, err error) {
	a, err = s.actor(ctx, a, "")
	if err != nil {
		return
	}
	if !sourceMenus(a, kind) {
		return out, ErrForbidden
	}
	if err = s.EnsureBuiltins(ctx, a.TenantID); err != nil {
		return
	}
	err = s.Store.GovernanceRead(ctx, a.TenantID, func(tx ports.AlarmGovernanceTx) error {
		if s.ResolveTx != nil {
			fresh, e := s.ResolveTx(tx, a)
			if e != nil || !fresh.menu() || !sourceMenus(fresh, kind) {
				return ErrForbidden
			}
			a = fresh
		}
		d, e := tx.Get(kind, id)
		if e != nil {
			return e
		}
		if e = s.scope(d, a); e != nil {
			return e
		}
		out = d
		return nil
	})
	if err == nil {
		fresh, e := s.actor(ctx, a, "")
		if e != nil || !sourceMenus(fresh, kind) || s.scope(out, fresh) != nil {
			return model.GovernanceDocument{}, ErrForbidden
		}
		a = fresh
	}
	if err == nil && s.AuthorizeSource != nil {
		err = s.AuthorizeSource(ctx, a, out)
	}
	return
}
func (s *Service) List(ctx context.Context, a Actor, f model.GovernanceFilter) (out []model.GovernanceDocument, total int, err error) {
	var authorizeDocuments []model.GovernanceDocument
	a, err = s.actor(ctx, a, "")
	if err != nil {
		return
	}
	if !sourceMenus(a, f.Kind) {
		return nil, 0, ErrForbidden
	}
	if err = s.EnsureBuiltins(ctx, a.TenantID); err != nil {
		return
	}
	f.AllDevices = a.AllDevices
	f.DeviceIDs = a.DeviceIDs
	if f.Limit <= 0 {
		f.Limit = 20
	}
	if f.Limit > 100 {
		f.Limit = 100
	}
	err = s.Store.GovernanceRead(ctx, a.TenantID, func(tx ports.AlarmGovernanceTx) error {
		if s.ResolveTx != nil {
			fresh, e := s.ResolveTx(tx, a)
			if e != nil || !fresh.menu() || !sourceMenus(fresh, f.Kind) {
				return ErrForbidden
			}
			a = fresh
			f.AllDevices = a.AllDevices
			f.DeviceIDs = a.DeviceIDs
		}
		var e error
		out, total, e = tx.List(f)
		if e != nil || s.AuthorizeSource == nil {
			return e
		}
		probe := f
		probe.Offset = 0
		probe.Limit = 100
		for {
			page, n, e := tx.List(probe)
			if e != nil {
				return e
			}
			authorizeDocuments = append(authorizeDocuments, page...)
			probe.Offset += len(page)
			if probe.Offset >= n {
				return nil
			}
			if len(page) == 0 || probe.Offset > 10000 {
				return invalid("来源权限校验超过读取限制")
			}
		}
	})
	if err == nil {
		fresh, e := s.actor(ctx, a, "")
		if e != nil || !sourceMenus(fresh, f.Kind) {
			return nil, 0, ErrForbidden
		}
		a = fresh
		for _, d := range out {
			if s.scope(d, a) != nil {
				return nil, 0, ErrForbidden
			}
		}
	}
	if err == nil && s.AuthorizeSource != nil {
		for _, d := range authorizeDocuments {
			if s.scope(d, a) != nil {
				return nil, 0, ErrForbidden
			}
			if e := s.AuthorizeSource(ctx, a, d); e != nil {
				return nil, 0, e
			}
		}
	}
	return
}
func (s *Service) Execute(ctx context.Context, a Actor, c Command) (out model.GovernanceDocument, err error) {
	a, err = s.actor(ctx, a, Permission(c.Kind, c.Operation))
	if err != nil {
		return
	}
	if !sourceMenus(a, c.Kind) {
		return out, ErrForbidden
	}
	if len(c.Body) == 0 {
		c.Body = []byte("{}")
	}
	q, e := decode[commandFields](c.Body)
	if e != nil {
		return out, e
	}
	if c.ExpectedVersion == 0 {
		c.ExpectedVersion = q.ExpectedVersion
	}
	if c.IdempotencyKey == "" {
		c.IdempotencyKey = q.IdempotencyKey
	}
	if len(c.IdempotencyKey) < 1 || len(c.IdempotencyKey) > 200 {
		return out, invalid("必须提供1至200字符idempotencyKey")
	}
	if err = s.EnsureBuiltins(ctx, a.TenantID); err != nil {
		return
	}
	var raw map[string]json.RawMessage
	if err = json.Unmarshal(c.Body, &raw); err != nil {
		return out, invalid("正文必须是对象")
	}
	if _, ok := raw["tenantId"]; ok {
		return out, invalid("租户由当前会话决定")
	}
	// Validate current users and immutable observation identities before entering
	// repository locks; the transaction validates all governance relationships.
	if q.OwnerUserID != "" && s.Resolve != nil {
		owner, e := s.Resolve(ctx, Actor{TenantID: a.TenantID, Username: q.OwnerUserID})
		if e != nil || !owner.menu() {
			return out, invalid("负责人无效或无治理权限")
		}
		var point model.GovernancePoint
		_ = json.Unmarshal(c.Body, &point)
		if point.DeviceID != "" && !owner.covers([]string{point.DeviceID}) {
			return out, invalid("负责人无目标设备权限")
		}
	}
	var p model.GovernancePoint
	_ = json.Unmarshal(c.Body, &p)
	var ids []string
	_ = json.Unmarshal(raw["deviceIds"], &ids)
	if p.DeviceID != "" {
		ids = []string{p.DeviceID}
	}
	if len(ids) > 0 {
		if err = s.checkIDs(ctx, a, unique(ids)); err != nil {
			return
		}
	}
	if len(q.ObservationIDs) > 200 {
		return out, invalid("关联事件超过200条")
	}
	for _, id := range q.ObservationIDs {
		if s.Observations == nil {
			return out, invalid("观察事实读取不可用")
		}
		v, e := s.Observations.GetAlarmObservation(ctx, a.TenantID, id)
		if e != nil {
			return out, e
		}
		if !a.covers([]string{v.DeviceID}) {
			return out, ErrForbidden
		}
		if p.DeviceID != "" && (p.DeviceID != v.DeviceID || p.ComponentID != v.ComponentID || p.AlarmType != v.AlarmType || p.OriginKind != v.OriginKind || p.SignalKey != v.SignalKey) {
			return out, invalid("事件不属于所选点位及信号")
		}
	}
	if c.Kind == model.GovernanceReviewKind && (c.Operation == "create" || c.Operation == "confirm") {
		r, _ := decode[model.ObservationReview](c.Body)
		if c.Operation == "create" {
			if r.RoundID != "" && r.RoundID != c.RoundID {
				return out, invalid("roundId与路由轮次不一致")
			}
			r.RoundID = c.RoundID
		}
		if c.Operation == "confirm" {
			d, e := s.Get(ctx, a, c.Kind, c.ID)
			if e != nil {
				return out, e
			}
			r, _ = model.GovernanceBody[model.ObservationReview](d)
		}
		if s.ValidateReview == nil {
			return out, invalid("确定性分析校验不可用")
		}
		if err = s.ValidateReview(ctx, a.TenantID, r); err != nil {
			return
		}
	}
	hash := model.GovernanceHash(c)
	receiptID := model.GovernanceHash([]string{a.Username, c.Kind, c.ID, c.RoundID, c.Operation, c.IdempotencyKey})
	err = s.Store.GovernanceTransaction(ctx, a.TenantID, func(tx ports.AlarmGovernanceTx) error {
		if s.ResolveTx != nil {
			fresh, e := s.ResolveTx(tx, a)
			if e != nil || !fresh.menu() || !fresh.can(Permission(c.Kind, c.Operation)) || !sourceMenus(fresh, c.Kind) {
				return ErrForbidden
			}
			a = fresh
		}
		if d, e := tx.Get(model.GovernanceReceiptKind, receiptID); e == nil {
			r, _ := model.GovernanceBody[receipt](d)
			if r.Hash != hash {
				return model.ErrGovernanceConflict
			}
			if e = s.scope(r.Result, a); e != nil {
				return e
			}
			out = r.Result
			return nil
		} else if !errors.Is(e, model.ErrNotFound) {
			return e
		}
		var d model.GovernanceDocument
		if c.ID != "" {
			var e error
			d, e = tx.Get(c.Kind, c.ID)
			if e != nil {
				return e
			}
			if e = s.scope(d, a); e != nil {
				return e
			}
			if c.Operation != "revisions" && c.Operation != "corrections" && d.Version != c.ExpectedVersion {
				return model.ErrGovernanceConflict
			}
		}
		if s.ResolveTx != nil {
			targetIDs := ids
			if len(d.DeviceIDs) > 0 {
				targetIDs = d.DeviceIDs
			}
			if c.RoundID != "" {
				rd, e := tx.Get(model.GovernanceRoundKind, c.RoundID)
				if e != nil {
					return e
				}
				targetIDs = rd.DeviceIDs
			}
			for _, ownerID := range unique([]string{q.OwnerUserID, q.ReviewerUserID, q.FollowupOwnerUserID}) {
				owner, e := s.ResolveTx(tx, Actor{TenantID: a.TenantID, Username: ownerID})
				if e != nil || !owner.menu() || !owner.covers(targetIDs) {
					return invalid("负责人无效或缺少目标资源访问权限")
				}
			}
		}
		var e error
		out, e = s.execute(ctx, tx, a, c, d, q)
		if e != nil {
			return e
		}
		if out.CaseID != "" && out.Kind != model.GovernanceCaseKind && out.Kind != model.GovernanceRoundKind && out.Kind != model.GovernanceReportKind && out.Kind != model.GovernanceReviewKind {
			if e = s.bumpCase(tx, out.CaseID); e != nil {
				return e
			}
		}
		event := model.GovernanceEvent{CaseID: out.CaseID, RoundID: out.RoundID, ResourceID: out.ID, ResourceVersion: out.Version, Action: c.Kind + ":" + c.Operation, Actor: a.Username, OccurredAt: s.Now().UnixMilli(), RecordedAt: s.Now().UnixMilli(), Reason: q.Reason}
		if out.Kind == model.GovernanceCaseKind {
			event.CaseID = out.ID
			v, _ := model.GovernanceBody[model.GovernanceCase](out)
			event.RoundID = v.CurrentRoundID
		}
		_, e = put(tx, model.GovernanceDocument{Kind: model.GovernanceEventKind, ID: uuid.NewString(), CreatedBy: a.Username, CaseID: event.CaseID, RoundID: event.RoundID, DeviceIDs: out.DeviceIDs, OccurredAt: event.OccurredAt}, event, 0)
		if e != nil {
			return e
		}
		_, e = put(tx, model.GovernanceDocument{Kind: model.GovernanceReceiptKind, ID: receiptID, CreatedBy: a.Username}, receipt{Hash: hash, Result: out}, 0)
		return e
	})
	if err == nil {
		fresh, e := s.actor(ctx, a, Permission(c.Kind, c.Operation))
		if e != nil || !fresh.covers(out.DeviceIDs) {
			return out, ErrForbidden
		}
		if s.AuthorizeSource != nil {
			err = s.AuthorizeSource(ctx, fresh, out)
		}
	}
	return
}
func validateObservations(tx ports.AlarmGovernanceTx, p model.GovernancePoint, ids []string) error {
	reader, ok := tx.(ports.AlarmObservationReader)
	if !ok {
		return invalid("事务观察事实读取不可用")
	}
	for _, id := range ids {
		v, e := reader.GetAlarmObservation(id)
		if e != nil {
			return e
		}
		if v.DeviceID != p.DeviceID || v.ComponentID != p.ComponentID || v.AlarmType != p.AlarmType || v.OriginKind != p.OriginKind || v.SignalKey != p.SignalKey {
			return invalid("事件范围不能扩大到其他点位或信号")
		}
	}
	return nil
}
func (s *Service) bumpCase(tx ports.AlarmGovernanceTx, id string) error {
	d, b, e := read[model.GovernanceCase](tx, model.GovernanceCaseKind, id)
	if e != nil {
		return e
	}
	b.DataRevision++
	_, e = put(tx, d, b, d.Version)
	return e
}
func (s *Service) round(tx ports.AlarmGovernanceTx, a Actor, id string) (model.GovernanceDocument, model.GovernanceCase, error) {
	rd, r, e := read[model.GovernanceRound](tx, model.GovernanceRoundKind, id)
	if e != nil {
		return rd, model.GovernanceCase{}, e
	}
	if r.Status != "ACTIVE" {
		return rd, model.GovernanceCase{}, invalid("已结束轮次不能追加记录，请独立登记或重开新轮次")
	}
	cd, c, e := read[model.GovernanceCase](tx, model.GovernanceCaseKind, r.CaseID)
	if e != nil {
		return rd, c, e
	}
	if e = s.scope(cd, a); e != nil {
		return rd, c, e
	}
	return rd, c, nil
}
func (s *Service) execute(ctx context.Context, tx ports.AlarmGovernanceTx, a Actor, c Command, d model.GovernanceDocument, q commandFields) (model.GovernanceDocument, error) {
	if configKind(c.Kind) {
		return s.configuration(tx, a, c, d)
	}
	if c.Kind == model.GovernanceCaseKind {
		return s.caseCommand(tx, a, c, d, q)
	}
	if c.Kind == model.GovernanceVerificationKind {
		return s.verification(tx, a, c, d)
	}
	if c.Kind == model.GovernanceActivityKind || c.Kind == model.GovernanceCoverageKind {
		return s.activity(tx, a, c, d)
	}
	return s.roundRecord(ctx, tx, a, c, d, q)
}
