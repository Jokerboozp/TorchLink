package analytics

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// StorageDocument is the durable envelope. Its indexed fields are derived by
// the store, never accepted from a browser. Body carries a typed analysis object.
type StorageDocument struct {
	TenantID, Kind, ID, RunID, DeviceID, ApplicationKind, ResourceID, Status string
	DeviceIDs                                                                []string
	Version, CreatedAt                                                       int64
	Body                                                                     json.RawMessage
}

type StorageTx interface {
	Get(kind, id string) (StorageDocument, error)
	Put(StorageDocument, int64) error
	List(kind string, filter model.AnalysisFilter) ([]StorageDocument, int, error)
}

// Transactions serialize each tenant. PostgreSQL uses transaction advisory
// locks plus CAS; reads use a repeatable-read transaction on primary storage.
type StorageBackend interface {
	Transaction(context.Context, string, func(StorageTx) error) error
	Read(context.Context, string, func(StorageTx) error) error
	WorkTenants(context.Context, []string) ([]string, error)
}

type Store struct {
	backend StorageBackend
	now     func() time.Time
}

var _ ports.AnalysisStore = (*Store)(nil)

func (s *Store) timestamp(tx StorageTx) int64 {
	if clock, ok := tx.(interface{ TransactionTime() int64 }); ok {
		return clock.TransactionTime()
	}
	return s.now().UTC().UnixMilli()
}

func NewStore(backend StorageBackend) *Store { return &Store{backend: backend, now: time.Now} }

func NormalizeAnalysisPage(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func AnalysisHash(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	// Re-encoding eliminates insignificant whitespace and map key order.
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	if err = decoder.Decode(&decoded); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(canonical)
	return hex.EncodeToString(h[:]), nil
}

func TerminalAnalysisStatus(status string) bool {
	return slices.Contains([]string{model.AnalysisSucceeded, model.AnalysisPartial, model.AnalysisFailed, model.AnalysisCancelled}, status)
}

func explicitDevices(ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("%w: explicit devices required", model.ErrAnalysisInvalid)
	}
	copyIDs := slices.Clone(ids)
	for _, id := range copyIDs {
		if strings.TrimSpace(id) == "" {
			return nil, model.ErrAnalysisInvalid
		}
	}
	slices.Sort(copyIDs)
	return slices.Compact(copyIDs), nil
}

func bodyDocument(kind, id, tenant string, v any) (StorageDocument, error) {
	b, err := json.Marshal(v)
	return StorageDocument{Kind: kind, ID: id, TenantID: tenant, Body: b}, err
}

func load[T any](tx StorageTx, kind, id string) (v T, err error) {
	d, err := tx.Get(kind, id)
	if err != nil {
		return v, err
	}
	err = json.Unmarshal(d.Body, &v)
	return
}

func save(tx StorageTx, d StorageDocument, expected int64) error {
	if d.TenantID == "" || d.ID == "" {
		return model.ErrAnalysisInvalid
	}
	d.Version = expected + 1
	return tx.Put(d, expected)
}

func saveRun(tx StorageTx, r model.AnalysisRun, expected int64) error {
	d, err := bodyDocument("run", r.ID, r.TenantID, r)
	if err != nil {
		return err
	}
	d.RunID, d.ApplicationKind, d.Status, d.DeviceIDs, d.CreatedAt = r.ID, r.Kind, r.Status, r.DeviceIDs, r.CreatedAt
	return save(tx, d, expected)
}

