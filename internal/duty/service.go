package duty

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type Service struct {
	Store   ports.DutyStore
	Resolve ResolveActor
	Now     func() time.Time
}

func New(store ports.DutyStore, resolve ResolveActor) *Service {
	return &Service{Store: store, Resolve: resolve, Now: time.Now}
}
func (s *Service) now() int64 { return s.Now().UnixMilli() }
func canonicalKind(k string) string {
	switch k {
	case "stations":
		return model.DutyStationKind
	case "teams":
		return model.DutyTeamKind
	case "templates", "shift-templates":
		return model.DutyShiftTemplateKind
	case "rosters":
		return model.DutyRosterKind
	case "runs":
		return model.DutyRunKind
	case "records":
		return model.DutyRecordKind
	case "items":
		return model.DutyItemKind
	case "itemEvents", "item-events":
		return model.DutyItemEventKind
	case "handovers":
		return model.DutyHandoverKind
	case "revisions":
		return model.DutyRevisionKind
	case "aiJobs", "ai-jobs":
		return model.DutyAIJobKind
	case "notices", "notifications":
		return model.DutyNotificationKind
	}
	return k
}
func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
func unique(xs []string) []string {
	out := []string{}
	for _, x := range xs {
		x = strings.TrimSpace(x)
		if x != "" && !contains(out, x) {
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}
func (a Actor) can(p string) bool {
	return a.Admin || contains(a.Permissions, "*") || contains(a.Permissions, "action:duty:"+p)
}
func (a Actor) menu() bool {
	return a.Admin || contains(a.Permissions, "*") || contains(a.Permissions, "menu:duty")
}
func (a Actor) covers(ids []string) bool {
	if a.AllDevices || a.Admin {
		return true
	}
	for _, id := range ids {
		if !contains(a.AllowedDeviceIDs, id) {
			return false
		}
	}
	return true
}
func permission(k, op string) string {
	switch k {
	case model.DutyStationKind, model.DutyTeamKind, model.DutyShiftTemplateKind:
		return "settings"
	case model.DutyRosterKind:
		return "roster"
	case model.DutyRunKind:
		return "participate"
	case model.DutyRecordKind:
		return "record"
	case model.DutyItemKind:
		return "item"
	case model.DutyAIJobKind:
		return "ai"
	case model.DutyNotificationKind:
		return "participate"
	case model.DutyHandoverKind:
		if op == "accept" || op == "return" {
			return "accept"
		}
		if op == "start-ai" {
			return "ai"
		}
		return "handover"
	case model.DutyRevisionKind:
		return "handover"
	}
	return "settings"
}
func decode[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, invalid("请求内容格式错误：%v", err)
	}
	return v, nil
}
func read[T any](tx ports.DutyTx, k, id string) (model.DutyDocument, T, error) {
	doc, err := tx.Get(k, id)
	var v T
	if err != nil {
		return doc, v, err
	}
	v, err = model.DutyBody[T](doc)
	return doc, v, err
}
func put(tx ports.DutyTx, doc model.DutyDocument, body any, version int64) (model.DutyDocument, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return doc, err
	}
	doc.Body = raw
	return tx.Put(doc, version)
}
func create(tx ports.DutyTx, k string, body any) (model.DutyDocument, error) {
	return tx.Put(model.NewDutyDocument(k, uuid.NewString(), body), 0)
}
func checkVersion(doc model.DutyDocument, expected int64) error {
	if expected <= 0 || doc.Version != expected {
		return ErrConflict
	}
	return nil
}
func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func listAll(tx ports.DutyTx, f model.DutyFilter) ([]model.DutyDocument, error) {
	f.Limit = 1000
	f.Offset = 0
	var all []model.DutyDocument
	for {
		docs, total, err := tx.List(f)
		if err != nil {
			return nil, err
		}
		all = append(all, docs...)
		if len(docs) == 0 || len(all) >= total {
			return all, nil
		}
		f.Offset += len(docs)
	}
}
func eventsAll(tx ports.DutyTx, f model.DutyFilter) ([]model.DutyBusinessEvent, error) {
	f.Limit = 1000
	f.Offset = 0
	var all []model.DutyBusinessEvent
	for {
		docs, total, err := tx.Events(f)
		if err != nil {
			return nil, err
		}
		all = append(all, docs...)
		if len(docs) == 0 || len(all) >= total {
			return all, nil
		}
		f.Offset += len(docs)
	}
}

