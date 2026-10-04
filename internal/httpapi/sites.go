package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"iot-platform/internal/core"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"iot-platform/internal/sites"
)

const (
	sitePlanBucket = "iot-site-plans"
	maxSitePlan    = 20 << 20
	maxSiteImport  = 10 << 20
)

// siteService shares the engine's site service, so alarm locations and
// requests use one cache.
func siteService(engine *core.Engine) *sites.Service {
	if svc, ok := engine.Locator.(*sites.Service); ok {
		return svc
	}
	svc := sites.New(engine.Repo)
	if engine.Locator == nil {
		engine.Locator = svc
	}
	return svc
}

func siteRouteAction(method, path string) string {
	names := map[string]string{"units": "单位", "buildings": "建筑", "floors": "楼层", "points": "设备点位"}
	for kind, name := range names {
		switch method + " " + path {
		case "POST /api/v1/sites/" + kind:
			return "新增" + name
		case "PUT /api/v1/sites/" + kind + "/:id":
			return "编辑" + name
		case "DELETE /api/v1/sites/" + kind + "/:id":
			return "删除" + name
		}
	}
	return map[string]string{"PUT /api/v1/sites/floors/:id/plan": "上传楼层平面图", "POST /api/v1/sites/import": "批量导入单位与点位"}[method+" "+path]
}

func (s *Server) siteRoutes() {
	s.router.GET("/api/v1/sites", s.authorize("viewer"), s.endpoint(s.listSites))
	s.router.GET("/api/v1/sites/import-template", s.authorize("viewer"), s.endpoint(s.siteImportTemplate))
	s.router.POST("/api/v1/sites/import", s.authorize("operator"), s.endpoint(s.importSites))
	for _, kind := range []string{"units", "buildings", "floors", "points"} {
		s.router.POST("/api/v1/sites/"+kind, s.authorize("operator"), s.endpoint(s.saveSite(kind)))
		s.router.PUT("/api/v1/sites/"+kind+"/:id", s.authorize("operator"), s.endpoint(s.saveSite(kind), "id"))
		s.router.DELETE("/api/v1/sites/"+kind+"/:id", s.authorize("operator"), s.endpoint(s.deleteSite(kind), "id"))
	}
	s.router.PUT("/api/v1/sites/floors/:id/plan", s.authorize("operator"), s.endpoint(s.uploadFloorPlan, "id"))
	s.router.GET("/api/v1/sites/floors/:id/plan", s.authorize("viewer"), s.endpoint(s.floorPlan, "id"))
	s.router.GET("/api/v1/alarms/:id/location-plan", s.authorize("viewer"), s.endpoint(s.alarmLocationPlan, "id"))
}

func siteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errDeviceScope):
		problem(w, 403, err.Error())
	case errors.Is(err, sites.ErrValidation):
		problem(w, 422, err.Error())
	case errors.Is(err, sites.ErrNotFound):
		problem(w, 404, err.Error())
	case errors.Is(err, sites.ErrConflict):
		problem(w, 409, err.Error())
	default:
		problem(w, 500, "单位建筑数据读取或保存失败，请稍后重试")
	}
}

type sitePointView struct {
	model.SitePoint
	DeviceName string `json:"deviceName,omitempty"`
}

// listSites returns the tenant's units, buildings and floors, and the points
// of devices the caller may see.
func (s *Server) listSites(w http.ResponseWriter, r *http.Request) {
	tenant := claims(r).TenantID
	state, err := s.sites.Snapshot(r.Context(), tenant)
	if err != nil {
		siteError(w, err)
		return
	}
	state.Units, state.Buildings, state.Floors = append([]model.SiteUnit(nil), state.Units...), append([]model.SiteBuilding(nil), state.Buildings...), append([]model.SiteFloor(nil), state.Floors...)
	points := []model.SitePoint{}
	ids, seen := []string{}, map[string]bool{}
	for _, p := range state.Points {
		if deviceAllowed(r.Context(), tenant, p.DeviceID) {
			points = append(points, p)
			if !seen[p.DeviceID] {
				seen[p.DeviceID] = true
				ids = append(ids, p.DeviceID)
			}
		}
	}
	state.Points = points
	sites.SortState(&state)
	names := map[string]string{}
	for start := 0; start < len(ids); start += 1000 {
		end := min(start+1000, len(ids))
		// The IDs are already limited to the caller's scope.
		devices, _, err := s.unscopedRepo().ListManagedDevicesFiltered(r.Context(), ports.DeviceFilter{TenantID: tenant, RestrictDevices: true, DeviceIDs: ids[start:end]}, end-start, 0)
		if err != nil {
			siteError(w, err)
			return
		}
		for _, d := range devices {
			names[d.ID] = d.Name
		}
	}
	views := make([]sitePointView, len(state.Points))
	for i, p := range state.Points {
		views[i] = sitePointView{SitePoint: p, DeviceName: names[p.DeviceID]}
	}
	write(w, 200, map[string]any{"revision": state.Revision, "units": state.Units, "buildings": state.Buildings, "floors": state.Floors, "points": views})
}