func (s *Store) CreateAnalysisRun(ctx context.Context, r model.AnalysisRun, queueLimit int) (out model.AnalysisRun, err error) {
	if r.ID == "" || r.TenantID == "" || r.Kind == "" || r.Creator == "" || r.Start < 0 || r.Start >= r.End || r.ConfigurationVersion == "" || r.AlgorithmVersion == "" || r.IdempotencyKey == "" {
		return out, model.ErrAnalysisInvalid
	}
	if r.DeviceIDs, err = explicitDevices(r.DeviceIDs); err != nil {
		return out, err
	}
	if queueLimit <= 0 {
		queueLimit = 100
	}
	r.Parameters = slices.Clone(r.Parameters)
	if len(r.Parameters) != 0 && !json.Valid(r.Parameters) {
		return out, model.ErrAnalysisInvalid
	}
	request := struct {
		Kind, Creator, PermissionsVersion, Config, Algorithm, Previous string
		Session                                                        int64
		Managed                                                        bool
		Devices                                                        []string
		Start, End                                                     int64
		Parameters                                                     json.RawMessage
	}{r.Kind, r.Creator, r.PermissionsVersion, r.ConfigurationVersion, r.AlgorithmVersion, r.PreviousRunID, r.CreatorSessionVersion, r.CreatorManaged, r.DeviceIDs, r.Start, r.End, r.Parameters}
	r.RequestHash, err = AnalysisHash(request)
	if err != nil {
		return out, err
	}
	key, _ := AnalysisHash(struct{ Kind, Creator, Key string }{r.Kind, r.Creator, r.IdempotencyKey})
	err = s.backend.Transaction(ctx, r.TenantID, func(tx StorageTx) error {
		if old, e := tx.Get("request", key); e == nil {
			var ref struct{ RunID, Hash string }
			if e = json.Unmarshal(old.Body, &ref); e != nil {
				return e
			}
			if ref.Hash != r.RequestHash {
				return model.ErrAnalysisConflict
			}
			out, e = load[model.AnalysisRun](tx, "run", ref.RunID)
			return e
		} else if e != model.ErrNotFound {
			return e
		}
		if r.PreviousRunID != "" {
			if _, e := load[model.AnalysisRun](tx, "run", r.PreviousRunID); e != nil {
				return e
			}
		}
		_, n, e := tx.List("run", model.AnalysisFilter{Kind: r.Kind, Statuses: []string{model.AnalysisQueued, model.AnalysisPreparing, model.AnalysisRunning}, Limit: 1})
		if e != nil {
			return e
		}
		if n >= queueLimit {
			return model.ErrAnalysisQueueFull
		}
		r.Version, r.Status, r.Stage, r.CreatedAt, r.UpdatedAt = 1, model.AnalysisQueued, "queued", s.timestamp(tx), s.timestamp(tx)
		r.LeaseToken, r.LeaseOwner, r.LeaseExpiresAt, r.Processed, r.CompletedAt, r.SnapshotID, r.Error, r.Checkpoint = 0, "", 0, 0, 0, "", "", nil
		r.InputsFrozen, r.InputHashes, r.Sources, r.DataCutoff = false, nil, nil, 0
		r.StartedAt = 0
		if e = saveRun(tx, r, 0); e != nil {
			return e
		}
		d, _ := bodyDocument("request", key, r.TenantID, struct{ RunID, Hash string }{r.ID, r.RequestHash})
		d.RunID, d.DeviceIDs = r.ID, r.DeviceIDs
		if e = save(tx, d, 0); e != nil {
			return e
		}
		out = r
		return nil
	})
	return
}

func (s *Store) GetAnalysisRun(ctx context.Context, tenant, id string) (out model.AnalysisRun, err error) {
	err = s.backend.Read(ctx, tenant, func(tx StorageTx) error { var e error; out, e = load[model.AnalysisRun](tx, "run", id); return e })
	return
}

func listTyped[T any](s *Store, ctx context.Context, tenant, kind string, filter model.AnalysisFilter) (out []T, total int, err error) {
	out = []T{}
	err = s.backend.Read(ctx, tenant, func(tx StorageTx) error {
		docs, n, e := tx.List(kind, filter)
		total = n
		if e != nil {
			return e
		}
		for _, d := range docs {
			var v T
			if e = json.Unmarshal(d.Body, &v); e != nil {
				return e
			}
			out = append(out, v)
		}
		return nil
	})
	return
}

func (s *Store) ListAnalysisRuns(ctx context.Context, tenant string, f model.AnalysisFilter) ([]model.AnalysisRun, int, error) {
	return listTyped[model.AnalysisRun](s, ctx, tenant, "run", f)
}