// accountMap resolves account eligibility outside a DutyTx, because both stores
// can share database connections/mutexes with the access repository.
func (s *Service) accountMap(ctx context.Context, a Actor, c Command) (map[string]Actor, error) {
	names := map[string]bool{a.Username: true}
	type ref struct{ kind, id string }
	queue := []ref{}
	if c.ID != "" {
		queue = append(queue, ref{c.Kind, c.ID})
	}
	seen := map[string]bool{}
	var scan func(any)
	scan = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, value := range x {
				if k == "memberIds" {
					if arr, ok := value.([]any); ok {
						for _, member := range arr {
							if n, ok := member.(string); ok && n != "" {
								names[n] = true
							}
						}
					}
				}
				if k == "leaderId" || k == "supervisorId" || k == "ownerId" || k == "userId" || k == "replacementId" {
					if n, ok := value.(string); ok && n != "" {
						names[n] = true
					}
				}
				kind := ""
				switch k {
				case "stationId":
					kind = model.DutyStationKind
				case "teamId":
					kind = model.DutyTeamKind
				case "runId":
					kind = model.DutyRunKind
				case "rosterId", "nextRosterId":
					kind = model.DutyRosterKind
				case "handoverId":
					kind = model.DutyHandoverKind
				case "itemId":
					kind = model.DutyItemKind
				}
				if id, ok := value.(string); ok && id != "" && kind != "" {
					queue = append(queue, ref{kind, id})
				}
				scan(value)
			}
		case []any:
			for _, value := range x {
				scan(value)
			}
		}
	}
	var raw any
	_ = json.Unmarshal(c.Body, &raw)
	scan(raw)
	if c.Operation == "import" || c.Operation == "import-preview" {
		req, _ := decode[ImportRequest](c.Body)
		rows, _ := parseImport(req)
		b, _ := json.Marshal(rows)
		_ = json.Unmarshal(b, &raw)
		scan(raw)
	}
	err := s.Store.DutyRead(ctx, a.TenantID, func(tx ports.DutyTx) error {
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			key := current.kind + ":" + current.id
			if seen[key] {
				continue
			}
			seen[key] = true
			doc, e := tx.Get(current.kind, current.id)
			if e != nil {
				if errors.Is(e, model.ErrNotFound) {
					continue
				}
				return e
			}
			var v any
			if e = json.Unmarshal(doc.Body, &v); e != nil {
				return e
			}
			scan(v)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	actors := map[string]Actor{a.Username: a}
	for name := range names {
		if name == a.Username {
			continue
		}
		if s.Resolve == nil {
			return nil, invalid("账号资格解析器未配置")
		}
		value, err := s.Resolve(ctx, a.TenantID, name)
		if err == nil {
			actors[name] = value
		}
	}
	return actors, nil
}

func eligible(actors map[string]Actor, n string, ids []string) error {
	a, ok := actors[n]
	if !ok || !a.Enabled || !a.menu() || !a.can("participate") || !a.covers(ids) {
		return invalid("人员 %s 未启用或缺少值班权限/设备范围", n)
	}
	return nil
}

func (s *Service) Execute(ctx context.Context, a Actor, c Command) (any, error) {
	if s.Store == nil {
		return nil, invalid("值班持久化未配置")
	}
	c.Kind = canonicalKind(c.Kind)
	if !a.Enabled || a.Username == "" || a.TenantID == "" || !a.menu() {
		return nil, ErrForbidden
	}
	if (c.Kind == model.DutyRunKind && c.Operation == "current") || (c.Kind == model.DutyHandoverKind && c.Operation == "delta") || (c.Kind == model.DutyItemKind && c.Operation == "events") || (c.Kind == "event" && c.Operation == "query") {
		return s.readOperation(ctx, a, c)
	}
	if !(c.Kind == model.DutyNotificationKind && c.Operation == "read") && !a.can(permission(c.Kind, c.Operation)) {
		return nil, ErrForbidden
	}
	actors, err := s.accountMap(ctx, a, c)
	if err != nil {
		return nil, err
	}
	var out any
	err = s.Store.DutyTransaction(model.WithDutyActor(ctx, a.Username), a.TenantID, func(tx ports.DutyTx) error {
		out = nil
		key := ""
		if c.IdempotencyKey != "" {
			if len(c.IdempotencyKey) > 200 {
				return invalid("幂等键过长")
			}
			key = digest([]string{a.Username, c.IdempotencyKey})
			if receipt, e := tx.Get("receipt", key); e == nil {
				var v struct {
					Digest string          `json:"digest"`
					Result json.RawMessage `json:"result"`
				}
				if e = json.Unmarshal(receipt.Body, &v); e != nil {
					return e
				}
				if v.Digest != digest(c) {
					return ErrConflict
				}
				if e = s.receiptScope(tx, a, v.Result); e != nil {
					return e
				}
				return json.Unmarshal(v.Result, &out)
			}
		}
		var e error
		switch c.Kind {
		case model.DutyStationKind, model.DutyTeamKind, model.DutyShiftTemplateKind:
			out, e = s.configure(tx, a, c, actors)
		case model.DutyRosterKind:
			out, e = s.roster(tx, a, c, actors)
		case model.DutyRunKind:
			out, e = s.run(tx, a, c, actors)
		case model.DutyRecordKind:
			out, e = s.record(tx, a, c)
		case model.DutyItemKind:
			out, e = s.item(tx, a, c, actors)
		case model.DutyHandoverKind, model.DutyRevisionKind:
			out, e = s.handover(tx, a, c, actors)
		case model.DutyAIJobKind:
			out, e = s.job(tx, a, c)
		case model.DutyNotificationKind:
			out, e = s.notice(tx, a, c)
		default:
			e = invalid("未知值班资源 %s", c.Kind)
		}
		if e != nil {
			return e
		}
		if key != "" {
			raw, _ := json.Marshal(out)
			_, e = tx.Put(model.NewDutyDocument("receipt", key, map[string]any{"digest": digest(c), "result": json.RawMessage(raw)}), 0)
		}
		return e
	})
	if errors.Is(err, model.ErrDutyConflict) {
		err = ErrConflict
	}
	return out, err
}
func docDevices(tx ports.DutyTx, doc model.DutyDocument) ([]string, error) {
	var body struct {
		DeviceIDs  []string `json:"deviceIds"`
		DeviceID   string   `json:"deviceId"`
		StationID  string   `json:"stationId"`
		RunID      string   `json:"runId"`
		HandoverID string   `json:"handoverId"`
	}
	if err := json.Unmarshal(doc.Body, &body); err != nil {
		return nil, err
	}
	if body.DeviceIDs != nil {
		return body.DeviceIDs, nil
	}
	if body.HandoverID != "" {
		d, e := tx.Get(model.DutyHandoverKind, body.HandoverID)
		if e != nil {
			return nil, e
		}
		return docDevices(tx, d)
	}
	if body.RunID != "" {
		d, e := tx.Get(model.DutyRunKind, body.RunID)
		if e != nil {
			return nil, e
		}
		return docDevices(tx, d)
	}
	if body.StationID != "" {
		d, e := tx.Get(model.DutyStationKind, body.StationID)
		if e != nil {
			return nil, e
		}
		return docDevices(tx, d)
	}
	if body.DeviceID != "" {
		return []string{body.DeviceID}, nil
	}
	return []string{}, nil
}
func (s *Service) readable(tx ports.DutyTx, a Actor, doc model.DutyDocument) bool {
	if doc.Kind == "receipt" {
		return false
	}
	if doc.Kind == model.DutyNotificationKind {
		v, _ := model.DutyBody[Notice](doc)
		if v.UserID != a.Username {
			return false
		}
	}
	ids, err := docDevices(tx, doc)
	if err != nil || !a.covers(ids) {
		return false
	}
	if doc.Kind == model.DutyNotificationKind {
		return true
	}
	if a.Admin || a.can("history") || a.can("settings") || a.can("roster") {
		return true
	}
	switch doc.Kind {
	case model.DutyStationKind, model.DutyTeamKind, model.DutyShiftTemplateKind:
		return true
	case model.DutyRosterKind:
		v, _ := model.DutyBody[Roster](doc)
		if contains(v.MemberIDs, a.Username) {
			return true
		}
		if v.Status == "PUBLISHED" && v.EndAt > s.now() {
			runs, _ := listAll(tx, model.DutyFilter{Kind: model.DutyRunKind, StationID: v.StationID, Status: "ACTIVE"})
			for _, d := range runs {
				r, _ := model.DutyBody[Run](d)
				if contains(r.MemberIDs, a.Username) {
					return true
				}
			}
		}
		return false
	case model.DutyRunKind:
		v, _ := model.DutyBody[Run](doc)
		return contains(v.MemberIDs, a.Username)
	}
	var v struct {
		RunID     string `json:"runId"`
		StationID string `json:"stationId"`
	}
	_ = json.Unmarshal(doc.Body, &v)
	if v.RunID != "" {
		_, r, e := read[Run](tx, model.DutyRunKind, v.RunID)
		if e == nil && contains(r.MemberIDs, a.Username) {
			return true
		}
	}
	if v.StationID != "" {
		_, st, e := read[Station](tx, model.DutyStationKind, v.StationID)
		if e == nil && st.SupervisorID == a.Username {
			return true
		}
		rosters, _ := listAll(tx, model.DutyFilter{Kind: model.DutyRosterKind, StationID: v.StationID})
		for _, d := range rosters {
			r, _ := model.DutyBody[Roster](d)
			if contains(r.MemberIDs, a.Username) {
				return true
			}
		}
	}
	return false
}

// Retrying an idempotent command still checks today's authorization. A saved
// response is not an enduring grant to the original scope or participant.
func (s *Service) receiptScope(tx ports.DutyTx, a Actor, raw json.RawMessage) error {
	var v any
	if e := json.Unmarshal(raw, &v); e != nil {
		return e
	}
	var visit func(any) error
	visit = func(value any) error {
		switch x := value.(type) {
		case []any:
			for _, v := range x {
				if e := visit(v); e != nil {
					return e
				}
			}
		case map[string]any:
			if _, ok := x["kind"]; ok {
				if _, ok = x["body"]; ok {
					b, _ := json.Marshal(x)
					var doc model.DutyDocument
					if e := json.Unmarshal(b, &doc); e != nil {
						return e
					}
					if !s.readable(tx, a, doc) {
						return ErrForbidden
					}
				}
			}
			if arr, ok := x["deviceIds"].([]any); ok {
				var ids []string
				for _, id := range arr {
					if v, ok := id.(string); ok {
						ids = append(ids, v)
					}
				}
				if !a.covers(ids) {
					return ErrForbidden
				}
			}
			if id, ok := x["deviceId"].(string); ok && id != "" && !a.covers([]string{id}) {
				return ErrForbidden
			}
			for _, value := range x {
				if e := visit(value); e != nil {
					return e
				}
			}
		}
		return nil
	}
	return visit(v)
}
func (s *Service) Query(ctx context.Context, a Actor, f model.DutyFilter) (Result, error) {
	var out Result
	out.Items = []model.DutyDocument{}
	if !a.Enabled || !a.menu() {
		return out, ErrForbidden
	}
	f.Kind = canonicalKind(f.Kind)
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
	err := s.Store.DutyRead(ctx, a.TenantID, func(tx ports.DutyTx) error {
		docs, e := listAll(tx, f)
		if e != nil {
			return e
		}
		for _, d := range docs {
			if s.readable(tx, a, d) {
				out.Items = append(out.Items, d)
			}
		}
		out.Total = len(out.Items)
		if offset >= out.Total {
			out.Items = []model.DutyDocument{}
		} else {
			end := offset + limit
			if end > out.Total {
				end = out.Total
			}
			out.Items = out.Items[offset:end]
		}
		return nil
	})
	return out, err
}
func (s *Service) Get(ctx context.Context, a Actor, kind, id string) (model.DutyDocument, error) {
	var out model.DutyDocument
	if !a.Enabled || !a.menu() {
		return out, ErrForbidden
	}
	err := s.Store.DutyRead(ctx, a.TenantID, func(tx ports.DutyTx) error {
		v, e := tx.Get(canonicalKind(kind), id)
		if e != nil {
			return e
		}
		if !s.readable(tx, a, v) {
			return ErrForbidden
		}
		out = v
		return nil
	})
	return out, err
}
func (s *Service) appendEvent(tx ports.DutyTx, a Actor, typ string, doc model.DutyDocument, station, run, device, alarm string, at int64) error {
	return tx.AppendEvent(model.DutyBusinessEvent{ID: uuid.NewString(), TenantID: a.TenantID, Type: typ, Source: "duty", ResourceID: doc.ID, ResourceVersion: doc.Version, StationID: station, RunID: run, DeviceID: device, AlarmID: alarm, ActorID: a.Username, OccurredAt: at, RecordedAt: s.now(), Body: doc.Body})
}
func (s *Service) notify(tx ports.DutyTx, user, station, run, kind, resource, title string) error {
	return s.notifyKey(tx, user, station, run, kind, resource, title, resource)
}
func (s *Service) notifyKey(tx ports.DutyTx, user, station, run, kind, resource, title, variant string) error {
	id := digest([]string{user, kind, variant})
	if _, err := tx.Get(model.DutyNotificationKind, id); err == nil {
		return nil
	}
	_, err := tx.Put(model.NewDutyDocument(model.DutyNotificationKind, id, Notice{UserID: user, StationID: station, RunID: run, Type: kind, ResourceID: resource, Title: title}), 0)
	return err
}
func (s *Service) notice(tx ports.DutyTx, a Actor, c Command) (any, error) {
	doc, v, e := read[Notice](tx, model.DutyNotificationKind, c.ID)
	if e != nil {
		return nil, e
	}
	if v.UserID != a.Username {
		return nil, ErrForbidden
	}
	if !s.readable(tx, a, doc) {
		return nil, ErrForbidden
	}
	if e = checkVersion(doc, c.ExpectedVersion); e != nil {
		return nil, e
	}
	if c.Operation != "read" {
		return nil, invalid("未知提醒操作")
	}
	v.ReadAt = s.now()
	return put(tx, doc, v, c.ExpectedVersion)
}