// checkPointDevice requires an existing device within the caller's scope.
func (s *Server) checkPointDevice(r *http.Request, deviceID string) error {
	tenant := claims(r).TenantID
	if !deviceAllowed(r.Context(), tenant, deviceID) {
		return errDeviceScope
	}
	if _, err := s.engine.Repo.GetManagedDevice(r.Context(), tenant, deviceID); err != nil {
		return errDeviceScope
	}
	return nil
}

func (s *Server) saveSite(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claims(r)
		id := r.PathValue("id")
		var result any
		var err error
		switch kind {
		case "units":
			var in model.SiteUnit
			if decode(w, r, &in) != nil {
				return
			}
			in.ID = id
			result, err = s.sites.SaveUnit(r.Context(), c.TenantID, c.Username, in)
		case "buildings":
			var in model.SiteBuilding
			if decode(w, r, &in) != nil {
				return
			}
			in.ID = id
			result, err = s.sites.SaveBuilding(r.Context(), c.TenantID, c.Username, in)
		case "floors":
			var in model.SiteFloor
			if decode(w, r, &in) != nil {
				return
			}
			in.ID = id
			result, err = s.sites.SaveFloor(r.Context(), c.TenantID, c.Username, in)
		case "points":
			var in model.SitePoint
			if decode(w, r, &in) != nil {
				return
			}
			in.ID = id
			in.DeviceID = strings.TrimSpace(in.DeviceID)
			if err = s.checkPointDevice(r, in.DeviceID); err == nil && id != "" {
				err = s.checkExistingPoint(r, id)
			}
			if err == nil {
				result, err = s.sites.SavePoint(r.Context(), c.TenantID, c.Username, in)
			}
		}
		if err != nil {
			siteError(w, err)
			return
		}
		targetID := id
		if targetID == "" {
			targetID = siteRecordID(result)
		}
		s.audit(r, "site.save", strings.TrimSuffix(kind, "s"), targetID, nil)
		status := http.StatusOK
		if id == "" {
			status = http.StatusCreated
		}
		write(w, status, result)
	}
}

func siteRecordID(v any) string {
	switch record := v.(type) {
	case model.SiteUnit:
		return record.ID
	case model.SiteBuilding:
		return record.ID
	case model.SiteFloor:
		return record.ID
	case model.SitePoint:
		return record.ID
	}
	return ""
}

// checkExistingPoint keeps a limited user from moving or deleting the point
// of a device outside their scope.
func (s *Server) checkExistingPoint(r *http.Request, id string) error {
	state, err := s.sites.Snapshot(r.Context(), claims(r).TenantID)
	if err != nil {
		return err
	}
	for _, p := range state.Points {
		if p.ID == id && !deviceAllowed(r.Context(), claims(r).TenantID, p.DeviceID) {
			return errDeviceScope
		}
	}
	return nil
}