func (s *Store) ClaimAnalysisRun(ctx context.Context, worker string, lease time.Duration, kinds []string) (out model.AnalysisRun, err error) {
	if worker == "" || lease < time.Millisecond {
		return out, model.ErrAnalysisInvalid
	}
	if len(kinds) == 0 {
		return out, model.ErrNotFound
	}
	tenants, err := s.backend.WorkTenants(ctx, kinds)
	if err != nil {
		return out, err
	}
	for _, tenant := range tenants {
		err = s.backend.Transaction(ctx, tenant, func(tx StorageTx) error {
			docs := []StorageDocument{}
			for offset := 0; ; {
				page, total, e := tx.List("run", model.AnalysisFilter{Statuses: []string{model.AnalysisQueued, model.AnalysisPreparing, model.AnalysisRunning}, Limit: 100, Offset: offset})
				if e != nil {
					return e
				}
				docs = append(docs, page...)
				offset += len(page)
				if offset >= total {
					break
				}
			}
			var e error
			runs := make([]model.AnalysisRun, 0, len(docs))
			live := map[string]bool{}
			recoverable := map[string]bool{}
			now := s.timestamp(tx)
			for _, d := range docs {
				var r model.AnalysisRun
				if e = json.Unmarshal(d.Body, &r); e != nil {
					return e
				}
				runs = append(runs, r)
				if r.Status != model.AnalysisQueued {
					if r.LeaseExpiresAt > now {
						live[r.Kind] = true
					} else {
						recoverable[r.Kind] = true
					}
				}
			}
			for _, r := range runs {
				if !slices.Contains(kinds, r.Kind) || live[r.Kind] || (r.Status == model.AnalysisQueued && recoverable[r.Kind]) {
					continue
				}
				old := r.Version
				r.Version++
				r.LeaseToken++
				r.LeaseOwner = worker
				r.LeaseExpiresAt = now + lease.Milliseconds()
				r.UpdatedAt = now
				if r.StartedAt == 0 {
					r.StartedAt = now
				}
				if r.Status == model.AnalysisQueued {
					r.Status = model.AnalysisPreparing
					r.Stage = "preparing"
				}
				if e = saveRun(tx, r, old); e != nil {
					return e
				}
				out = r
				return nil
			}
			return model.ErrNotFound
		})
		if err == nil {
			return out, nil
		}
		if err != model.ErrNotFound {
			return out, err
		}
	}
	return out, model.ErrNotFound
}

func validLease(r model.AnalysisRun, token, now int64) error {
	if r.LeaseToken != token || token <= 0 || r.LeaseExpiresAt <= now || (r.Status != model.AnalysisPreparing && r.Status != model.AnalysisRunning) {
		return model.ErrAnalysisLeaseLost
	}
	return nil
}

func (s *Store) RenewAnalysisLease(ctx context.Context, tenant, id string, token int64, lease time.Duration) (out model.AnalysisRun, err error) {
	if lease < time.Millisecond {
		return out, model.ErrAnalysisInvalid
	}
	err = s.backend.Transaction(ctx, tenant, func(tx StorageTx) error {
		r, e := load[model.AnalysisRun](tx, "run", id)
		if e != nil {
			return e
		}
		now := s.timestamp(tx)
		if e = validLease(r, token, now); e != nil {
			return e
		}
		if fence, ok := tx.(interface{ SetLeaseDeadline(int64) }); ok {
			fence.SetLeaseDeadline(r.LeaseExpiresAt)
		}
		old := r.Version
		r.Version++
		r.UpdatedAt = now
		r.LeaseExpiresAt = now + lease.Milliseconds()
		if e = saveRun(tx, r, old); e != nil {
			return e
		}
		out = r
		return nil
	})
	return
}

func putImmutable(tx StorageTx, d StorageDocument) error {
	old, err := tx.Get(d.Kind, d.ID)
	if err == model.ErrNotFound {
		return save(tx, d, 0)
	}
	if err != nil {
		return err
	}
	a, e := AnalysisHash(old.Body)
	if e != nil {
		return e
	}
	b, e := AnalysisHash(d.Body)
	if e != nil {
		return e
	}
	if a != b {
		return model.ErrAnalysisConflict
	}
	return nil
}

