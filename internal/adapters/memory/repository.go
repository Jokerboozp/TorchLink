package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

var ErrNotFound = errors.New("not found")

type Repository struct {
	twins               map[string]model.TwinTopology
	edgeReadJobs        map[string]model.EdgeReadJob
	rawReservations     map[string]rawReservation
	edgeSecrets         map[string]string
	edgeHeartbeats      map[string]model.EdgeHeartbeat
	leases              map[string]model.ExecutionLease
	edgeNodes           map[string]model.EdgeNode
	revocations         map[string]model.CredentialRevocation
	commands            map[string]model.DeviceCommand
	mu                  sync.RWMutex
	shadows             map[string]model.DeviceShadow
	shadowChanges       map[string][]model.ShadowChange
	raw                 map[string]model.RawArchiveIndex
	rawMessages         map[string]model.RawMessage
	standard            map[string]model.StandardMessage
	standardProcessed   map[string]bool
	rulePending         map[string]int64
	states              map[string]model.DeviceState
	stateEvents         []model.DeviceStateEvent
	rules               map[string]model.AlarmRule
	alarms              map[string]model.Alarm
	video               map[string]model.VideoAlarmEvent
	videoMappings       map[string]model.VideoCameraMapping
	videoRelations      map[string][]model.VideoCameraRelation
	ai                  map[string]model.AIAnalysis
	knowledge           map[string]model.KnowledgeDoc
	workflowKnowledge   map[string]model.WorkflowKnowledgeBinding
	replays             map[string]model.ReplayRequest
	audits              []model.AuditLog
	aiToolCalls         []model.AIToolCallLog
	aiProviderConfig    *ports.AIPluginConfig
	products            map[string]model.Product
	protocols           map[string]model.ProtocolPackage
	protocolDefinitions map[string]model.ProtocolDefinition
	protocolReleases    map[string]model.ProtocolRelease
	pointTables         map[string]model.PointTableRelease
	protocolBindings    map[string]model.ProductProtocolBinding
	accessProfiles      map[string]model.DeviceAccessProfile
	devices             map[string]model.ManagedDevice
}

func NewRepository() *Repository {
	return &Repository{raw: map[string]model.RawArchiveIndex{}, rawMessages: map[string]model.RawMessage{}, standard: map[string]model.StandardMessage{}, standardProcessed: map[string]bool{}, rulePending: map[string]int64{}, states: map[string]model.DeviceState{}, rules: map[string]model.AlarmRule{}, alarms: map[string]model.Alarm{}, video: map[string]model.VideoAlarmEvent{}, videoMappings: map[string]model.VideoCameraMapping{}, videoRelations: map[string][]model.VideoCameraRelation{}, ai: map[string]model.AIAnalysis{}, knowledge: map[string]model.KnowledgeDoc{}, workflowKnowledge: map[string]model.WorkflowKnowledgeBinding{}, replays: map[string]model.ReplayRequest{}, products: map[string]model.Product{}, protocols: map[string]model.ProtocolPackage{}, protocolDefinitions: map[string]model.ProtocolDefinition{}, protocolReleases: map[string]model.ProtocolRelease{}, pointTables: map[string]model.PointTableRelease{}, protocolBindings: map[string]model.ProductProtocolBinding{}, accessProfiles: map[string]model.DeviceAccessProfile{}, devices: map[string]model.ManagedDevice{}}
}

func key(parts ...string) string { return strings.Join(parts, "\x00") }

