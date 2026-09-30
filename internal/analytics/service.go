package analytics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"github.com/google/uuid"
	"iot-platform/internal/config"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type Processor func(context.Context, *Execution) error

type Service struct {
	AI         *AIService
	Store      ports.AnalysisStore
	Limits     config.AnalyticsConfig
	Resolve    ResolveActor
	Device     ValidateDevice
	mu         sync.RWMutex
	processors map[string]Processor
}

func NewService(store ports.AnalysisStore, limits config.AnalyticsConfig, resolve ResolveActor, device ValidateDevice) *Service {
	return &Service{Store: store, Limits: limits.WithDefaults(), Resolve: resolve, Device: device, processors: map[string]Processor{}}
}

func (s *Service) Register(kind string, p Processor) error {
	if Menu(kind) == "" || p == nil {
		return model.ErrAnalysisInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.processors[kind] = p
	return nil
}

func (s *Service) supported(kind string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.processors[kind] != nil
}

func (s *Service) Current(ctx context.Context, a Actor) (Actor, error) {
	if s.Resolve == nil {
		return Actor{}, ErrForbidden
	}
	current, err := s.Resolve(ctx, a)
	if err != nil || current.TenantID != a.TenantID || current.Username != a.Username {
		return Actor{}, ErrForbidden
	}
	return current, nil
}

func (s *Service) authorize(ctx context.Context, a Actor, kind, operation string, ids []string) (Actor, error) {
	current, err := s.Current(ctx, a)
	if err != nil || !current.Allows(kind, operation, ids) {
		return Actor{}, ErrForbidden
	}
	return current, nil
}

type CreateRequest struct {
	DeviceIDs            []string        `json:"deviceIds"`
	Start                int64           `json:"start"`
	End                  int64           `json:"end"`
	ConfigurationVersion string          `json:"configurationVersion"`
	Parameters           json.RawMessage `json:"parameters,omitempty"`
	PreviousRunID        string          `json:"previousRunId,omitempty"`
	IdempotencyKey       string          `json:"idempotencyKey"`
}

func (s *Service) Create(ctx context.Context, a Actor, kind, algorithm string, q CreateRequest) (model.AnalysisRun, error) {
	if !s.supported(kind) {
		return model.AnalysisRun{}, ErrUnsupported
	}
	ids := slices.Clone(q.DeviceIDs)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) == 0 || len(ids) > s.Limits.MaxDevices || ids[0] == "" || q.Start < 0 || q.End <= q.Start || q.End-q.Start > s.Limits.MaxRange.Milliseconds() || q.ConfigurationVersion == "" || algorithm == "" || q.IdempotencyKey == "" || len(q.IdempotencyKey) > 200 {
		return model.AnalysisRun{}, fmt.Errorf("%w: 范围、区间、版本或幂等键不符合限制", model.ErrAnalysisInvalid)
	}
	if len(q.Parameters) == 0 {
		q.Parameters = json.RawMessage(`{}`)
	}
	if !json.Valid(q.Parameters) {
		return model.AnalysisRun{}, model.ErrAnalysisInvalid
	}
	current, err := s.authorize(ctx, a, kind, "POST "+Prefix(kind)+"/runs", ids)
	if err != nil {
		return model.AnalysisRun{}, err
	}
	if s.Device == nil {
		return model.AnalysisRun{}, ErrForbidden
	}
	for _, id := range ids {
		if err = s.Device(ctx, a.TenantID, id); err != nil {
			return model.AnalysisRun{}, ErrForbidden
		}
	}
	if q.PreviousRunID != "" {
		previous, err := s.Get(ctx, a, kind, q.PreviousRunID)
		if err != nil {
			return model.AnalysisRun{}, err
		}
		if previous.Kind != kind {
			return model.AnalysisRun{}, model.ErrAnalysisInvalid
		}
	}
	r := model.AnalysisRun{ID: uuid.NewString(), TenantID: a.TenantID, Kind: kind, Creator: a.Username, CreatorManaged: current.Managed, CreatorSessionVersion: current.SessionVersion, PermissionsVersion: current.AccessVersion, DeviceIDs: ids, Start: q.Start, End: q.End, ConfigurationVersion: q.ConfigurationVersion, AlgorithmVersion: algorithm, Parameters: q.Parameters, PreviousRunID: q.PreviousRunID, IdempotencyKey: q.IdempotencyKey}
	payload, _ := json.Marshal([]any{r.TenantID, r.Kind, r.Creator, r.PermissionsVersion, ids, r.Start, r.End, r.ConfigurationVersion, r.AlgorithmVersion, r.Parameters, r.PreviousRunID})
	sum := sha256.Sum256(payload)
	r.RequestHash = hex.EncodeToString(sum[:])
	return s.Store.CreateAnalysisRun(ctx, r, s.Limits.QueueLimit)
}

func (s *Service) Get(ctx context.Context, a Actor, kind, id string) (model.AnalysisRun, error) {
	r, err := s.Store.GetAnalysisRun(ctx, a.TenantID, id)
	if err != nil {
		return r, err
	}
	if r.Kind != kind {
		return model.AnalysisRun{}, model.ErrNotFound
	}
	_, err = s.authorize(ctx, a, kind, "", r.DeviceIDs)
	return r, err
}

func (s *Service) List(ctx context.Context, a Actor, kind string, f model.AnalysisFilter) ([]model.AnalysisRun, int, error) {
	current, err := s.Current(ctx, a)
	if err != nil {
		return nil, 0, err
	}
	if !slices.Contains(current.Permissions, "*") && (!slices.Contains(current.Permissions, "menu:devices") || !slices.Contains(current.Permissions, "menu:"+Menu(kind))) {
		return nil, 0, ErrForbidden
	}
	f.Kind = kind
	if !current.AllDevices {
		f.DeviceScopeSet = true
		f.DeviceIDs = current.DeviceIDs
	}
	return s.Store.ListAnalysisRuns(ctx, a.TenantID, f)
}

func (s *Service) Stop(ctx context.Context, a Actor, kind, id string, expected int64) (model.AnalysisRun, error) {
	r, err := s.Get(ctx, a, kind, id)
	if err != nil {
		return r, err
	}
	if _, err = s.authorize(ctx, a, kind, "POST "+Prefix(kind)+"/runs/:id/stop", r.DeviceIDs); err != nil {
		return r, err
	}
	return s.Store.StopAnalysisRun(ctx, a.TenantID, id, expected)
}

func (s *Service) Snapshot(ctx context.Context, a Actor, kind, id string) (model.AnalysisSnapshot, error) {
	r, err := s.Get(ctx, a, kind, id)
	if err != nil {
		return model.AnalysisSnapshot{}, err
	}
	if r.SnapshotID == "" {
		return model.AnalysisSnapshot{}, model.ErrNotFound
	}
	return s.Store.GetAnalysisSnapshot(ctx, a.TenantID, r.SnapshotID)
}

func (s *Service) Outputs(ctx context.Context, a Actor, kind, id string, f model.AnalysisFilter) ([]model.AnalysisOutput, int, error) {
	if _, err := s.Get(ctx, a, kind, id); err != nil {
		return nil, 0, err
	}
	f.RunID = id
	return s.Store.ListAnalysisOutputs(ctx, a.TenantID, f)
}

func (s *Service) Evidence(ctx context.Context, a Actor, kind, id string, f model.AnalysisFilter) ([]model.AnalysisEvidence, int, error) {
	if _, err := s.Get(ctx, a, kind, id); err != nil {
		return nil, 0, err
	}
	f.RunID = id
	return s.Store.ListAnalysisEvidence(ctx, a.TenantID, f)
}