func freezeInputs(r *model.AnalysisRun, hashes []string, sources []model.AnalysisSourceCoverage, cutoff int64) error {
	if cutoff <= 0 {
		return fmt.Errorf("%w: input cutoff required", model.ErrAnalysisInvalid)
	}
	for _, hash := range hashes {
		if hash == "" {
			return model.ErrAnalysisInvalid
		}
	}
	for _, source := range sources {
		if source.Source == "" || source.Start > source.End || source.ReadAt <= 0 {
			return model.ErrAnalysisInvalid
		}
	}
	if r.InputsFrozen {
		expected, _ := AnalysisHash(r.Sources)
		actual, _ := AnalysisHash(sources)
		if !slices.Equal(r.InputHashes, hashes) || expected != actual || r.DataCutoff != cutoff {
			return model.ErrAnalysisConflict
		}
		return nil
	}
	r.InputsFrozen, r.InputHashes, r.Sources, r.DataCutoff = true, slices.Clone(hashes), slices.Clone(sources), cutoff
	return nil
}

func (s *Store) CommitAnalysisBatch(ctx context.Context, tenant, id string, token int64, b model.AnalysisBatch) (out model.AnalysisRun, err error) {
	if b.ID == "" || b.Processed < 0 {
		return out, model.ErrAnalysisInvalid
	}
	hash, e := AnalysisHash(b)
	if e != nil {
		return out, e
	}
	err = s.backend.Transaction(ctx, tenant, func(tx StorageTx) error {
		r, e := load[model.AnalysisRun](tx, "run", id)
		if e != nil {
			return e
		}
		if r.LeaseToken != token {
			return model.ErrAnalysisLeaseLost
		}
		batchID, _ := AnalysisHash(struct{ Run, ID string }{id, b.ID})
		if old, e := tx.Get("batch", batchID); e == nil {
			var ref struct{ Hash string }
			if e = json.Unmarshal(old.Body, &ref); e != nil {
				return e
			}
			if ref.Hash != hash {
				return model.ErrAnalysisConflict
			}
			out = r
			return nil
		} else if e != model.ErrNotFound {
			return e
		}
		now := s.timestamp(tx)
		if e = validLease(r, token, now); e != nil {
			return e
		}
		if fence, ok := tx.(interface{ SetLeaseDeadline(int64) }); ok {
			fence.SetLeaseDeadline(r.LeaseExpiresAt)
		}
		if b.Processed < r.Processed {
			return model.ErrAnalysisConflict
		}
		status := b.Status
		if status == "" {
			status = model.AnalysisRunning
		}
		if !slices.Contains([]string{model.AnalysisPreparing, model.AnalysisRunning, model.AnalysisSucceeded, model.AnalysisPartial, model.AnalysisFailed}, status) {
			return model.ErrAnalysisInvalid
		}
		if r.Status == model.AnalysisRunning && status == model.AnalysisPreparing {
			return model.ErrAnalysisConflict
		}
		if (status == model.AnalysisSucceeded || status == model.AnalysisPartial) && b.Snapshot == nil {
			return fmt.Errorf("%w: completed facts need snapshot", model.ErrAnalysisInvalid)
		}
		if b.FreezeInputs {
			if e = freezeInputs(&r, b.InputHashes, b.Sources, b.DataCutoff); e != nil {
				return e
			}
		} else if len(b.InputHashes) > 0 || len(b.Sources) > 0 || b.DataCutoff != 0 {
			return fmt.Errorf("%w: input metadata requires FreezeInputs", model.ErrAnalysisInvalid)
		}
		for _, v := range b.Outputs {
			if v.ID == "" || v.Kind == "" || len(v.Body) == 0 || !json.Valid(v.Body) || (v.DeviceID != "" && !slices.Contains(r.DeviceIDs, v.DeviceID)) || (v.TenantID != "" && v.TenantID != tenant) || (v.RunID != "" && v.RunID != id) {
				return model.ErrAnalysisInvalid
			}
			v.TenantID, v.RunID = tenant, id
			d, _ := bodyDocument("output", v.ID, tenant, v)
			d.RunID, d.DeviceID, d.ApplicationKind, d.DeviceIDs, d.CreatedAt = id, v.DeviceID, v.Kind, r.DeviceIDs, now
			if e = putImmutable(tx, d); e != nil {
				return e
			}
		}
		for _, v := range b.Evidence {
			if v.ID == "" || v.SourceKind == "" || v.SourceID == "" || len(v.Summary) == 0 || !json.Valid(v.Summary) || !slices.Contains(r.DeviceIDs, v.DeviceID) || (v.TenantID != "" && v.TenantID != tenant) || (v.RunID != "" && v.RunID != id) {
				return model.ErrAnalysisInvalid
			}
			v.TenantID, v.RunID = tenant, id
			d, _ := bodyDocument("evidence", v.ID, tenant, v)
			d.RunID, d.DeviceID, d.DeviceIDs, d.CreatedAt = id, v.DeviceID, r.DeviceIDs, now
			if e = putImmutable(tx, d); e != nil {
				return e
			}
		}
		if b.Snapshot != nil {
			v := *b.Snapshot
			if !TerminalAnalysisStatus(status) || v.ID == "" || (v.TenantID != "" && v.TenantID != tenant) || (v.RunID != "" && v.RunID != id) {
				return model.ErrAnalysisInvalid
			}
			if len(v.DeviceIDs) > 0 && !slices.Equal(v.DeviceIDs, r.DeviceIDs) {
				return model.ErrAnalysisInvalid
			}
			if v.Start != 0 && v.Start != r.Start || v.End != 0 && v.End != r.End {
				return model.ErrAnalysisInvalid
			}
			if status == model.AnalysisPartial && len(v.MissingSources)+len(v.AffectedIntervals)+len(v.UncomputableMetrics)+len(v.Limitations) == 0 {
				return model.ErrAnalysisInvalid
			}
			if e = freezeInputs(&r, v.InputHashes, v.Sources, v.DataCutoff); e != nil {
				return e
			}
			if status == model.AnalysisSucceeded {
				for _, source := range v.Sources {
					if !source.Complete {
						return fmt.Errorf("%w: incomplete source requires PARTIAL", model.ErrAnalysisInvalid)
					}
				}
			}
			v.TenantID, v.RunID, v.DeviceIDs, v.Start, v.End, v.Version, v.CreatedAt = tenant, id, slices.Clone(r.DeviceIDs), r.Start, r.End, 1, now
			// The digest covers persisted compact facts and the fixed input/source manifest.
			facts := []json.RawMessage{}
			for _, kind := range []string{"output", "evidence"} {
				offset := 0
				for {
					ds, n, e := tx.List(kind, model.AnalysisFilter{RunID: id, Limit: 100, Offset: offset})
					if e != nil {
						return e
					}
					for _, d := range ds {
						facts = append(facts, d.Body)
					}
					offset += len(ds)
					if offset >= n {
						break
					}
				}
			}
			slices.SortFunc(facts, func(a, b json.RawMessage) int {
				left, _ := AnalysisHash(a)
				right, _ := AnalysisHash(b)
				return strings.Compare(left, right)
			})
			manifest := v
			manifest.FactsHash = ""
			manifest.CreatedAt = 0
			computed, e := AnalysisHash(struct {
				Snapshot model.AnalysisSnapshot
				Facts    []json.RawMessage
			}{manifest, facts})
			if e != nil {
				return e
			}
			if v.FactsHash != "" && v.FactsHash != computed {
				return model.ErrAnalysisConflict
			}
			v.FactsHash = computed
			d, _ := bodyDocument("snapshot", v.ID, tenant, v)
			d.RunID, d.DeviceIDs, d.CreatedAt = id, r.DeviceIDs, now
			if e = putImmutable(tx, d); e != nil {
				return e
			}
			r.SnapshotID = v.ID
		}
		d, _ := bodyDocument("batch", batchID, tenant, struct{ Hash string }{hash})
		d.RunID, d.DeviceIDs, d.CreatedAt = id, r.DeviceIDs, now
		if e = save(tx, d, 0); e != nil {
			return e
		}
		old := r.Version
		r.Version++
		r.Processed, r.Status, r.Stage, r.Error, r.Checkpoint, r.UpdatedAt = b.Processed, status, b.Stage, b.Error, slices.Clone(b.Checkpoint), now
		if TerminalAnalysisStatus(status) {
			r.CompletedAt = now
			r.LeaseOwner = ""
			r.LeaseExpiresAt = 0
		}
		if e = saveRun(tx, r, old); e != nil {
			return e
		}
		out = r
		return nil
	})
	return
}