func (s *Server) deleteSite(kind string) endpointHandler {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claims(r)
		id := r.PathValue("id")
		version, err := strconv.ParseInt(r.URL.Query().Get("version"), 10, 64)
		if err != nil || version < 1 {
			problem(w, 400, "请提供当前记录版本后重试")
			return
		}
		switch kind {
		case "units":
			err = s.sites.DeleteUnit(r.Context(), c.TenantID, id, version)
		case "buildings":
			err = s.sites.DeleteBuilding(r.Context(), c.TenantID, id, version)
		case "floors":
			var plan *model.FloorPlan
			if plan, err = s.sites.DeleteFloor(r.Context(), c.TenantID, id, version); err == nil && plan != nil {
				s.removeFloorPlan(r, id, plan)
			}
		case "points":
			if err = s.checkExistingPoint(r, id); err == nil {
				_, err = s.sites.DeletePoint(r.Context(), c.TenantID, id, version)
			}
		}
		if err != nil {
			siteError(w, err)
			return
		}
		s.audit(r, "site.delete", strings.TrimSuffix(kind, "s"), id, nil)
		write(w, 200, map[string]bool{"success": true})
	}
}

func floorPlanKey(tenant, floorID string, plan model.FloorPlan) string {
	ext := ".png"
	if plan.ContentType == "image/jpeg" {
		ext = ".jpg"
	}
	return fmt.Sprintf("%s/floors/%s/%s%s", tenant, floorID, plan.SHA256, ext)
}

func (s *Server) removeFloorPlan(r *http.Request, floorID string, plan *model.FloorPlan) {
	if deleter, ok := s.engine.Archive.(ports.ObjectDeleter); ok {
		_ = deleter.DeleteObject(r.Context(), sitePlanBucket, floorPlanKey(claims(r).TenantID, floorID, *plan))
	}
}

// uploadFloorPlan accepts a PNG or JPEG floor plan of up to 20 MiB.
func (s *Server) uploadFloorPlan(w http.ResponseWriter, r *http.Request) {
	if s.engine.Archive == nil {
		problem(w, 503, "对象存储未配置，无法保存平面图")
		return
	}
	version, err := strconv.ParseInt(r.URL.Query().Get("version"), 10, 64)
	if err != nil || version < 1 {
		problem(w, 400, "请提供当前楼层版本后重试")
		return
	}
	data, _, ok := readUpload(w, r, maxSitePlan, "平面图不能超过 20 MiB")
	if !ok {
		return
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") {
		problem(w, 422, "平面图须为 PNG 或 JPEG 图片")
		return
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 16000 || config.Height > 16000 {
		problem(w, 422, "平面图尺寸须在 16000×16000 像素以内")
		return
	}
	digest := sha256.Sum256(data)
	plan := model.FloorPlan{ContentType: "image/" + format, SHA256: hex.EncodeToString(digest[:]), Size: int64(len(data)), Width: config.Width, Height: config.Height}
	c := claims(r)
	id := r.PathValue("id")
	if _, err = s.engine.Archive.PutObject(r.Context(), sitePlanBucket, floorPlanKey(c.TenantID, id, plan), bytes.NewReader(data), int64(len(data)), plan.ContentType); err != nil {
		problem(w, 502, "平面图保存失败，请检查对象存储")
		return
	}
	floor, previous, err := s.sites.SetFloorPlan(r.Context(), c.TenantID, c.Username, id, version, plan)
	if err != nil {
		siteError(w, err)
		return
	}
	if previous != nil && previous.SHA256 != plan.SHA256 {
		s.removeFloorPlan(r, id, previous)
	}
	s.audit(r, "site.floor-plan", "floor", id, map[string]any{"sha256": plan.SHA256, "size": plan.Size})
	write(w, 200, floor)
}

// readUpload reads the multipart "file" field of at most limit bytes.
func readUpload(w http.ResponseWriter, r *http.Request, limit int64, tooLarge string) ([]byte, string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, limit+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			problem(w, http.StatusRequestEntityTooLarge, tooLarge)
		} else {
			problem(w, 400, "请以 multipart 表单上传文件")
		}
		return nil, "", false
	}
	defer r.MultipartForm.RemoveAll()
	f, h, err := r.FormFile("file")
	if err != nil {
		problem(w, 400, "请选择文件")
		return nil, "", false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		problem(w, http.StatusRequestEntityTooLarge, tooLarge)
		return nil, "", false
	}
	return data, h.Filename, true
}