func (r *Repository) SaveProduct(_ context.Context, v model.Product) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.products[key(v.TenantID, v.ID)] = clone(v)
	return nil
}
func (r *Repository) GetProduct(_ context.Context, tenant, id string) (model.Product, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.products[key(tenant, id)]
	if !ok {
		return v, ErrNotFound
	}
	return clone(v), nil
}
func (r *Repository) ListProducts(_ context.Context, tenant string) ([]model.Product, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.Product{}
	for _, v := range r.products {
		if tenant == "" || v.TenantID == tenant {
			out = append(out, clone(v))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}
func (r *Repository) ListProductsPage(ctx context.Context, tenant string, limit, offset int) ([]model.Product, int, error) {
	items, err := r.ListProducts(ctx, tenant)
	if err != nil {
		return nil, 0, err
	}
	return page(items, offset, limit), len(items), nil
}
func (r *Repository) SaveProtocolPackage(_ context.Context, v model.ProtocolPackage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.protocols[key(v.TenantID, v.ID)] = clone(v)
	return nil
}
func (r *Repository) GetProtocolPackage(_ context.Context, tenant, id string) (model.ProtocolPackage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.protocols[key(tenant, id)]
	if !ok {
		return v, ErrNotFound
	}
	return clone(v), nil
}
func (r *Repository) ListProtocolPackages(_ context.Context, tenant string) ([]model.ProtocolPackage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.ProtocolPackage{}
	for _, v := range r.protocols {
		if tenant == "" || v.TenantID == tenant {
			out = append(out, clone(v))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}
func (r *Repository) ListProtocolPackagesPage(ctx context.Context, tenant string, limit, offset int) ([]model.ProtocolPackage, int, error) {
	items, err := r.ListProtocolPackages(ctx, tenant)
	if err != nil {
		return nil, 0, err
	}
	return page(items, offset, limit), len(items), nil
}

func (r *Repository) SaveProtocolDefinition(_ context.Context, v model.ProtocolDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.protocolDefinitions[key(v.TenantID, v.ID)] = clone(v)
	return nil
}
func (r *Repository) GetProtocolDefinition(_ context.Context, tenant, id string) (model.ProtocolDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.protocolDefinitions[key(tenant, id)]
	if !ok {
		return v, ErrNotFound
	}
	return clone(v), nil
}
func (r *Repository) ListProtocolDefinitions(_ context.Context, tenant string) ([]model.ProtocolDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.ProtocolDefinition{}
	for _, v := range r.protocolDefinitions {
		if tenant == "" || v.TenantID == tenant {
			out = append(out, clone(v))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}
func (r *Repository) CreateProtocolRelease(_ context.Context, v model.ProtocolRelease) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(v.TenantID, v.ProtocolID, v.Version)
	if _, exists := r.protocolReleases[k]; exists {
		return errors.New("protocol release already exists")
	}
	r.protocolReleases[k] = clone(v)
	return nil
}
func (r *Repository) GetProtocolRelease(_ context.Context, tenant, protocolID, version string) (model.ProtocolRelease, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.protocolReleases[key(tenant, protocolID, version)]
	if !ok {
		return v, ErrNotFound
	}
	return clone(v), nil
}
func (r *Repository) ListProtocolReleases(_ context.Context, tenant, protocolID string) ([]model.ProtocolRelease, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.ProtocolRelease{}
	for _, v := range r.protocolReleases {
		if (tenant == "" || v.TenantID == tenant) && (protocolID == "" || v.ProtocolID == protocolID) {
			out = append(out, clone(v))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ProtocolID == out[j].ProtocolID {
			return out[i].CreatedAt > out[j].CreatedAt
		}
		return out[i].ProtocolID < out[j].ProtocolID
	})
	return out, nil
}
func (r *Repository) UpdateProtocolReleaseStatus(_ context.Context, tenant, protocolID, version, status string, publishedAt int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(tenant, protocolID, version)
	v, ok := r.protocolReleases[k]
	if !ok {
		return ErrNotFound
	}
	v.Status, v.PublishedAt = status, publishedAt
	r.protocolReleases[k] = clone(v)
	return nil
}
func (r *Repository) CreatePointTableRelease(_ context.Context, v model.PointTableRelease) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(v.TenantID, v.ProtocolID, v.Version)
	if _, exists := r.pointTables[k]; exists {
		return errors.New("point table release already exists")
	}
	r.pointTables[k] = clone(v)
	return nil
}
func (r *Repository) GetPointTableRelease(_ context.Context, tenant, protocolID, version string) (model.PointTableRelease, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.pointTables[key(tenant, protocolID, version)]
	if !ok {
		return v, ErrNotFound
	}
	return clone(v), nil
}
func (r *Repository) SaveProductProtocolBinding(_ context.Context, v model.ProductProtocolBinding) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.protocolBindings[key(v.TenantID, v.ProductID)] = clone(v)
	return nil
}
func (r *Repository) GetProductProtocolBinding(_ context.Context, tenant, productID string) (model.ProductProtocolBinding, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.protocolBindings[key(tenant, productID)]
	if !ok {
		return v, ErrNotFound
	}
	return clone(v), nil
}
func (r *Repository) SaveDeviceAccessProfile(_ context.Context, v model.DeviceAccessProfile) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accessProfiles[key(v.TenantID, v.ID)] = clone(v)
	return nil
}
func (r *Repository) GetDeviceAccessProfile(_ context.Context, tenant, id string) (model.DeviceAccessProfile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.accessProfiles[key(tenant, id)]
	if !ok {
		return v, ErrNotFound
	}
	return clone(v), nil
}
func (r *Repository) ListDeviceAccessProfiles(_ context.Context, tenant string) ([]model.DeviceAccessProfile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.DeviceAccessProfile{}
	for _, v := range r.accessProfiles {
		if tenant == "" || v.TenantID == tenant {
			out = append(out, clone(v))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}
func (r *Repository) SaveManagedDevice(_ context.Context, v model.ManagedDevice) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, current := range r.devices {
		if current.AccessKey == v.AccessKey && (current.TenantID != v.TenantID || current.ID != v.ID) {
			return fmt.Errorf("access key already exists")
		}
	}
	r.devices[key(v.TenantID, v.ID)] = cloneManaged(v)
	return nil
}
func (r *Repository) GetManagedDevice(_ context.Context, tenant, id string) (model.ManagedDevice, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.devices[key(tenant, id)]
	if !ok {
		return v, ErrNotFound
	}
	return cloneManaged(v), nil
}
func (r *Repository) GetManagedDeviceByAccessKey(_ context.Context, accessKey string) (model.ManagedDevice, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, v := range r.devices {
		if v.AccessKey == accessKey {
			return cloneManaged(v), nil
		}
	}
	return model.ManagedDevice{}, ErrNotFound
}
func (r *Repository) ListManagedDevices(_ context.Context, tenant string) ([]model.ManagedDevice, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.ManagedDevice{}
	for _, v := range r.devices {
		if tenant == "" || v.TenantID == tenant {
			out = append(out, cloneManaged(v))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}
func (r *Repository) ListManagedDevicesPage(ctx context.Context, tenant string, limit, offset int) ([]model.ManagedDevice, int, error) {
	items, err := r.ListManagedDevices(ctx, tenant)
	if err != nil {
		return nil, 0, err
	}
	return page(items, offset, limit), len(items), nil
}
func (r *Repository) CountManagedDeviceChildren(_ context.Context, tenant string, ids []string) (map[string]int, error) {
	counts := make(map[string]int, len(ids))
	if len(ids) == 0 {
		return counts, nil
	}
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id != "" {
			wanted[id] = struct{}{}
		}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, device := range r.devices {
		if device.TenantID == tenant && device.GatewayID != "" {
			if _, ok := wanted[device.GatewayID]; ok {
				counts[device.GatewayID]++
			}
		}
	}
	return counts, nil
}

func (r *Repository) SaveRawIndex(_ context.Context, v model.RawArchiveIndex) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.raw[key(v.TenantID, v.MessageID)]; ok {
		return false, nil
	}
	r.raw[key(v.TenantID, v.MessageID)] = v
	return true, nil
}

func (r *Repository) SaveRawMessage(_ context.Context, v model.RawMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := key(v.TenantID, v.MessageID)
	if old, exists := r.rawMessages[key]; exists {
		if old.PayloadHash() != v.PayloadHash() || old.DeviceID != v.DeviceID || old.ProductID != v.ProductID {
			return model.ErrRawConflict
		}
	} else {
		r.rawMessages[key] = clone(v)
	}
	return nil
}

func (r *Repository) GetRawMessage(_ context.Context, tenant, messageID string) (model.RawMessage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.rawMessages[key(tenant, messageID)]
	if !ok {
		return model.RawMessage{}, ErrNotFound
	}
	return clone(v), nil
}

func (r *Repository) MarkRawPublished(_ context.Context, tenant, messageID string, publishedAt int64, lastError string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(tenant, messageID)
	v, ok := r.raw[k]
	if !ok {
		return ErrNotFound
	}
	v.PublishAttempts++
	v.LastPublishError = lastError
	if lastError == "" {
		v.PublishedAt = publishedAt
	}
	r.raw[k] = v
	return nil
}
func (r *Repository) ListPendingRawIndexes(_ context.Context, limit int) ([]model.RawArchiveIndex, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.RawArchiveIndex{}
	for _, v := range r.raw {
		if v.PublishedAt == 0 {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ArchivedAt < out[j].ArchivedAt })
	return page(out, 0, limit), nil
}
func (r *Repository) GetRawIndex(_ context.Context, tenant, messageID string) (model.RawArchiveIndex, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.raw[key(tenant, messageID)]
	if !ok {
		return v, ErrNotFound
	}
	return v, nil
}
func (r *Repository) ListRawIndexes(_ context.Context, f ports.RawFilter) ([]model.RawArchiveIndex, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]model.RawArchiveIndex, 0)
	for _, v := range r.raw {
		if f.TenantID != "" && v.TenantID != f.TenantID || f.ProductID != "" && v.ProductID != f.ProductID || f.DeviceID != "" && v.DeviceID != f.DeviceID || f.Start > 0 && v.ReceivedAt < f.Start || f.End > 0 && v.ReceivedAt > f.End {
			continue
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ReceivedAt > out[j].ReceivedAt })
	return page(out, f.Offset, f.Limit), nil
}
func (r *Repository) CountRawIndexes(_ context.Context, f ports.RawFilter) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, v := range r.raw {
		if f.TenantID != "" && v.TenantID != f.TenantID || f.ProductID != "" && v.ProductID != f.ProductID || f.DeviceID != "" && v.DeviceID != f.DeviceID || f.Start > 0 && v.ReceivedAt < f.Start || f.End > 0 && v.ReceivedAt > f.End {
			continue
		}
		count++
	}
	return count, nil
}
func page[T any](v []T, offset, limit int) []T {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(v) {
		return []T{}
	}
	if limit <= 0 {
		limit = 100
	}
	end := offset + limit
	if end > len(v) {
		end = len(v)
	}
	return v[offset:end]
}
func (r *Repository) SaveStandardMessage(ctx context.Context, v model.StandardMessage) error {
	_, err := r.SaveStandardMessageIfAbsent(ctx, v)
	return err
}
func (r *Repository) SaveStandardMessageIfAbsent(_ context.Context, v model.StandardMessage) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(v.TenantID, v.MessageID)
	if _, exists := r.standard[k]; exists {
		return false, nil
	}
	r.standard[k] = v
	r.standardProcessed[k] = false
	return true, nil
}
func (r *Repository) ClaimStandardMessage(_ context.Context, v model.StandardMessage) (bool, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(v.TenantID, v.MessageID)
	if _, exists := r.standard[k]; !exists {
		r.standard[k] = v
		r.standardProcessed[k] = false
		return true, true, nil
	}
	return !r.standardProcessed[k], false, nil
}
func (r *Repository) MarkStandardMessageProcessed(_ context.Context, tenant, messageID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(tenant, messageID)
	if _, exists := r.standard[k]; !exists {
		return ErrNotFound
	}
	r.standardProcessed[k] = true
	return nil
}
func (r *Repository) GetStandardMessageByRaw(_ context.Context, tenant, rawID string) (model.StandardMessage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, v := range r.standard {
		if v.TenantID == tenant && v.RawMessageID == rawID {
			return clone(v), nil
		}
	}
	return model.StandardMessage{}, ErrNotFound
}
func (r *Repository) GetLatestMessage(_ context.Context, tenant, device string) (model.StandardMessage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out model.StandardMessage
	found := false
	for _, v := range r.standard {
		if v.TenantID == tenant && v.DeviceID == device && (!found || v.Timestamp > out.Timestamp) {
			out, found = clone(v), true
		}
	}
	if !found {
		return out, ErrNotFound
	}
	return out, nil
}
func (r *Repository) PropertyHistory(_ context.Context, tenant, device, property string, start, end int64, limit int) ([]map[string]any, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []map[string]any{}
	for _, v := range r.standard {
		if v.TenantID != tenant || v.DeviceID != device || v.Timestamp < start || (end > 0 && v.Timestamp > end) {
			continue
		}
		value, ok := v.Properties[property]
		if !ok {
			continue
		}
		out = append(out, map[string]any{"timestamp": v.Timestamp, "value": value, "messageId": v.MessageID})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["timestamp"].(int64) < out[j]["timestamp"].(int64) })
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}
func (r *Repository) PropertyHistoryPage(ctx context.Context, tenant, device, property string, start, end int64, limit, offset int) ([]map[string]any, int, error) {
	items, err := r.PropertyHistory(ctx, tenant, device, property, start, end, 0)
	if err != nil {
		return nil, 0, err
	}
	total := len(items)
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	endIndex := total - offset
	if endIndex <= 0 {
		return []map[string]any{}, total, nil
	}
	startIndex := endIndex - limit
	if startIndex < 0 {
		startIndex = 0
	}
	return items[startIndex:endIndex], total, nil
}
func (r *Repository) UpsertDeviceState(_ context.Context, v model.DeviceState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states[key(v.TenantID, v.DeviceID)] = v
	return nil
}
func (r *Repository) GetDeviceState(_ context.Context, tenant, device string) (model.DeviceState, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.states[key(tenant, device)]
	if !ok {
		return v, ErrNotFound
	}
	return v, nil
}
func (r *Repository) ListDeviceStates(_ context.Context, tenant string) ([]model.DeviceState, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.DeviceState{}
	for _, v := range r.states {
		if tenant == "" || v.TenantID == tenant {
			out = append(out, v)
		}
	}
	return out, nil
}
func (r *Repository) ListDeviceStatesPage(ctx context.Context, tenant string, limit, offset int) ([]model.DeviceState, int, error) {
	items, err := r.ListDeviceStates(ctx, tenant)
	if err != nil {
		return nil, 0, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].LastSeenAt > items[j].LastSeenAt })
	return page(items, offset, limit), len(items), nil
}
func (r *Repository) ListUnregisteredDeviceStatesPage(ctx context.Context, tenant string, limit, offset int) ([]model.DeviceState, int, error) {
	items, err := r.ListDeviceStates(ctx, tenant)
	if err != nil {
		return nil, 0, err
	}
	r.mu.RLock()
	registered := make(map[string]struct{})
	for _, device := range r.devices {
		if device.TenantID == tenant {
			registered[device.ID] = struct{}{}
		}
	}
	r.mu.RUnlock()
	unregistered := make([]model.DeviceState, 0, len(items))
	for _, item := range items {
		if _, ok := registered[item.DeviceID]; !ok {
			unregistered = append(unregistered, item)
		}
	}
	sort.Slice(unregistered, func(i, j int) bool { return unregistered[i].LastSeenAt > unregistered[j].LastSeenAt })
	return page(unregistered, offset, limit), len(unregistered), nil
}
func (r *Repository) CountDeviceStates(_ context.Context, tenant string, unregisteredOnly bool) (int, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	registered := make(map[string]struct{})
	if unregisteredOnly {
		for _, device := range r.devices {
			if device.TenantID == tenant {
				registered[device.ID] = struct{}{}
			}
		}
	}
	total, online := 0, 0
	for _, state := range r.states {
		if state.TenantID != tenant {
			continue
		}
		if unregisteredOnly {
			if _, ok := registered[state.DeviceID]; ok {
				continue
			}
		}
		total++
		if state.BusinessStatus == "ONLINE" || state.BusinessStatus == "ALARM" {
			online++
		}
	}
	return total, online, nil
}
func (r *Repository) SaveDeviceStateEvent(_ context.Context, v model.DeviceState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stateEvents = append(r.stateEvents, model.DeviceStateEvent{State: clone(v), RecordedAt: time.Now().UnixMilli()})
	return nil
}
func (r *Repository) SaveRule(_ context.Context, v model.AlarmRule) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rules[key(v.TenantID, v.ID)] = v
	return nil
}
func (r *Repository) ListRules(_ context.Context, tenant string) ([]model.AlarmRule, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.AlarmRule{}
	for _, v := range r.rules {
		if tenant == "" || v.TenantID == tenant {
			out = append(out, v)
		}
	}
	return out, nil
}
func (r *Repository) ListRulesPage(ctx context.Context, tenant string, limit, offset int) ([]model.AlarmRule, int, error) {
	items, err := r.ListRules(ctx, tenant)
	if err != nil {
		return nil, 0, err
	}
	return page(items, offset, limit), len(items), nil
}
func (r *Repository) DeleteRule(_ context.Context, tenant, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(tenant, id)
	if _, ok := r.rules[k]; !ok {
		return ErrNotFound
	}
	delete(r.rules, k)
	return nil
}
func (r *Repository) SaveRulePending(_ context.Context, tenant, ruleID, deviceID string, since int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(tenant, ruleID, deviceID)
	if current, ok := r.rulePending[k]; ok && current <= since {
		return nil
	}
	r.rulePending[k] = since
	return nil
}
func (r *Repository) GetRulePending(_ context.Context, tenant, ruleID, deviceID string) (int64, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	since, ok := r.rulePending[key(tenant, ruleID, deviceID)]
	return since, ok, nil
}
func (r *Repository) DeleteRulePending(_ context.Context, tenant, ruleID, deviceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rulePending, key(tenant, ruleID, deviceID))
	return nil
}
func (r *Repository) DeleteRulePendings(_ context.Context, tenant, ruleID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	prefix := key(tenant, ruleID) + "\x00"
	for pendingKey := range r.rulePending {
		if strings.HasPrefix(pendingKey, prefix) {
			delete(r.rulePending, pendingKey)
		}
	}
	return nil
}
func (r *Repository) UpsertAlarm(_ context.Context, v model.Alarm) (model.Alarm, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, a := range r.alarms {
		if a.TenantID == v.TenantID && a.DeviceID == v.DeviceID && a.RuleID == v.RuleID && (a.Status == "ACTIVE" || a.Status == "ACKED") {
			if v.TriggerID != "" && a.TriggerID == v.TriggerID {
				return cloneAlarm(a), false, nil
			}
			a.LastTriggeredAt = v.LastTriggeredAt
			a.TriggerCount++
			if v.Confidence > a.Confidence {
				a.Confidence = v.Confidence
			}
			r.alarms[k] = cloneAlarm(a)
			return cloneAlarm(a), false, nil
		}
	}
	r.alarms[key(v.TenantID, v.ID)] = cloneAlarm(v)
	return cloneAlarm(v), true, nil
}
func (r *Repository) GetAlarm(_ context.Context, tenant, id string) (model.Alarm, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.alarms[key(tenant, id)]
	if !ok {
		return v, ErrNotFound
	}
	return cloneAlarm(v), nil
}
func (r *Repository) ListAlarms(_ context.Context, f ports.AlarmFilter) ([]model.Alarm, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.Alarm{}
	for _, v := range r.alarms {
		if f.TenantID != "" && v.TenantID != f.TenantID || f.DeviceID != "" && v.DeviceID != f.DeviceID || f.Status != "" && v.Status != f.Status || f.Level != "" && v.AlarmLevel != f.Level || f.Source != "" && v.Source != f.Source || f.Start > 0 && v.LastTriggeredAt < f.Start || f.End > 0 && v.LastTriggeredAt > f.End {
			continue
		}
		out = append(out, cloneAlarm(v))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastTriggeredAt > out[j].LastTriggeredAt })
	return page(out, f.Offset, f.Limit), nil
}
func (r *Repository) CountAlarms(_ context.Context, f ports.AlarmFilter) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, v := range r.alarms {
		if f.TenantID != "" && v.TenantID != f.TenantID || f.DeviceID != "" && v.DeviceID != f.DeviceID || f.Status != "" && v.Status != f.Status || f.Level != "" && v.AlarmLevel != f.Level || f.Source != "" && v.Source != f.Source || f.Start > 0 && v.LastTriggeredAt < f.Start || f.End > 0 && v.LastTriggeredAt > f.End {
			continue
		}
		count++
	}
	return count, nil
}
func (r *Repository) UpdateAlarm(_ context.Context, v model.Alarm) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.alarms[key(v.TenantID, v.ID)]; !ok {
		return ErrNotFound
	}
	r.alarms[key(v.TenantID, v.ID)] = cloneAlarm(v)
	return nil
}
func (r *Repository) SaveVideoEvent(_ context.Context, v model.VideoAlarmEvent) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.video[key(v.TenantID, v.EventID)]; ok {
		return false, nil
	}
	r.video[key(v.TenantID, v.EventID)] = cloneVideoEvent(v)
	return true, nil
}
func (r *Repository) UpdateVideoEvent(_ context.Context, v model.VideoAlarmEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.video[key(v.TenantID, v.EventID)] = cloneVideoEvent(v)
	return nil
}
func (r *Repository) ListPendingVideoEvents(_ context.Context, limit int) ([]model.VideoAlarmEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.VideoAlarmEvent{}
	for _, v := range r.video {
		status, _ := v.Raw["mediaTransferStatus"].(string)
		if status == "PENDING" || status == "FAILED" {
			out = append(out, cloneVideoEvent(v))
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
func (r *Repository) SaveVideoCameraMapping(_ context.Context, v model.VideoCameraMapping) error {
	legacyDeviceIDs := uniqueStrings(v.RelatedDeviceIDs)
	if v.DeviceID == "" && len(legacyDeviceIDs) == 1 {
		v.DeviceID = legacyDeviceIDs[0]
	}
	if len(legacyDeviceIDs) > 1 || v.DeviceID != "" && len(legacyDeviceIDs) == 1 && legacyDeviceIDs[0] != v.DeviceID {
		return errors.New("a camera can be associated with at most one device")
	}
	v.RelatedDeviceIDs, v.RelatedFloorIDs, v.RelatedRoomIDs = nil, nil, nil
	r.mu.Lock()
	defer r.mu.Unlock()
	r.videoMappings[key(v.TenantID, v.CameraID)] = clone(v)
	relations := make([]model.VideoCameraRelation, 0, 1)
	if v.DeviceID != "" {
		relations = append(relations, model.VideoCameraRelation{TenantID: v.TenantID, CameraID: v.CameraID, RelationType: "device", TargetID: v.DeviceID})
	}
	r.videoRelations[key(v.TenantID, v.CameraID)] = clone(relations)
	return nil
}
func (r *Repository) GetVideoCameraMapping(_ context.Context, tenant, camera string) (model.VideoCameraMapping, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.videoMappings[key(tenant, camera)]
	if !ok {
		return v, ErrNotFound
	}
	return clone(v), nil
}
func (r *Repository) ListVideoCameraMappings(_ context.Context, tenant string) ([]model.VideoCameraMapping, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.VideoCameraMapping{}
	for _, v := range r.videoMappings {
		if tenant == "" || v.TenantID == tenant {
			out = append(out, clone(v))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CameraID < out[j].CameraID })
	return out, nil
}
func (r *Repository) ListVideoCameraMappingsPage(ctx context.Context, tenant string, limit, offset int) ([]model.VideoCameraMapping, int, error) {
	items, err := r.ListVideoCameraMappings(ctx, tenant)
	if err != nil {
		return nil, 0, err
	}
	return page(items, offset, limit), len(items), nil
}
func (r *Repository) ReplaceVideoCameraRelations(_ context.Context, tenant, camera string, relations []model.VideoCameraRelation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copyRelations := make([]model.VideoCameraRelation, 0, len(relations))
	deviceID := ""
	for _, relation := range relations {
		if relation.RelationType != "device" || strings.TrimSpace(relation.TargetID) == "" {
			continue
		}
		if deviceID != "" && deviceID != relation.TargetID {
			return errors.New("a camera can be associated with at most one device")
		}
		deviceID = relation.TargetID
		relation.TenantID, relation.CameraID = tenant, camera
		copyRelations = append(copyRelations, relation)
	}
	r.videoRelations[key(tenant, camera)] = clone(copyRelations)
	if mapping, ok := r.videoMappings[key(tenant, camera)]; ok {
		mapping.DeviceID = deviceID
		mapping.RelatedDeviceIDs, mapping.RelatedFloorIDs, mapping.RelatedRoomIDs = nil, nil, nil
		r.videoMappings[key(tenant, camera)] = clone(mapping)
	}
	return nil
}
func (r *Repository) ListVideoCameraRelations(_ context.Context, tenant, camera string) ([]model.VideoCameraRelation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return clone(r.videoRelations[key(tenant, camera)]), nil
}
func (r *Repository) ListVideoCameraRelationsByTarget(_ context.Context, tenant, relationType, targetID string) ([]model.VideoCameraRelation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.VideoCameraRelation{}
	for _, relations := range r.videoRelations {
		for _, relation := range relations {
			if relation.TenantID == tenant && relation.RelationType == relationType && relation.TargetID == targetID {
				out = append(out, clone(relation))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CameraID < out[j].CameraID })
	return out, nil
}
func (r *Repository) SaveAIAnalysis(_ context.Context, v model.AIAnalysis) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ai[key(v.TenantID, v.AlarmID)] = v
	return nil
}
func (r *Repository) GetAIAnalysis(_ context.Context, tenant, id string) (model.AIAnalysis, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.ai[key(tenant, id)]
	if !ok {
		return v, ErrNotFound
	}
	return v, nil
}
func (r *Repository) SaveKnowledgeDoc(_ context.Context, v model.KnowledgeDoc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.knowledge[key(v.TenantID, v.ID)] = v
	return nil
}

func uniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
func (r *Repository) ListKnowledgeDocs(_ context.Context, tenant string) ([]model.KnowledgeDoc, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []model.KnowledgeDoc{}
	for _, v := range r.knowledge {
		if v.TenantID == tenant {
			out = append(out, clone(v))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt == out[j].CreatedAt {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out, nil
}
func (r *Repository) ListKnowledgeDocsPage(ctx context.Context, tenant string, limit, offset int) ([]model.KnowledgeDoc, int, error) {
	items, err := r.ListKnowledgeDocs(ctx, tenant)
	if err != nil {
		return nil, 0, err
	}
	return page(items, offset, limit), len(items), nil
}

func (r *Repository) SaveWorkflowKnowledgeBinding(_ context.Context, v model.WorkflowKnowledgeBinding) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workflowKnowledge[key(v.TenantID, v.WorkflowID)] = clone(v)
	return nil
}

func (r *Repository) GetWorkflowKnowledgeBinding(_ context.Context, tenant, workflowID string) (model.WorkflowKnowledgeBinding, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.workflowKnowledge[key(tenant, workflowID)]
	if !ok {
		return model.WorkflowKnowledgeBinding{}, nil
	}
	return clone(v), nil
}
func (r *Repository) SaveReplay(_ context.Context, v model.ReplayRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replays[v.ID] = v
	return nil
}
func (r *Repository) UpdateReplay(ctx context.Context, v model.ReplayRequest) error {
	return r.SaveReplay(ctx, v)
}
func (r *Repository) GetReplay(_ context.Context, id string) (model.ReplayRequest, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.replays[id]
	if !ok {
		return v, ErrNotFound
	}
	return v, nil
}
func (r *Repository) SaveAudit(_ context.Context, v model.AuditLog) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.audits = append(r.audits, v)
	return nil
}
func (r *Repository) SaveAIToolCall(_ context.Context, v model.AIToolCallLog) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.aiToolCalls = append(r.aiToolCalls, clone(v))
	return nil
}
func (r *Repository) LoadAIProviderConfig(_ context.Context) (ports.AIPluginConfig, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.aiProviderConfig == nil {
		return ports.AIPluginConfig{}, false, nil
	}
	return *r.aiProviderConfig, true, nil
}
func (r *Repository) SaveAIProviderConfig(_ context.Context, v ports.AIPluginConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := v
	r.aiProviderConfig = &copy
	return nil
}
func (r *Repository) Health(context.Context) error { return nil }
func (r *Repository) Close() error                 { return nil }

func cloneAnyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, value := range src {
		switch typed := value.(type) {
		case map[string]any:
			dst[k] = cloneAnyMap(typed)
		case []any:
			items := make([]any, len(typed))
			for i, item := range typed {
				if nested, ok := item.(map[string]any); ok {
					items[i] = cloneAnyMap(nested)
				} else {
					items[i] = item
				}
			}
			dst[k] = items
		case model.VideoAlarmEvent:
			dst[k] = cloneVideoEvent(typed)
		default:
			dst[k] = value
		}
	}
	return dst
}

func cloneVideoEvent(v model.VideoAlarmEvent) model.VideoAlarmEvent {
	v.Raw = cloneAnyMap(v.Raw)
	return v
}

func cloneAlarm(v model.Alarm) model.Alarm {
	v.Details = cloneAnyMap(v.Details)
	return v
}

func clone[T any](v T) T { b, _ := json.Marshal(v); var out T; _ = json.Unmarshal(b, &out); return out }
func cloneManaged(v model.ManagedDevice) model.ManagedDevice {
	if v.Tags != nil {
		v.Tags = clone(v.Tags)
	}
	return v
}

var _ = fmt.Sprintf

func (r *Repository) MarkRawParseResult(_ context.Context, tenant, id string, at int64, message string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(tenant, id)
	v, ok := r.raw[k]
	if !ok {
		return nil
	}
	v.ParseAttemptedAt = at
	v.ParseError = message
	r.raw[k] = v
	return nil
}

// UpdateDeviceAccessStatus never writes back a stale configuration snapshot.
func (r *Repository) UpdateDeviceAccessStatus(_ context.Context, expected model.DeviceAccessProfile, status, message string, at int64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(expected.TenantID, expected.ID)
	current, ok := r.accessProfiles[k]
	if !ok || current.Configuration() != expected.Configuration() {
		return false, nil
	}
	current.RuntimeStatus, current.LastError = status, message
	if status == "ONLINE" {
		current.LastSuccessAt = at
	} else {
		current.LastErrorAt = at
	}
	r.accessProfiles[k] = current
	return true, nil
}