func (s *Store) StopAnalysisRun(ctx context.Context, tenant, id string, expected int64) (out model.AnalysisRun, err error) {
	err = s.backend.Transaction(ctx, tenant, func(tx StorageTx) error {
		r, e := load[model.AnalysisRun](tx, "run", id)
		if e != nil {
			return e
		}
		if r.Version != expected {
			return model.ErrAnalysisConflict
		}
		if TerminalAnalysisStatus(r.Status) {
			out = r
			return nil
		}
		old := r.Version
		r.Version++
		r.LeaseToken++
		r.Status = model.AnalysisCancelled
		r.Stage = "cancelled"
		r.LeaseOwner = ""
		r.LeaseExpiresAt = 0
		r.CompletedAt = s.timestamp(tx)
		r.UpdatedAt = r.CompletedAt
		if e = saveRun(tx, r, old); e != nil {
			return e
		}
		out = r
		return nil
	})
	return
}

func (s *Store) GetAnalysisSnapshot(ctx context.Context, tenant, id string) (out model.AnalysisSnapshot, err error) {
	err = s.backend.Read(ctx, tenant, func(tx StorageTx) error {
		var e error
		out, e = load[model.AnalysisSnapshot](tx, "snapshot", id)
		return e
	})
	return
}
func (s *Store) ListAnalysisEvidence(ctx context.Context, tenant string, f model.AnalysisFilter) ([]model.AnalysisEvidence, int, error) {
	return listTyped[model.AnalysisEvidence](s, ctx, tenant, "evidence", f)
}
func (s *Store) ListAnalysisOutputs(ctx context.Context, tenant string, f model.AnalysisFilter) ([]model.AnalysisOutput, int, error) {
	return listTyped[model.AnalysisOutput](s, ctx, tenant, "output", f)
}