func (s *Server) writeFloorPlan(w http.ResponseWriter, r *http.Request, tenant string, floor model.SiteFloor) {
	if floor.Plan == nil || s.engine.Archive == nil {
		problem(w, 404, "该楼层没有平面图")
		return
	}
	reader, err := s.engine.Archive.GetObject(r.Context(), sitePlanBucket, floorPlanKey(tenant, floor.ID, *floor.Plan))
	if err != nil {
		problem(w, 404, "平面图文件不存在")
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", floor.Plan.ContentType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("ETag", `"`+floor.Plan.SHA256+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, io.LimitReader(reader, maxSitePlan))
}

func (s *Server) floorPlan(w http.ResponseWriter, r *http.Request) {
	tenant := claims(r).TenantID
	state, err := s.sites.Snapshot(r.Context(), tenant)
	if err != nil {
		siteError(w, err)
		return
	}
	for _, floor := range state.Floors {
		if floor.ID == r.PathValue("id") {
			s.writeFloorPlan(w, r, tenant, floor)
			return
		}
	}
	problem(w, 404, "楼层不存在")
}

// alarmLocationPlan serves the floor plan of an alarm's location to anyone
// who may read the alarm, without the sites menu.
func (s *Server) alarmLocationPlan(w http.ResponseWriter, r *http.Request) {
	tenant := claims(r).TenantID
	alarm, err := s.engine.Repo.GetAlarm(r.Context(), tenant, r.PathValue("id"))
	if err != nil {
		problem(w, 404, "告警不存在")
		return
	}
	location := alarm.Location
	if location == nil {
		state, err := s.sites.Snapshot(r.Context(), tenant)
		if err == nil {
			location = sites.Locate(state, alarm.DeviceID, alarm.ComponentID)
		}
	}
	if location == nil || location.FloorID == "" {
		problem(w, 404, "告警没有楼层位置")
		return
	}
	state, err := s.sites.Snapshot(r.Context(), tenant)
	if err != nil {
		siteError(w, err)
		return
	}
	for _, floor := range state.Floors {
		if floor.ID == location.FloorID {
			s.writeFloorPlan(w, r, tenant, floor)
			return
		}
	}
	problem(w, 404, "楼层已删除")
}

func (s *Server) siteImportTemplate(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="site-import-template.csv"`)
	_, _ = io.WriteString(w, "\ufeff"+strings.Join(sites.ImportColumns, ",")+"\n"+"示例大厦物业,DW-001,示例路 1 号,1 号楼,3F,3,smoke-0301,,三层东侧走廊\n")
}

func (s *Server) importSites(w http.ResponseWriter, r *http.Request) {
	data, filename, ok := readUpload(w, r, maxSiteImport, "导入文件不能超过 10 MiB")
	if !ok {
		return
	}
	rows, err := sites.ParseImport(filename, data)
	if err != nil {
		siteError(w, err)
		return
	}
	c := claims(r)
	result, err := s.sites.Import(r.Context(), c.TenantID, c.Username, rows, func(deviceID string) bool { return s.checkPointDevice(r, deviceID) == nil })
	if err != nil {
		if errors.Is(err, sites.ErrValidation) && len(result.Errors) > 0 {
			write(w, 422, map[string]any{"detail": err.Error(), "status": 422, "title": "Unprocessable Entity", "result": result})
			return
		}
		siteError(w, err)
		return
	}
	s.audit(r, "site.import", "site", "", map[string]any{"rows": result.Rows, "pointsCreated": result.PointsCreated, "pointsUpdated": result.PointsUpdated})
	write(w, 200, result)
}

// validUnitGrants keeps unit grants only for the selected device scope and
// requires every unit to exist.
func (s *Server) validUnitGrants(w http.ResponseWriter, r *http.Request, scope string, units []string) ([]string, bool) {
	if scope != "selected" || len(units) == 0 {
		return nil, true
	}
	state, err := s.sites.Snapshot(r.Context(), claims(r).TenantID)
	if err != nil {
		problem(w, 500, "读取单位失败")
		return nil, false
	}
	known := map[string]bool{}
	for _, u := range state.Units {
		known[u.ID] = true
	}
	out := []string{}
	for _, id := range units {
		if !known[id] {
			problem(w, 422, "授权单位不存在")
			return nil, false
		}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, true
}
