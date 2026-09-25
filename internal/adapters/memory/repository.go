package memory /* 声明 memory 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"sort"          /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"sync"          /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

var ErrNotFound = model.ErrNotFound /* 声明 ErrNotFound。 */

type Repository struct { /* 定义 Repository 类型。 */
	accessStates        map[string][]byte                         /* 执行当前语句并推进处理流程。 */
	componentAlarms     map[string]model.ComponentAlarmState      /* 执行当前语句并推进处理流程。 */
	rawReservations     map[string]rawReservation                 /* 执行当前语句并推进处理流程。 */
	leases              map[string]model.ExecutionLease           /* 执行当前语句并推进处理流程。 */
	revocations         map[string]model.CredentialRevocation     /* 执行当前语句并推进处理流程。 */
	commands            map[string]model.DeviceCommand            /* 执行当前语句并推进处理流程。 */
	mu                  sync.RWMutex                              /* 执行当前语句并推进处理流程。 */
	raw                 map[string]model.RawArchiveIndex          /* 执行当前语句并推进处理流程。 */
	rawMessages         map[string]model.RawMessage               /* 执行当前语句并推进处理流程。 */
	standard            map[string]model.StandardMessage          /* 执行当前语句并推进处理流程。 */
	standardProcessed   map[string]bool                           /* 执行当前语句并推进处理流程。 */
	rulePending         map[string]int64                          /* 执行当前语句并推进处理流程。 */
	states              map[string]model.DeviceState              /* 执行当前语句并推进处理流程。 */
	stateEvents         []model.DeviceStateEvent                  /* 执行当前语句并推进处理流程。 */
	rules               map[string]model.AlarmRule                /* 执行当前语句并推进处理流程。 */
	alarms              map[string]model.Alarm                    /* 执行当前语句并推进处理流程。 */
	video               map[string]model.VideoAlarmEvent          /* 执行当前语句并推进处理流程。 */
	videoMappings       map[string]model.VideoCameraMapping       /* 执行当前语句并推进处理流程。 */
	videoRelations      map[string][]model.VideoCameraRelation    /* 执行当前语句并推进处理流程。 */
	ai                  map[string]model.AIAnalysis               /* 执行当前语句并推进处理流程。 */
	knowledge           map[string]model.KnowledgeDoc             /* 执行当前语句并推进处理流程。 */
	workflowKnowledge   map[string]model.WorkflowKnowledgeBinding /* 执行当前语句并推进处理流程。 */
	replays             map[string]model.ReplayRequest            /* 执行当前语句并推进处理流程。 */
	audits              []model.AuditLog                          /* 执行当前语句并推进处理流程。 */
	aiToolCalls         []model.AIToolCallLog                     /* 执行当前语句并推进处理流程。 */
	aiProviderConfig    *ports.AIPluginConfig                     /* 执行当前语句并推进处理流程。 */
	products            map[string]model.Product                  /* 执行当前语句并推进处理流程。 */
	protocols           map[string]model.ProtocolPackage          /* 执行当前语句并推进处理流程。 */
	protocolDefinitions map[string]model.ProtocolDefinition       /* 执行当前语句并推进处理流程。 */
	protocolReleases    map[string]model.ProtocolRelease          /* 执行当前语句并推进处理流程。 */
	pointTables         map[string]model.PointTableRelease        /* 执行当前语句并推进处理流程。 */
	protocolBindings    map[string]model.ProductProtocolBinding   /* 执行当前语句并推进处理流程。 */
	accessProfiles      map[string]model.DeviceAccessProfile      /* 执行当前语句并推进处理流程。 */
	devices             map[string]model.ManagedDevice            /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func NewRepository() *Repository { /* 定义 NewRepository 函数。 */
	return &Repository{raw: map[string]model.RawArchiveIndex{}, rawMessages: map[string]model.RawMessage{}, standard: map[string]model.StandardMessage{}, standardProcessed: map[string]bool{}, rulePending: map[string]int64{}, states: map[string]model.DeviceState{}, rules: map[string]model.AlarmRule{}, alarms: map[string]model.Alarm{}, video: map[string]model.VideoAlarmEvent{}, videoMappings: map[string]model.VideoCameraMapping{}, videoRelations: map[string][]model.VideoCameraRelation{}, ai: map[string]model.AIAnalysis{}, knowledge: map[string]model.KnowledgeDoc{}, workflowKnowledge: map[string]model.WorkflowKnowledgeBinding{}, replays: map[string]model.ReplayRequest{}, products: map[string]model.Product{}, protocols: map[string]model.ProtocolPackage{}, protocolDefinitions: map[string]model.ProtocolDefinition{}, protocolReleases: map[string]model.ProtocolRelease{}, pointTables: map[string]model.PointTableRelease{}, protocolBindings: map[string]model.ProductProtocolBinding{}, accessProfiles: map[string]model.DeviceAccessProfile{}, devices: map[string]model.ManagedDevice{}} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func key(parts ...string) string { return strings.Join(parts, "\x00") } /* 定义 key 函数。 */

func (r *Repository) SaveProduct(_ context.Context, v model.Product) error { /* 定义 SaveProduct 函数。 */
	r.mu.Lock()                                  /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                          /* 安排函数结束时执行清理。 */
	r.products[key(v.TenantID, v.ID)] = clone(v) /* 执行当前语句并推进处理流程。 */
	return nil                                   /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetProduct(_ context.Context, tenant, id string) (model.Product, error) { /* 定义 GetProduct 函数。 */
	r.mu.RLock()                         /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                 /* 安排函数结束时执行清理。 */
	v, ok := r.products[key(tenant, id)] /* 更新 ok 的值。 */
	if !ok {                             /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return clone(v), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListProducts(_ context.Context, tenant string) ([]model.Product, error) { /* 定义 ListProducts 函数。 */
	r.mu.RLock()                   /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()           /* 安排函数结束时执行清理。 */
	out := []model.Product{}       /* 更新 out 的值。 */
	for _, v := range r.products { /* 循环处理当前数据。 */
		if tenant == "" || v.TenantID == tenant { /* 判断条件并选择处理分支。 */
			out = append(out, clone(v)) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt }) /* 执行当前语句并推进处理流程。 */
	return out, nil                                                                     /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListProductsPage(ctx context.Context, tenant string, limit, offset int) ([]model.Product, int, error) { /* 定义 ListProductsPage 函数。 */
	items, err := r.ListProducts(ctx, tenant) /* 更新 err 的值。 */
	if err != nil {                           /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return page(items, offset, limit), len(items), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveProtocolPackage(_ context.Context, v model.ProtocolPackage) error { /* 定义 SaveProtocolPackage 函数。 */
	r.mu.Lock()                                   /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                           /* 安排函数结束时执行清理。 */
	r.protocols[key(v.TenantID, v.ID)] = clone(v) /* 执行当前语句并推进处理流程。 */
	return nil                                    /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetProtocolPackage(_ context.Context, tenant, id string) (model.ProtocolPackage, error) { /* 定义 GetProtocolPackage 函数。 */
	r.mu.RLock()                          /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                  /* 安排函数结束时执行清理。 */
	v, ok := r.protocols[key(tenant, id)] /* 更新 ok 的值。 */
	if !ok {                              /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return clone(v), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListProtocolPackages(_ context.Context, tenant string) ([]model.ProtocolPackage, error) { /* 定义 ListProtocolPackages 函数。 */
	r.mu.RLock()                     /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()             /* 安排函数结束时执行清理。 */
	out := []model.ProtocolPackage{} /* 更新 out 的值。 */
	for _, v := range r.protocols {  /* 循环处理当前数据。 */
		if tenant == "" || v.TenantID == tenant { /* 判断条件并选择处理分支。 */
			out = append(out, clone(v)) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt }) /* 执行当前语句并推进处理流程。 */
	return out, nil                                                                     /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListProtocolPackagesPage(ctx context.Context, tenant string, limit, offset int) ([]model.ProtocolPackage, int, error) { /* 定义 ListProtocolPackagesPage 函数。 */
	items, err := r.ListProtocolPackages(ctx, tenant) /* 更新 err 的值。 */
	if err != nil {                                   /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return page(items, offset, limit), len(items), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) SaveProtocolDefinition(_ context.Context, v model.ProtocolDefinition) error { /* 定义 SaveProtocolDefinition 函数。 */
	r.mu.Lock()                                             /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                                     /* 安排函数结束时执行清理。 */
	r.protocolDefinitions[key(v.TenantID, v.ID)] = clone(v) /* 执行当前语句并推进处理流程。 */
	return nil                                              /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetProtocolDefinition(_ context.Context, tenant, id string) (model.ProtocolDefinition, error) { /* 定义 GetProtocolDefinition 函数。 */
	r.mu.RLock()                                    /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                            /* 安排函数结束时执行清理。 */
	v, ok := r.protocolDefinitions[key(tenant, id)] /* 更新 ok 的值。 */
	if !ok {                                        /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return clone(v), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListProtocolDefinitions(_ context.Context, tenant string) ([]model.ProtocolDefinition, error) { /* 定义 ListProtocolDefinitions 函数。 */
	r.mu.RLock()                              /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                      /* 安排函数结束时执行清理。 */
	out := []model.ProtocolDefinition{}       /* 更新 out 的值。 */
	for _, v := range r.protocolDefinitions { /* 循环处理当前数据。 */
		if tenant == "" || v.TenantID == tenant { /* 判断条件并选择处理分支。 */
			out = append(out, clone(v)) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt }) /* 执行当前语句并推进处理流程。 */
	return out, nil                                                                     /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) CreateProtocolRelease(_ context.Context, v model.ProtocolRelease) error { /* 定义 CreateProtocolRelease 函数。 */
	r.mu.Lock()                                     /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                             /* 安排函数结束时执行清理。 */
	k := key(v.TenantID, v.ProtocolID, v.Version)   /* 更新 k 的值。 */
	if _, exists := r.protocolReleases[k]; exists { /* 判断条件并选择处理分支。 */
		return errors.New("protocol release already exists") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.protocolReleases[k] = clone(v) /* 更新 r.protocolReleases[k] 的值。 */
	return nil                       /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetProtocolRelease(_ context.Context, tenant, protocolID, version string) (model.ProtocolRelease, error) { /* 定义 GetProtocolRelease 函数。 */
	r.mu.RLock()                                                  /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                                          /* 安排函数结束时执行清理。 */
	v, ok := r.protocolReleases[key(tenant, protocolID, version)] /* 更新 ok 的值。 */
	if !ok {                                                      /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return clone(v), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListProtocolReleases(_ context.Context, tenant, protocolID string) ([]model.ProtocolRelease, error) { /* 定义 ListProtocolReleases 函数。 */
	r.mu.RLock()                           /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                   /* 安排函数结束时执行清理。 */
	out := []model.ProtocolRelease{}       /* 更新 out 的值。 */
	for _, v := range r.protocolReleases { /* 循环处理当前数据。 */
		if (tenant == "" || v.TenantID == tenant) && (protocolID == "" || v.ProtocolID == protocolID) { /* 判断条件并选择处理分支。 */
			out = append(out, clone(v)) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(out, func(i, j int) bool { /* 执行当前语句并推进处理流程。 */
		if out[i].ProtocolID == out[j].ProtocolID { /* 判断条件并选择处理分支。 */
			return out[i].CreatedAt > out[j].CreatedAt /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return out[i].ProtocolID < out[j].ProtocolID /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) UpdateProtocolReleaseStatus(_ context.Context, tenant, protocolID, version, status string, publishedAt int64) error { /* 定义 UpdateProtocolReleaseStatus 函数。 */
	r.mu.Lock()                           /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                   /* 安排函数结束时执行清理。 */
	k := key(tenant, protocolID, version) /* 更新 k 的值。 */
	v, ok := r.protocolReleases[k]        /* 更新 ok 的值。 */
	if !ok {                              /* 判断条件并选择处理分支。 */
		return ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v.Status, v.PublishedAt = status, publishedAt /* 更新 v.PublishedAt 的值。 */
	r.protocolReleases[k] = clone(v)              /* 更新 r.protocolReleases[k] 的值。 */
	return nil                                    /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) CreatePointTableRelease(_ context.Context, v model.PointTableRelease) error { /* 定义 CreatePointTableRelease 函数。 */
	r.mu.Lock()                                   /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                           /* 安排函数结束时执行清理。 */
	k := key(v.TenantID, v.ProtocolID, v.Version) /* 更新 k 的值。 */
	if _, exists := r.pointTables[k]; exists {    /* 判断条件并选择处理分支。 */
		return errors.New("point table release already exists") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.pointTables[k] = clone(v) /* 更新 r.pointTables[k] 的值。 */
	return nil                  /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetPointTableRelease(_ context.Context, tenant, protocolID, version string) (model.PointTableRelease, error) { /* 定义 GetPointTableRelease 函数。 */
	r.mu.RLock()                                             /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                                     /* 安排函数结束时执行清理。 */
	v, ok := r.pointTables[key(tenant, protocolID, version)] /* 更新 ok 的值。 */
	if !ok {                                                 /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return clone(v), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveProductProtocolBinding(_ context.Context, v model.ProductProtocolBinding) error { /* 定义 SaveProductProtocolBinding 函数。 */
	r.mu.Lock()                                                 /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                                         /* 安排函数结束时执行清理。 */
	r.protocolBindings[key(v.TenantID, v.ProductID)] = clone(v) /* 执行当前语句并推进处理流程。 */
	return nil                                                  /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetProductProtocolBinding(_ context.Context, tenant, productID string) (model.ProductProtocolBinding, error) { /* 定义 GetProductProtocolBinding 函数。 */
	r.mu.RLock()                                        /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                                /* 安排函数结束时执行清理。 */
	v, ok := r.protocolBindings[key(tenant, productID)] /* 更新 ok 的值。 */
	if !ok {                                            /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return clone(v), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveDeviceAccessProfile(_ context.Context, v model.DeviceAccessProfile) error { /* 定义 SaveDeviceAccessProfile 函数。 */
	r.mu.Lock()                                        /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                                /* 安排函数结束时执行清理。 */
	r.accessProfiles[key(v.TenantID, v.ID)] = clone(v) /* 执行当前语句并推进处理流程。 */
	return nil                                         /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetDeviceAccessProfile(_ context.Context, tenant, id string) (model.DeviceAccessProfile, error) { /* 定义 GetDeviceAccessProfile 函数。 */
	r.mu.RLock()                               /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                       /* 安排函数结束时执行清理。 */
	v, ok := r.accessProfiles[key(tenant, id)] /* 更新 ok 的值。 */
	if !ok {                                   /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return clone(v), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListDeviceAccessProfiles(_ context.Context, tenant string) ([]model.DeviceAccessProfile, error) { /* 定义 ListDeviceAccessProfiles 函数。 */
	r.mu.RLock()                         /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                 /* 安排函数结束时执行清理。 */
	out := []model.DeviceAccessProfile{} /* 更新 out 的值。 */
	for _, v := range r.accessProfiles { /* 循环处理当前数据。 */
		if tenant == "" || v.TenantID == tenant { /* 判断条件并选择处理分支。 */
			out = append(out, clone(v)) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt }) /* 执行当前语句并推进处理流程。 */
	return out, nil                                                                     /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveManagedDevice(_ context.Context, v model.ManagedDevice) error { /* 定义 SaveManagedDevice 函数。 */
	r.mu.Lock()                         /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                 /* 安排函数结束时执行清理。 */
	for _, current := range r.devices { /* 循环处理当前数据。 */
		if current.AccessKey == v.AccessKey && (current.TenantID != v.TenantID || current.ID != v.ID) { /* 判断条件并选择处理分支。 */
			return fmt.Errorf("access key already exists") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	r.devices[key(v.TenantID, v.ID)] = cloneManaged(v) /* 执行当前语句并推进处理流程。 */
	return nil                                         /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetManagedDevice(_ context.Context, tenant, id string) (model.ManagedDevice, error) { /* 定义 GetManagedDevice 函数。 */
	r.mu.RLock()                        /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                /* 安排函数结束时执行清理。 */
	v, ok := r.devices[key(tenant, id)] /* 更新 ok 的值。 */
	if !ok {                            /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return cloneManaged(v), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetManagedDeviceByAccessKey(_ context.Context, accessKey string) (model.ManagedDevice, error) { /* 定义 GetManagedDeviceByAccessKey 函数。 */
	r.mu.RLock()                  /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()          /* 安排函数结束时执行清理。 */
	for _, v := range r.devices { /* 循环处理当前数据。 */
		if v.AccessKey == accessKey { /* 判断条件并选择处理分支。 */
			return cloneManaged(v), nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return model.ManagedDevice{}, ErrNotFound /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListManagedDevices(_ context.Context, tenant string) ([]model.ManagedDevice, error) { /* 定义 ListManagedDevices 函数。 */
	r.mu.RLock()                   /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()           /* 安排函数结束时执行清理。 */
	out := []model.ManagedDevice{} /* 更新 out 的值。 */
	for _, v := range r.devices {  /* 循环处理当前数据。 */
		if tenant == "" || v.TenantID == tenant { /* 判断条件并选择处理分支。 */
			out = append(out, cloneManaged(v)) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt }) /* 执行当前语句并推进处理流程。 */
	return out, nil                                                                     /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListManagedDevicesPage(ctx context.Context, tenant string, limit, offset int) ([]model.ManagedDevice, int, error) { /* 定义 ListManagedDevicesPage 函数。 */
	items, err := r.ListManagedDevices(ctx, tenant) /* 更新 err 的值。 */
	if err != nil {                                 /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return page(items, offset, limit), len(items), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) CountManagedDeviceChildren(_ context.Context, tenant string, ids []string) (map[string]int, error) { /* 定义 CountManagedDeviceChildren 函数。 */
	counts := make(map[string]int, len(ids)) /* 更新 counts 的值。 */
	if len(ids) == 0 {                       /* 判断条件并选择处理分支。 */
		return counts, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	wanted := make(map[string]struct{}, len(ids)) /* 更新 wanted 的值。 */
	for _, id := range ids {                      /* 循环处理当前数据。 */
		if id != "" { /* 判断条件并选择处理分支。 */
			wanted[id] = struct{}{} /* 更新 wanted[id] 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	r.mu.RLock()                       /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()               /* 安排函数结束时执行清理。 */
	for _, device := range r.devices { /* 循环处理当前数据。 */
		if device.TenantID == tenant && device.GatewayID != "" { /* 判断条件并选择处理分支。 */
			if _, ok := wanted[device.GatewayID]; ok { /* 判断条件并选择处理分支。 */
				counts[device.GatewayID]++ /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return counts, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) SaveRawIndex(_ context.Context, v model.RawArchiveIndex) (bool, error) { /* 定义 SaveRawIndex 函数。 */
	r.mu.Lock()                                           /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                                   /* 安排函数结束时执行清理。 */
	if _, ok := r.raw[key(v.TenantID, v.MessageID)]; ok { /* 判断条件并选择处理分支。 */
		return false, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.raw[key(v.TenantID, v.MessageID)] = v /* 执行当前语句并推进处理流程。 */
	return true, nil                        /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) SaveRawMessage(_ context.Context, v model.RawMessage) error { /* 定义 SaveRawMessage 函数。 */
	r.mu.Lock()                                    /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                            /* 安排函数结束时执行清理。 */
	key := key(v.TenantID, v.MessageID)            /* 更新 key 的值。 */
	if old, exists := r.rawMessages[key]; exists { /* 判断条件并选择处理分支。 */
		if old.PayloadHash() != v.PayloadHash() || old.DeviceID != v.DeviceID || old.ProductID != v.ProductID { /* 判断条件并选择处理分支。 */
			return model.ErrRawConflict /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} else { /* 结束当前表达式或代码块。 */
		r.rawMessages[key] = clone(v) /* 更新 r.rawMessages[key] 的值。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) GetRawMessage(_ context.Context, tenant, messageID string) (model.RawMessage, error) { /* 定义 GetRawMessage 函数。 */
	r.mu.RLock()                                   /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                           /* 安排函数结束时执行清理。 */
	v, ok := r.rawMessages[key(tenant, messageID)] /* 更新 ok 的值。 */
	if !ok {                                       /* 判断条件并选择处理分支。 */
		return model.RawMessage{}, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return clone(v), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) MarkRawPublished(_ context.Context, tenant, messageID string, publishedAt int64, lastError string) error { /* 定义 MarkRawPublished 函数。 */
	r.mu.Lock()                 /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()         /* 安排函数结束时执行清理。 */
	k := key(tenant, messageID) /* 更新 k 的值。 */
	v, ok := r.raw[k]           /* 更新 ok 的值。 */
	if !ok {                    /* 判断条件并选择处理分支。 */
		return ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v.PublishAttempts++            /* 执行当前语句并推进处理流程。 */
	v.LastPublishError = lastError /* 更新 v.LastPublishError 的值。 */
	if lastError == "" {           /* 判断条件并选择处理分支。 */
		v.PublishedAt = publishedAt /* 更新 v.PublishedAt 的值。 */
	} /* 结束当前表达式或代码块。 */
	r.raw[k] = v /* 更新 r.raw[k] 的值。 */
	return nil   /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListPendingRawIndexes(_ context.Context, limit int) ([]model.RawArchiveIndex, error) { /* 定义 ListPendingRawIndexes 函数。 */
	r.mu.RLock()                     /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()             /* 安排函数结束时执行清理。 */
	out := []model.RawArchiveIndex{} /* 更新 out 的值。 */
	for _, v := range r.raw {        /* 循环处理当前数据。 */
		if v.PublishedAt == 0 { /* 判断条件并选择处理分支。 */
			out = append(out, v) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(out, func(i, j int) bool { return out[i].ArchivedAt < out[j].ArchivedAt }) /* 执行当前语句并推进处理流程。 */
	return page(out, 0, limit), nil                                                       /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetRawIndex(_ context.Context, tenant, messageID string) (model.RawArchiveIndex, error) { /* 定义 GetRawIndex 函数。 */
	r.mu.RLock()                           /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                   /* 安排函数结束时执行清理。 */
	v, ok := r.raw[key(tenant, messageID)] /* 更新 ok 的值。 */
	if !ok {                               /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return v, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListRawIndexes(_ context.Context, f ports.RawFilter) ([]model.RawArchiveIndex, error) { /* 定义 ListRawIndexes 函数。 */
	r.mu.RLock()                            /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                    /* 安排函数结束时执行清理。 */
	out := make([]model.RawArchiveIndex, 0) /* 更新 out 的值。 */
	for _, v := range r.raw {               /* 循环处理当前数据。 */
		if f.TenantID != "" && v.TenantID != f.TenantID || f.ProductID != "" && v.ProductID != f.ProductID || f.DeviceID != "" && v.DeviceID != f.DeviceID || f.Start > 0 && v.ReceivedAt < f.Start || f.End > 0 && v.ReceivedAt > f.End { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, v) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(out, func(i, j int) bool { return out[i].ReceivedAt > out[j].ReceivedAt }) /* 执行当前语句并推进处理流程。 */
	return page(out, f.Offset, f.Limit), nil                                              /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) CountRawIndexes(_ context.Context, f ports.RawFilter) (int, error) { /* 定义 CountRawIndexes 函数。 */
	r.mu.RLock()              /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()      /* 安排函数结束时执行清理。 */
	count := 0                /* 更新 count 的值。 */
	for _, v := range r.raw { /* 循环处理当前数据。 */
		if f.TenantID != "" && v.TenantID != f.TenantID || f.ProductID != "" && v.ProductID != f.ProductID || f.DeviceID != "" && v.DeviceID != f.DeviceID || f.Start > 0 && v.ReceivedAt < f.Start || f.End > 0 && v.ReceivedAt > f.End { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		count++ /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return count, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func page[T any](v []T, offset, limit int) []T { /* 定义 page 函数。 */
	if offset < 0 { /* 判断条件并选择处理分支。 */
		offset = 0 /* 更新 offset 的值。 */
	} /* 结束当前表达式或代码块。 */
	if offset >= len(v) { /* 判断条件并选择处理分支。 */
		return []T{} /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if limit <= 0 { /* 判断条件并选择处理分支。 */
		limit = 100 /* 更新 limit 的值。 */
	} /* 结束当前表达式或代码块。 */
	end := offset + limit /* 更新 end 的值。 */
	if end > len(v) {     /* 判断条件并选择处理分支。 */
		end = len(v) /* 更新 end 的值。 */
	} /* 结束当前表达式或代码块。 */
	return v[offset:end] /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveStandardMessage(ctx context.Context, v model.StandardMessage) error { /* 定义 SaveStandardMessage 函数。 */
	_, err := r.SaveStandardMessageIfAbsent(ctx, v) /* 更新 err 的值。 */
	return err                                      /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveStandardMessageIfAbsent(_ context.Context, v model.StandardMessage) (bool, error) { /* 定义 SaveStandardMessageIfAbsent 函数。 */
	r.mu.Lock()                             /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                     /* 安排函数结束时执行清理。 */
	k := key(v.TenantID, v.MessageID)       /* 更新 k 的值。 */
	if _, exists := r.standard[k]; exists { /* 判断条件并选择处理分支。 */
		return false, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.standard[k] = v              /* 更新 r.standard[k] 的值。 */
	r.standardProcessed[k] = false /* 更新 r.standardProcessed[k] 的值。 */
	return true, nil               /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ClaimStandardMessage(_ context.Context, v model.StandardMessage) (bool, bool, error) { /* 定义 ClaimStandardMessage 函数。 */
	r.mu.Lock()                              /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                      /* 安排函数结束时执行清理。 */
	k := key(v.TenantID, v.MessageID)        /* 更新 k 的值。 */
	if _, exists := r.standard[k]; !exists { /* 判断条件并选择处理分支。 */
		r.standard[k] = v              /* 更新 r.standard[k] 的值。 */
		r.standardProcessed[k] = false /* 更新 r.standardProcessed[k] 的值。 */
		return true, true, nil         /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return !r.standardProcessed[k], false, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) MarkStandardMessageProcessed(_ context.Context, tenant, messageID string) error { /* 定义 MarkStandardMessageProcessed 函数。 */
	r.mu.Lock()                              /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                      /* 安排函数结束时执行清理。 */
	k := key(tenant, messageID)              /* 更新 k 的值。 */
	if _, exists := r.standard[k]; !exists { /* 判断条件并选择处理分支。 */
		return ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.standardProcessed[k] = true /* 更新 r.standardProcessed[k] 的值。 */
	return nil                    /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetStandardMessageByRaw(_ context.Context, tenant, rawID string) (model.StandardMessage, error) { /* 定义 GetStandardMessageByRaw 函数。 */
	r.mu.RLock()                   /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()           /* 安排函数结束时执行清理。 */
	for _, v := range r.standard { /* 循环处理当前数据。 */
		if v.TenantID == tenant && v.RawMessageID == rawID { /* 判断条件并选择处理分支。 */
			return clone(v), nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return model.StandardMessage{}, ErrNotFound /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetLatestMessage(_ context.Context, tenant, device string) (model.StandardMessage, error) { /* 定义 GetLatestMessage 函数。 */
	r.mu.RLock()                   /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()           /* 安排函数结束时执行清理。 */
	var out model.StandardMessage  /* 声明 out。 */
	found := false                 /* 更新 found 的值。 */
	for _, v := range r.standard { /* 循环处理当前数据。 */
		if v.TenantID == tenant && v.DeviceID == device && (!found || v.Timestamp > out.Timestamp) { /* 判断条件并选择处理分支。 */
			out, found = clone(v), true /* 更新 found 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if !found { /* 判断条件并选择处理分支。 */
		return out, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) PropertyHistory(_ context.Context, tenant, device, property string, start, end int64, limit int) ([]map[string]any, error) { /* 定义 PropertyHistory 函数。 */
	r.mu.RLock()                   /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()           /* 安排函数结束时执行清理。 */
	out := []map[string]any{}      /* 更新 out 的值。 */
	for _, v := range r.standard { /* 循环处理当前数据。 */
		if v.TenantID != tenant || v.DeviceID != device || v.Timestamp < start || (end > 0 && v.Timestamp > end) { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		value, ok := v.Properties[property] /* 更新 ok 的值。 */
		if !ok {                            /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, map[string]any{"timestamp": v.Timestamp, "value": value, "messageId": v.MessageID}) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(out, func(i, j int) bool { return out[i]["timestamp"].(int64) < out[j]["timestamp"].(int64) }) /* 执行当前语句并推进处理流程。 */
	if limit > 0 && len(out) > limit {                                                                        /* 判断条件并选择处理分支。 */
		out = out[len(out)-limit:] /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) PropertyHistoryPage(ctx context.Context, tenant, device, property string, start, end int64, limit, offset int) ([]map[string]any, int, error) { /* 定义 PropertyHistoryPage 函数。 */
	items, err := r.PropertyHistory(ctx, tenant, device, property, start, end, 0) /* 更新 err 的值。 */
	if err != nil {                                                               /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	total := len(items) /* 更新 total 的值。 */
	if limit <= 0 {     /* 判断条件并选择处理分支。 */
		limit = 20 /* 更新 limit 的值。 */
	} /* 结束当前表达式或代码块。 */
	if offset < 0 { /* 判断条件并选择处理分支。 */
		offset = 0 /* 更新 offset 的值。 */
	} /* 结束当前表达式或代码块。 */
	endIndex := total - offset /* 更新 endIndex 的值。 */
	if endIndex <= 0 {         /* 判断条件并选择处理分支。 */
		return []map[string]any{}, total, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	startIndex := endIndex - limit /* 更新 startIndex 的值。 */
	if startIndex < 0 {            /* 判断条件并选择处理分支。 */
		startIndex = 0 /* 更新 startIndex 的值。 */
	} /* 结束当前表达式或代码块。 */
	return items[startIndex:endIndex], total, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) UpsertDeviceState(_ context.Context, v model.DeviceState) error { /* 定义 UpsertDeviceState 函数。 */
	r.mu.Lock()                               /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                       /* 安排函数结束时执行清理。 */
	r.states[key(v.TenantID, v.DeviceID)] = v /* 执行当前语句并推进处理流程。 */
	return nil                                /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetDeviceState(_ context.Context, tenant, device string) (model.DeviceState, error) { /* 定义 GetDeviceState 函数。 */
	r.mu.RLock()                           /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                   /* 安排函数结束时执行清理。 */
	v, ok := r.states[key(tenant, device)] /* 更新 ok 的值。 */
	if !ok {                               /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return v, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListDeviceStates(_ context.Context, tenant string) ([]model.DeviceState, error) { /* 定义 ListDeviceStates 函数。 */
	r.mu.RLock()                 /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()         /* 安排函数结束时执行清理。 */
	out := []model.DeviceState{} /* 更新 out 的值。 */
	for _, v := range r.states { /* 循环处理当前数据。 */
		if tenant == "" || v.TenantID == tenant { /* 判断条件并选择处理分支。 */
			out = append(out, v) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListDeviceStatesPage(ctx context.Context, tenant string, limit, offset int) ([]model.DeviceState, int, error) { /* 定义 ListDeviceStatesPage 函数。 */
	items, err := r.ListDeviceStates(ctx, tenant) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(items, func(i, j int) bool { return items[i].LastSeenAt > items[j].LastSeenAt }) /* 执行当前语句并推进处理流程。 */
	return page(items, offset, limit), len(items), nil                                          /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListUnregisteredDeviceStatesPage(ctx context.Context, tenant string, limit, offset int) ([]model.DeviceState, int, error) { /* 定义 ListUnregisteredDeviceStatesPage 函数。 */
	items, err := r.ListDeviceStates(ctx, tenant) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.mu.RLock()                            /* 执行当前语句并推进处理流程。 */
	registered := make(map[string]struct{}) /* 更新 registered 的值。 */
	for _, device := range r.devices {      /* 循环处理当前数据。 */
		if device.TenantID == tenant { /* 判断条件并选择处理分支。 */
			registered[device.ID] = struct{}{} /* 更新 registered[device.ID] 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	r.mu.RUnlock()                                           /* 执行当前语句并推进处理流程。 */
	unregistered := make([]model.DeviceState, 0, len(items)) /* 更新 unregistered 的值。 */
	for _, item := range items {                             /* 循环处理当前数据。 */
		if _, ok := registered[item.DeviceID]; !ok { /* 判断条件并选择处理分支。 */
			unregistered = append(unregistered, item) /* 更新 unregistered 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(unregistered, func(i, j int) bool { return unregistered[i].LastSeenAt > unregistered[j].LastSeenAt }) /* 执行当前语句并推进处理流程。 */
	return page(unregistered, offset, limit), len(unregistered), nil                                                 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) CountDeviceStates(_ context.Context, tenant string, unregisteredOnly bool) (int, int, error) { /* 定义 CountDeviceStates 函数。 */
	r.mu.RLock()                            /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                    /* 安排函数结束时执行清理。 */
	registered := make(map[string]struct{}) /* 更新 registered 的值。 */
	if unregisteredOnly {                   /* 判断条件并选择处理分支。 */
		for _, device := range r.devices { /* 循环处理当前数据。 */
			if device.TenantID == tenant { /* 判断条件并选择处理分支。 */
				registered[device.ID] = struct{}{} /* 更新 registered[device.ID] 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	total, online := 0, 0            /* 更新 online 的值。 */
	for _, state := range r.states { /* 循环处理当前数据。 */
		if state.TenantID != tenant { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if unregisteredOnly { /* 判断条件并选择处理分支。 */
			if _, ok := registered[state.DeviceID]; ok { /* 判断条件并选择处理分支。 */
				continue /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		total++                                                                  /* 执行当前语句并推进处理流程。 */
		if state.BusinessStatus == "ONLINE" || state.BusinessStatus == "ALARM" { /* 判断条件并选择处理分支。 */
			online++ /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return total, online, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveDeviceStateEvent(_ context.Context, v model.DeviceState) error { /* 定义 SaveDeviceStateEvent 函数。 */
	r.mu.Lock()                                                                                                        /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                                                                                                /* 安排函数结束时执行清理。 */
	r.stateEvents = append(r.stateEvents, model.DeviceStateEvent{State: clone(v), RecordedAt: time.Now().UnixMilli()}) /* 更新 r.stateEvents 的值。 */
	return nil                                                                                                         /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveRule(_ context.Context, v model.AlarmRule) error { /* 定义 SaveRule 函数。 */
	r.mu.Lock()                        /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                /* 安排函数结束时执行清理。 */
	r.rules[key(v.TenantID, v.ID)] = v /* 执行当前语句并推进处理流程。 */
	return nil                         /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListRules(_ context.Context, tenant string) ([]model.AlarmRule, error) { /* 定义 ListRules 函数。 */
	r.mu.RLock()                /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()        /* 安排函数结束时执行清理。 */
	out := []model.AlarmRule{}  /* 更新 out 的值。 */
	for _, v := range r.rules { /* 循环处理当前数据。 */
		if tenant == "" || v.TenantID == tenant { /* 判断条件并选择处理分支。 */
			out = append(out, v) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListRulesPage(ctx context.Context, tenant string, limit, offset int) ([]model.AlarmRule, int, error) { /* 定义 ListRulesPage 函数。 */
	items, err := r.ListRules(ctx, tenant) /* 更新 err 的值。 */
	if err != nil {                        /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return page(items, offset, limit), len(items), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) DeleteRule(_ context.Context, tenant, id string) error { /* 定义 DeleteRule 函数。 */
	r.mu.Lock()                   /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()           /* 安排函数结束时执行清理。 */
	k := key(tenant, id)          /* 更新 k 的值。 */
	if _, ok := r.rules[k]; !ok { /* 判断条件并选择处理分支。 */
		return ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	delete(r.rules, k) /* 执行当前语句并推进处理流程。 */
	return nil         /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveRulePending(_ context.Context, tenant, ruleID, deviceID string, since int64) error { /* 定义 SaveRulePending 函数。 */
	r.mu.Lock()                                                  /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                                          /* 安排函数结束时执行清理。 */
	k := key(tenant, ruleID, deviceID)                           /* 更新 k 的值。 */
	if current, ok := r.rulePending[k]; ok && current <= since { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.rulePending[k] = since /* 更新 r.rulePending[k] 的值。 */
	return nil               /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetRulePending(_ context.Context, tenant, ruleID, deviceID string) (int64, bool, error) { /* 定义 GetRulePending 函数。 */
	r.mu.RLock()                                              /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                                      /* 安排函数结束时执行清理。 */
	since, ok := r.rulePending[key(tenant, ruleID, deviceID)] /* 更新 ok 的值。 */
	return since, ok, nil                                     /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) DeleteRulePending(_ context.Context, tenant, ruleID, deviceID string) error { /* 定义 DeleteRulePending 函数。 */
	r.mu.Lock()                                          /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                                  /* 安排函数结束时执行清理。 */
	delete(r.rulePending, key(tenant, ruleID, deviceID)) /* 执行当前语句并推进处理流程。 */
	return nil                                           /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) DeleteRulePendings(_ context.Context, tenant, ruleID string) error { /* 定义 DeleteRulePendings 函数。 */
	r.mu.Lock()                             /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                     /* 安排函数结束时执行清理。 */
	prefix := key(tenant, ruleID) + "\x00"  /* 更新 prefix 的值。 */
	for pendingKey := range r.rulePending { /* 循环处理当前数据。 */
		if strings.HasPrefix(pendingKey, prefix) { /* 判断条件并选择处理分支。 */
			delete(r.rulePending, pendingKey) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) UpsertAlarm(_ context.Context, v model.Alarm) (model.Alarm, bool, error) { /* 定义 UpsertAlarm 函数。 */
	r.mu.Lock()                  /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()          /* 安排函数结束时执行清理。 */
	for k, a := range r.alarms { /* 循环处理当前数据。 */
		if a.TenantID == v.TenantID && a.DeviceID == v.DeviceID && a.RuleID == v.RuleID && (a.Status == "ACTIVE" || a.Status == "ACKED") { /* 判断条件并选择处理分支。 */
			if v.TriggerID != "" && a.TriggerID == v.TriggerID { /* 判断条件并选择处理分支。 */
				return cloneAlarm(a), false, nil /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			a.LastTriggeredAt = v.LastTriggeredAt /* 更新 a.LastTriggeredAt 的值。 */
			a.TriggerCount++                      /* 执行当前语句并推进处理流程。 */
			if v.Confidence > a.Confidence {      /* 判断条件并选择处理分支。 */
				a.Confidence = v.Confidence /* 更新 a.Confidence 的值。 */
			} /* 结束当前表达式或代码块。 */
			r.alarms[k] = cloneAlarm(a)      /* 更新 r.alarms[k] 的值。 */
			return cloneAlarm(a), false, nil /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	r.alarms[key(v.TenantID, v.ID)] = cloneAlarm(v) /* 执行当前语句并推进处理流程。 */
	return cloneAlarm(v), true, nil                 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetAlarm(_ context.Context, tenant, id string) (model.Alarm, error) { /* 定义 GetAlarm 函数。 */
	r.mu.RLock()                       /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()               /* 安排函数结束时执行清理。 */
	v, ok := r.alarms[key(tenant, id)] /* 更新 ok 的值。 */
	if !ok {                           /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return cloneAlarm(v), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListAlarms(_ context.Context, f ports.AlarmFilter) ([]model.Alarm, error) { /* 定义 ListAlarms 函数。 */
	r.mu.RLock()                 /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()         /* 安排函数结束时执行清理。 */
	out := []model.Alarm{}       /* 更新 out 的值。 */
	for _, v := range r.alarms { /* 循环处理当前数据。 */
		if f.TenantID != "" && v.TenantID != f.TenantID || f.DeviceID != "" && v.DeviceID != f.DeviceID || f.Status != "" && v.Status != f.Status || f.Level != "" && v.AlarmLevel != f.Level || f.Source != "" && v.Source != f.Source || f.Start > 0 && v.LastTriggeredAt < f.Start || f.End > 0 && v.LastTriggeredAt > f.End { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		out = append(out, cloneAlarm(v)) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(out, func(i, j int) bool { return out[i].LastTriggeredAt > out[j].LastTriggeredAt }) /* 执行当前语句并推进处理流程。 */
	return page(out, f.Offset, f.Limit), nil                                                        /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) CountAlarms(_ context.Context, f ports.AlarmFilter) (int, error) { /* 定义 CountAlarms 函数。 */
	r.mu.RLock()                 /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()         /* 安排函数结束时执行清理。 */
	count := 0                   /* 更新 count 的值。 */
	for _, v := range r.alarms { /* 循环处理当前数据。 */
		if f.TenantID != "" && v.TenantID != f.TenantID || f.DeviceID != "" && v.DeviceID != f.DeviceID || f.Status != "" && v.Status != f.Status || f.Level != "" && v.AlarmLevel != f.Level || f.Source != "" && v.Source != f.Source || f.Start > 0 && v.LastTriggeredAt < f.Start || f.End > 0 && v.LastTriggeredAt > f.End { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		count++ /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return count, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) UpdateAlarm(_ context.Context, v model.Alarm) error { /* 定义 UpdateAlarm 函数。 */
	r.mu.Lock()                                        /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                                /* 安排函数结束时执行清理。 */
	if _, ok := r.alarms[key(v.TenantID, v.ID)]; !ok { /* 判断条件并选择处理分支。 */
		return ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.alarms[key(v.TenantID, v.ID)] = cloneAlarm(v) /* 执行当前语句并推进处理流程。 */
	return nil                                      /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveVideoEvent(_ context.Context, v model.VideoAlarmEvent) (bool, error) { /* 定义 SaveVideoEvent 函数。 */
	r.mu.Lock()                                           /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                                   /* 安排函数结束时执行清理。 */
	if _, ok := r.video[key(v.TenantID, v.EventID)]; ok { /* 判断条件并选择处理分支。 */
		return false, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.video[key(v.TenantID, v.EventID)] = cloneVideoEvent(v) /* 执行当前语句并推进处理流程。 */
	return true, nil                                         /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) UpdateVideoEvent(_ context.Context, v model.VideoAlarmEvent) error { /* 定义 UpdateVideoEvent 函数。 */
	r.mu.Lock()                                              /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                                      /* 安排函数结束时执行清理。 */
	r.video[key(v.TenantID, v.EventID)] = cloneVideoEvent(v) /* 执行当前语句并推进处理流程。 */
	return nil                                               /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListPendingVideoEvents(_ context.Context, limit int) ([]model.VideoAlarmEvent, error) { /* 定义 ListPendingVideoEvents 函数。 */
	r.mu.RLock()                     /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()             /* 安排函数结束时执行清理。 */
	out := []model.VideoAlarmEvent{} /* 更新 out 的值。 */
	for _, v := range r.video {      /* 循环处理当前数据。 */
		status, _ := v.Raw["mediaTransferStatus"].(string) /* 更新 _ 的值。 */
		if status == "PENDING" || status == "FAILED" {     /* 判断条件并选择处理分支。 */
			out = append(out, cloneVideoEvent(v)) /* 更新 out 的值。 */
			if len(out) >= limit {                /* 判断条件并选择处理分支。 */
				break /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveVideoCameraMapping(_ context.Context, v model.VideoCameraMapping) error { /* 定义 SaveVideoCameraMapping 函数。 */
	legacyDeviceIDs := uniqueStrings(v.RelatedDeviceIDs) /* 更新 legacyDeviceIDs 的值。 */
	if v.DeviceID == "" && len(legacyDeviceIDs) == 1 {   /* 判断条件并选择处理分支。 */
		v.DeviceID = legacyDeviceIDs[0] /* 更新 v.DeviceID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if len(legacyDeviceIDs) > 1 || v.DeviceID != "" && len(legacyDeviceIDs) == 1 && legacyDeviceIDs[0] != v.DeviceID { /* 判断条件并选择处理分支。 */
		return errors.New("a camera can be associated with at most one device") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v.RelatedDeviceIDs, v.RelatedFloorIDs, v.RelatedRoomIDs = nil, nil, nil /* 更新 v.RelatedRoomIDs 的值。 */
	r.mu.Lock()                                                             /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                                                     /* 安排函数结束时执行清理。 */
	r.videoMappings[key(v.TenantID, v.CameraID)] = clone(v)                 /* 执行当前语句并推进处理流程。 */
	relations := make([]model.VideoCameraRelation, 0, 1)                    /* 更新 relations 的值。 */
	if v.DeviceID != "" {                                                   /* 判断条件并选择处理分支。 */
		relations = append(relations, model.VideoCameraRelation{TenantID: v.TenantID, CameraID: v.CameraID, RelationType: "device", TargetID: v.DeviceID}) /* 更新 relations 的值。 */
	} /* 结束当前表达式或代码块。 */
	r.videoRelations[key(v.TenantID, v.CameraID)] = clone(relations) /* 执行当前语句并推进处理流程。 */
	return nil                                                       /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetVideoCameraMapping(_ context.Context, tenant, camera string) (model.VideoCameraMapping, error) { /* 定义 GetVideoCameraMapping 函数。 */
	r.mu.RLock()                                  /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                          /* 安排函数结束时执行清理。 */
	v, ok := r.videoMappings[key(tenant, camera)] /* 更新 ok 的值。 */
	if !ok {                                      /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return clone(v), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListVideoCameraMappings(_ context.Context, tenant string) ([]model.VideoCameraMapping, error) { /* 定义 ListVideoCameraMappings 函数。 */
	r.mu.RLock()                        /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                /* 安排函数结束时执行清理。 */
	out := []model.VideoCameraMapping{} /* 更新 out 的值。 */
	for _, v := range r.videoMappings { /* 循环处理当前数据。 */
		if tenant == "" || v.TenantID == tenant { /* 判断条件并选择处理分支。 */
			out = append(out, clone(v)) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(out, func(i, j int) bool { return out[i].CameraID < out[j].CameraID }) /* 执行当前语句并推进处理流程。 */
	return out, nil                                                                   /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListVideoCameraMappingsPage(ctx context.Context, tenant string, limit, offset int) ([]model.VideoCameraMapping, int, error) { /* 定义 ListVideoCameraMappingsPage 函数。 */
	items, err := r.ListVideoCameraMappings(ctx, tenant) /* 更新 err 的值。 */
	if err != nil {                                      /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return page(items, offset, limit), len(items), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ReplaceVideoCameraRelations(_ context.Context, tenant, camera string, relations []model.VideoCameraRelation) error { /* 定义 ReplaceVideoCameraRelations 函数。 */
	r.mu.Lock()                                                           /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                                                   /* 安排函数结束时执行清理。 */
	copyRelations := make([]model.VideoCameraRelation, 0, len(relations)) /* 更新 copyRelations 的值。 */
	deviceID := ""                                                        /* 更新 deviceID 的值。 */
	for _, relation := range relations {                                  /* 循环处理当前数据。 */
		if relation.RelationType != "device" || strings.TrimSpace(relation.TargetID) == "" { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if deviceID != "" && deviceID != relation.TargetID { /* 判断条件并选择处理分支。 */
			return errors.New("a camera can be associated with at most one device") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		deviceID = relation.TargetID                          /* 更新 deviceID 的值。 */
		relation.TenantID, relation.CameraID = tenant, camera /* 更新 relation.CameraID 的值。 */
		copyRelations = append(copyRelations, relation)       /* 更新 copyRelations 的值。 */
	} /* 结束当前表达式或代码块。 */
	r.videoRelations[key(tenant, camera)] = clone(copyRelations) /* 执行当前语句并推进处理流程。 */
	if mapping, ok := r.videoMappings[key(tenant, camera)]; ok { /* 判断条件并选择处理分支。 */
		mapping.DeviceID = deviceID                                                               /* 更新 mapping.DeviceID 的值。 */
		mapping.RelatedDeviceIDs, mapping.RelatedFloorIDs, mapping.RelatedRoomIDs = nil, nil, nil /* 更新 mapping.RelatedRoomIDs 的值。 */
		r.videoMappings[key(tenant, camera)] = clone(mapping)                                     /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListVideoCameraRelations(_ context.Context, tenant, camera string) ([]model.VideoCameraRelation, error) { /* 定义 ListVideoCameraRelations 函数。 */
	r.mu.RLock()                                             /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                                     /* 安排函数结束时执行清理。 */
	return clone(r.videoRelations[key(tenant, camera)]), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListVideoCameraRelationsByTarget(_ context.Context, tenant, relationType, targetID string) ([]model.VideoCameraRelation, error) { /* 定义 ListVideoCameraRelationsByTarget 函数。 */
	r.mu.RLock()                                 /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                         /* 安排函数结束时执行清理。 */
	out := []model.VideoCameraRelation{}         /* 更新 out 的值。 */
	for _, relations := range r.videoRelations { /* 循环处理当前数据。 */
		for _, relation := range relations { /* 循环处理当前数据。 */
			if relation.TenantID == tenant && relation.RelationType == relationType && relation.TargetID == targetID { /* 判断条件并选择处理分支。 */
				out = append(out, clone(relation)) /* 更新 out 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(out, func(i, j int) bool { return out[i].CameraID < out[j].CameraID }) /* 执行当前语句并推进处理流程。 */
	return out, nil                                                                   /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveAIAnalysis(_ context.Context, v model.AIAnalysis) error { /* 定义 SaveAIAnalysis 函数。 */
	r.mu.Lock()                                            /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                                    /* 安排函数结束时执行清理。 */
	r.ai[key(v.TenantID, v.AlarmID, v.KnowledgeScope)] = v /* 每个知识范围单独保存。 */
	return nil                                             /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetAIAnalysis(_ context.Context, tenant, id, knowledgeScope string) (model.AIAnalysis, error) { /* 定义 GetAIAnalysis 函数。 */
	r.mu.RLock()                                   /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                           /* 安排函数结束时执行清理。 */
	v, ok := r.ai[key(tenant, id, knowledgeScope)] /* 更新 ok 的值。 */
	if !ok {                                       /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return v, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveKnowledgeDoc(_ context.Context, v model.KnowledgeDoc) error { /* 定义 SaveKnowledgeDoc 函数。 */
	r.mu.Lock()                            /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                    /* 安排函数结束时执行清理。 */
	r.knowledge[key(v.TenantID, v.ID)] = v /* 执行当前语句并推进处理流程。 */
	return nil                             /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func uniqueStrings(values []string) []string { /* 定义 uniqueStrings 函数。 */
	out := make([]string, 0, len(values)) /* 更新 out 的值。 */
	seen := map[string]struct{}{}         /* 更新 seen 的值。 */
	for _, value := range values {        /* 循环处理当前数据。 */
		value = strings.TrimSpace(value) /* 更新 value 的值。 */
		if value == "" {                 /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if _, ok := seen[value]; ok { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		seen[value] = struct{}{} /* 更新 seen[value] 的值。 */
		out = append(out, value) /* 更新 out 的值。 */
	} /* 结束当前表达式或代码块。 */
	return out /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListKnowledgeDocs(_ context.Context, tenant string) ([]model.KnowledgeDoc, error) { /* 定义 ListKnowledgeDocs 函数。 */
	r.mu.RLock()                    /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()            /* 安排函数结束时执行清理。 */
	out := []model.KnowledgeDoc{}   /* 更新 out 的值。 */
	for _, v := range r.knowledge { /* 循环处理当前数据。 */
		if v.TenantID == tenant { /* 判断条件并选择处理分支。 */
			out = append(out, clone(v)) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(out, func(i, j int) bool { /* 执行当前语句并推进处理流程。 */
		if out[i].CreatedAt == out[j].CreatedAt { /* 判断条件并选择处理分支。 */
			return out[i].ID > out[j].ID /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return out[i].CreatedAt > out[j].CreatedAt /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	return out, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) ListKnowledgeDocsPage(ctx context.Context, tenant string, limit, offset int) ([]model.KnowledgeDoc, int, error) { /* 定义 ListKnowledgeDocsPage 函数。 */
	items, err := r.ListKnowledgeDocs(ctx, tenant) /* 更新 err 的值。 */
	if err != nil {                                /* 判断条件并选择处理分支。 */
		return nil, 0, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return page(items, offset, limit), len(items), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) SaveWorkflowKnowledgeBinding(_ context.Context, v model.WorkflowKnowledgeBinding) error { /* 定义 SaveWorkflowKnowledgeBinding 函数。 */
	r.mu.Lock()                                                   /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                                           /* 安排函数结束时执行清理。 */
	r.workflowKnowledge[key(v.TenantID, v.WorkflowID)] = clone(v) /* 执行当前语句并推进处理流程。 */
	return nil                                                    /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *Repository) GetWorkflowKnowledgeBinding(_ context.Context, tenant, workflowID string) (model.WorkflowKnowledgeBinding, error) { /* 定义 GetWorkflowKnowledgeBinding 函数。 */
	r.mu.RLock()                                          /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()                                  /* 安排函数结束时执行清理。 */
	v, ok := r.workflowKnowledge[key(tenant, workflowID)] /* 更新 ok 的值。 */
	if !ok {                                              /* 判断条件并选择处理分支。 */
		return model.WorkflowKnowledgeBinding{}, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return clone(v), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveReplay(_ context.Context, v model.ReplayRequest) error { /* 定义 SaveReplay 函数。 */
	r.mu.Lock()         /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock() /* 安排函数结束时执行清理。 */
	r.replays[v.ID] = v /* 更新 r.replays[v.ID] 的值。 */
	return nil          /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) UpdateReplay(ctx context.Context, v model.ReplayRequest) error { /* 定义 UpdateReplay 函数。 */
	return r.SaveReplay(ctx, v) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) GetReplay(_ context.Context, id string) (model.ReplayRequest, error) { /* 定义 GetReplay 函数。 */
	r.mu.RLock()           /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()   /* 安排函数结束时执行清理。 */
	v, ok := r.replays[id] /* 更新 ok 的值。 */
	if !ok {               /* 判断条件并选择处理分支。 */
		return v, ErrNotFound /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return v, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveAudit(_ context.Context, v model.AuditLog) error { /* 定义 SaveAudit 函数。 */
	r.mu.Lock()                    /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()            /* 安排函数结束时执行清理。 */
	r.audits = append(r.audits, v) /* 更新 r.audits 的值。 */
	return nil                     /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveAIToolCall(_ context.Context, v model.AIToolCallLog) error { /* 定义 SaveAIToolCall 函数。 */
	r.mu.Lock()                                     /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                             /* 安排函数结束时执行清理。 */
	r.aiToolCalls = append(r.aiToolCalls, clone(v)) /* 更新 r.aiToolCalls 的值。 */
	return nil                                      /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) LoadAIProviderConfig(_ context.Context) (ports.AIPluginConfig, bool, error) { /* 定义 LoadAIProviderConfig 函数。 */
	r.mu.RLock()                   /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock()           /* 安排函数结束时执行清理。 */
	if r.aiProviderConfig == nil { /* 判断条件并选择处理分支。 */
		return ports.AIPluginConfig{}, false, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return *r.aiProviderConfig, true, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *Repository) SaveAIProviderConfig(_ context.Context, v ports.AIPluginConfig) error { /* 定义 SaveAIProviderConfig 函数。 */
	r.mu.Lock()                /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()        /* 安排函数结束时执行清理。 */
	copy := v                  /* 更新 copy 的值。 */
	r.aiProviderConfig = &copy /* 更新 r.aiProviderConfig 的值。 */
	return nil                 /* 返回当前处理结果。 */
}                                                  /* 结束当前表达式或代码块。 */
func (r *Repository) Health(context.Context) error { return nil } /* 定义 Health 函数。 */
func (r *Repository) Close() error                 { return nil } /* 定义 Close 函数。 */

func cloneAnyMap(src map[string]any) map[string]any { /* 定义 cloneAnyMap 函数。 */
	if src == nil { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	dst := make(map[string]any, len(src)) /* 更新 dst 的值。 */
	for k, value := range src {           /* 循环处理当前数据。 */
		switch typed := value.(type) { /* 根据条件选择处理路径。 */
		case map[string]any: /* 处理当前分支。 */
			dst[k] = cloneAnyMap(typed) /* 更新 dst[k] 的值。 */
		case []any: /* 处理当前分支。 */
			items := make([]any, len(typed)) /* 更新 items 的值。 */
			for i, item := range typed {     /* 循环处理当前数据。 */
				if nested, ok := item.(map[string]any); ok { /* 判断条件并选择处理分支。 */
					items[i] = cloneAnyMap(nested) /* 更新 items[i] 的值。 */
				} else { /* 结束当前表达式或代码块。 */
					items[i] = item /* 更新 items[i] 的值。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
			dst[k] = items /* 更新 dst[k] 的值。 */
		case model.VideoAlarmEvent: /* 处理当前分支。 */
			dst[k] = cloneVideoEvent(typed) /* 更新 dst[k] 的值。 */
		default: /* 处理当前分支。 */
			dst[k] = value /* 更新 dst[k] 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return dst /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func cloneVideoEvent(v model.VideoAlarmEvent) model.VideoAlarmEvent { /* 定义 cloneVideoEvent 函数。 */
	v.Raw = cloneAnyMap(v.Raw) /* 更新 v.Raw 的值。 */
	return v                   /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func cloneAlarm(v model.Alarm) model.Alarm { /* 定义 cloneAlarm 函数。 */
	v.Details = cloneAnyMap(v.Details) /* 更新 v.Details 的值。 */
	return v                           /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func clone[T any](v T) T { b, _ := json.Marshal(v); var out T; _ = json.Unmarshal(b, &out); return out } /* 定义 clone 函数。 */
func cloneManaged(v model.ManagedDevice) model.ManagedDevice { /* 定义 cloneManaged 函数。 */
	if v.Tags != nil { /* 判断条件并选择处理分支。 */
		v.Tags = clone(v.Tags) /* 更新 v.Tags 的值。 */
	} /* 结束当前表达式或代码块。 */
	return v /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

var _ = fmt.Sprintf /* 声明 _。 */

func (r *Repository) MarkRawParseResult(_ context.Context, tenant, id string, at int64, message string) error { /* 定义 MarkRawParseResult 函数。 */
	r.mu.Lock()          /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()  /* 安排函数结束时执行清理。 */
	k := key(tenant, id) /* 更新 k 的值。 */
	v, ok := r.raw[k]    /* 更新 ok 的值。 */
	if !ok {             /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v.ParseAttemptedAt = at /* 更新 v.ParseAttemptedAt 的值。 */
	v.ParseError = message  /* 更新 v.ParseError 的值。 */
	r.raw[k] = v            /* 更新 r.raw[k] 的值。 */
	return nil              /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// UpdateDeviceAccessStatus never writes back a stale configuration snapshot.
func (r *Repository) UpdateDeviceAccessStatus(_ context.Context, expected model.DeviceAccessProfile, status, message string, at int64) (bool, error) { /* 定义 UpdateDeviceAccessStatus 函数。 */
	r.mu.Lock()                                                     /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                                             /* 安排函数结束时执行清理。 */
	k := key(expected.TenantID, expected.ID)                        /* 更新 k 的值。 */
	current, ok := r.accessProfiles[k]                              /* 更新 ok 的值。 */
	if !ok || current.Configuration() != expected.Configuration() { /* 判断条件并选择处理分支。 */
		return false, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	current.RuntimeStatus, current.LastError = status, message   /* 更新 current.LastError 的值。 */
	if status == "ONLINE" || (status == "LISTENING" && at > 0) { /* 判断条件并选择处理分支。 */
		current.LastSuccessAt = at /* 更新 current.LastSuccessAt 的值。 */
	} else if status == "ERROR" { /* 结束当前表达式或代码块。 */
		current.LastErrorAt = at /* 更新 current.LastErrorAt 的值。 */
	} /* 结束当前表达式或代码块。 */
	r.accessProfiles[k] = current /* 更新 r.accessProfiles[k] 的值。 */
	return true, nil              /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