func (s *Store) PutAnalysisConfig(ctx context.Context, v model.AnalysisConfigRevision, expected int64) (out model.AnalysisConfigRevision, err error) {
	if v.ID == "" || v.Kind == "" || v.ResourceID == "" || v.TenantID == "" || v.Creator == "" || !json.Valid(v.Body) || expected < 0 {
		return out, model.ErrAnalysisInvalid
	}
	if v.DeviceIDs, err = explicitDevices(v.DeviceIDs); err != nil {
		return out, err
	}
	if !slices.Contains([]string{"PERSONAL", "SHARED"}, v.Scope) {
		return out, model.ErrAnalysisInvalid
	}
	v.Hash, err = AnalysisHash(v.Body)
	if err != nil {
		return out, err
	}
	owner := ""
	if v.Scope == "PERSONAL" {
		owner = v.Creator
	}
	pointerID, _ := AnalysisHash(struct{ Kind, Resource, Scope, Owner string }{v.Kind, v.ResourceID, v.Scope, owner})
	err = s.backend.Transaction(ctx, v.TenantID, func(tx StorageTx) error {
		pointer, e := tx.Get("config-pointer", pointerID)
		current := int64(0)
		if e == nil {
			current = pointer.Version
		} else if e != model.ErrNotFound {
			return e
		}
		if current != expected {
			return model.ErrAnalysisConflict
		}
		v.Version = expected + 1
		v.CreatedAt = s.timestamp(tx)
		d, _ := bodyDocument("config", v.ID, v.TenantID, v)
		d.ApplicationKind, d.ResourceID, d.DeviceIDs, d.CreatedAt = v.Kind, v.ResourceID, v.DeviceIDs, v.CreatedAt
		if e = save(tx, d, 0); e != nil {
			return e
		}
		p, _ := bodyDocument("config-pointer", pointerID, v.TenantID, struct{ ID string }{v.ID})
		p.DeviceIDs = v.DeviceIDs
		if e = save(tx, p, current); e != nil {
			return e
		}
		out = v
		return nil
	})
	return
}
func (s *Store) GetAnalysisConfig(ctx context.Context, tenant, id string) (out model.AnalysisConfigRevision, err error) {
	err = s.backend.Read(ctx, tenant, func(tx StorageTx) error {
		var e error
		out, e = load[model.AnalysisConfigRevision](tx, "config", id)
		return e
	})
	return
}
func (s *Store) ListAnalysisConfigs(ctx context.Context, tenant string, f model.AnalysisFilter) ([]model.AnalysisConfigRevision, int, error) {
	return listTyped[model.AnalysisConfigRevision](s, ctx, tenant, "config", f)
}

func (s *Store) AppendAnalysisReview(ctx context.Context, v model.AnalysisReview, expected int64) (out model.AnalysisReview, err error) {
	if v.ID == "" || v.TenantID == "" || v.RunID == "" || v.Reviewer == "" || v.ResourceID == "" || v.IdempotencyKey == "" || v.ResourceVersion <= 0 || v.Result == "" {
		return out, model.ErrAnalysisInvalid
	}
	err = s.backend.Transaction(ctx, v.TenantID, func(tx StorageTx) error {
		r, e := load[model.AnalysisRun](tx, "run", v.RunID)
		if e != nil {
			return e
		}
		if r.Version != expected {
			return model.ErrAnalysisConflict
		}
		if !TerminalAnalysisStatus(r.Status) || r.SnapshotID == "" {
			return model.ErrAnalysisConflict
		}
		found := false
		for _, kind := range []string{"output", "snapshot"} {
			d, e := tx.Get(kind, v.ResourceID)
			if e == nil {
				if d.RunID != v.RunID || d.Version != v.ResourceVersion {
					return model.ErrAnalysisConflict
				}
				found = true
				break
			} else if e != model.ErrNotFound {
				return e
			}
		}
		if !found {
			return model.ErrNotFound
		}
		if v.CorrectsID != "" {
			old, e := load[model.AnalysisReview](tx, "review", v.CorrectsID)
			if e != nil {
				return e
			}
			if old.RunID != v.RunID || old.ResourceID != v.ResourceID {
				return model.ErrAnalysisInvalid
			}
		}
		v.ReviewedAt = s.timestamp(tx)
		d, _ := bodyDocument("review", v.ID, v.TenantID, v)
		d.RunID, d.ResourceID, d.DeviceIDs, d.CreatedAt = v.RunID, v.ResourceID, r.DeviceIDs, v.ReviewedAt
		key, _ := AnalysisHash(struct{ Run, Actor, Key string }{v.RunID, v.Reviewer, v.IdempotencyKey})
		old, e := tx.Get("review-key", key)
		if e == nil {
			var saved model.AnalysisReview
			if e = json.Unmarshal(old.Body, &saved); e != nil {
				return e
			}
			candidate := v
			candidate.ID = saved.ID
			candidate.ReviewedAt = saved.ReviewedAt
			a, _ := AnalysisHash(candidate)
			b, _ := AnalysisHash(saved)
			if a != b {
				return model.ErrAnalysisConflict
			}
			out = saved
			return nil
		} else if e != model.ErrNotFound {
			return e
		}
		if e = save(tx, d, 0); e != nil {
			return e
		}
		k, _ := bodyDocument("review-key", key, v.TenantID, v)
		k.RunID, k.DeviceIDs = v.RunID, r.DeviceIDs
		if e = save(tx, k, 0); e != nil {
			return e
		}
		out = v
		return nil
	})
	return
}
func (s *Store) ListAnalysisReviews(ctx context.Context, tenant string, f model.AnalysisFilter) ([]model.AnalysisReview, int, error) {
	return listTyped[model.AnalysisReview](s, ctx, tenant, "review", f)
}

func (s *Store) PutAnalysisAIRevision(ctx context.Context, v model.AnalysisAIRevision, expected int64) (out model.AnalysisAIRevision, err error) {
	if v.ID == "" || v.TenantID == "" || v.RunID == "" || v.SnapshotID == "" || v.WorkflowID == "" || v.PromptVersion == "" || expected < 0 || !slices.Contains([]string{model.AnalysisQueued, model.AnalysisRunning, model.AnalysisSucceeded, model.AnalysisFailed, model.AnalysisCancelled}, v.Status) {
		return out, model.ErrAnalysisInvalid
	}
	err = s.backend.Transaction(ctx, v.TenantID, func(tx StorageTx) error {
		r, e := load[model.AnalysisRun](tx, "run", v.RunID)
		if e != nil {
			return e
		}
		if (r.Status != model.AnalysisSucceeded && r.Status != model.AnalysisPartial) || r.SnapshotID != v.SnapshotID {
			return model.ErrAnalysisConflict
		}
		snap, e := load[model.AnalysisSnapshot](tx, "snapshot", v.SnapshotID)
		if e != nil {
			return e
		}
		if snap.Version != v.SnapshotVersion || snap.RunID != v.RunID {
			return model.ErrAnalysisConflict
		}
		if expected == 0 {
			if v.Status != model.AnalysisQueued {
				return model.ErrAnalysisInvalid
			}
			docs, _, e := tx.List("ai", model.AnalysisFilter{RunID: v.RunID, Statuses: []string{model.AnalysisQueued, model.AnalysisRunning}, Limit: 100})
			if e != nil {
				return e
			}
			for _, d := range docs {
				var existing model.AnalysisAIRevision
				if e = json.Unmarshal(d.Body, &existing); e != nil {
					return e
				}
				if existing.SnapshotID == v.SnapshotID && existing.WorkflowID == v.WorkflowID {
					out = existing
					return nil
				}
			}
			v.CreatedAt = s.timestamp(tx)
		} else {
			old, e := load[model.AnalysisAIRevision](tx, "ai", v.ID)
			if e != nil {
				return e
			}
			if old.Creator != "" || old.Version != expected || old.RunID != v.RunID || old.SnapshotID != v.SnapshotID || old.WorkflowID != v.WorkflowID || old.PromptVersion != v.PromptVersion || old.Model != v.Model || old.PermissionVersion != v.PermissionVersion || TerminalAnalysisStatus(old.Status) {
				return model.ErrAnalysisConflict
			}
			if old.Status == model.AnalysisRunning && v.Status == model.AnalysisQueued {
				return model.ErrAnalysisConflict
			}
			v.CreatedAt = old.CreatedAt
		}
		if v.Status == model.AnalysisSucceeded {
			if !json.Valid(v.Interpretation) {
				return model.ErrAnalysisInvalid
			}
			for _, factID := range v.FactIDs {
				valid := false
				for _, kind := range []string{"output", "evidence"} {
					d, e := tx.Get(kind, factID)
					if e == nil && d.RunID == v.RunID {
						valid = true
						break
					} else if e != nil && e != model.ErrNotFound {
						return e
					}
				}
				if !valid {
					return fmt.Errorf("%w: unknown fact reference", model.ErrAnalysisInvalid)
				}
			}
		}
		v.Version = expected + 1
		if TerminalAnalysisStatus(v.Status) {
			v.CompletedAt = s.timestamp(tx)
		}
		d, _ := bodyDocument("ai", v.ID, v.TenantID, v)
		d.RunID, d.DeviceIDs, d.Status, d.CreatedAt = v.RunID, r.DeviceIDs, v.Status, v.CreatedAt
		if e = save(tx, d, expected); e != nil {
			return e
		}
		out = v
		return nil
	})
	return
}
func (s *Store) GetAnalysisAIRevision(ctx context.Context, tenant, id string) (out model.AnalysisAIRevision, err error) {
	err = s.backend.Read(ctx, tenant, func(tx StorageTx) error { var e error; out, e = load[model.AnalysisAIRevision](tx, "ai", id); return e })
	return
}
func (s *Store) ListAnalysisAIRevisions(ctx context.Context, tenant string, f model.AnalysisFilter) ([]model.AnalysisAIRevision, int, error) {
	return listTyped[model.AnalysisAIRevision](s, ctx, tenant, "ai", f)
}
