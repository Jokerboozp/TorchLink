package sites

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"strconv"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
)

func TestSiteHierarchyPointsAndLocation(t *testing.T) {
	ctx := context.Background()
	svc := New(memory.NewRepository())
	unit, err := svc.SaveUnit(ctx, "t", "admin", model.SiteUnit{Name: " 示例单位 "})
	if err != nil || unit.Name != "示例单位" || unit.Version != 1 {
		t.Fatalf("unit %+v %v", unit, err)
	}
	if _, err = svc.SaveUnit(ctx, "t", "admin", model.SiteUnit{Name: "示例单位"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate unit name: %v", err)
	}
	building, err := svc.SaveBuilding(ctx, "t", "admin", model.SiteBuilding{UnitID: unit.ID, Name: "1 号楼"})
	if err != nil {
		t.Fatal(err)
	}
	floor, err := svc.SaveFloor(ctx, "t", "admin", model.SiteFloor{BuildingID: building.ID, Name: "3F", Level: 3})
	if err != nil {
		t.Fatal(err)
	}
	x, y := 0.5, 0.25
	if _, err = svc.SavePoint(ctx, "t", "admin", model.SitePoint{FloorID: floor.ID, DeviceID: "panel", X: &x, Y: &y}); !errors.Is(err, ErrValidation) {
		t.Fatalf("coordinates without a plan must be refused: %v", err)
	}
	floor, _, err = svc.SetFloorPlan(ctx, "t", "admin", floor.ID, floor.Version, model.FloorPlan{ContentType: "image/png", SHA256: "h", Width: 10, Height: 10})
	if err != nil || floor.Version != 2 {
		t.Fatalf("plan %+v %v", floor, err)
	}
	if _, _, err = svc.SetFloorPlan(ctx, "t", "admin", floor.ID, 1, model.FloorPlan{}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale floor version: %v", err)
	}
	bad := 1.5
	if _, err = svc.SavePoint(ctx, "t", "admin", model.SitePoint{FloorID: floor.ID, DeviceID: "panel", X: &bad, Y: &y}); !errors.Is(err, ErrValidation) {
		t.Fatalf("coordinates outside the plan: %v", err)
	}
	device, err := svc.SavePoint(ctx, "t", "admin", model.SitePoint{FloorID: floor.ID, DeviceID: "panel", Name: "消防主机", X: &x, Y: &y})
	if err != nil || device.UnitID != unit.ID || device.BuildingID != building.ID {
		t.Fatalf("point must inherit building and unit from its floor: %+v %v", device, err)
	}
	if _, err = svc.SavePoint(ctx, "t", "admin", model.SitePoint{UnitID: unit.ID, DeviceID: "panel"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("second device-level point: %v", err)
	}
	other, err := svc.SaveUnit(ctx, "t", "admin", model.SiteUnit{Name: "其他单位"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SavePoint(ctx, "t", "admin", model.SitePoint{UnitID: other.ID, BuildingID: building.ID, DeviceID: "x"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("building of another unit: %v", err)
	}
	if _, err = svc.SavePoint(ctx, "t", "admin", model.SitePoint{UnitID: unit.ID, BuildingID: building.ID, DeviceID: "panel", ComponentID: "loop1-7", Name: "二楼烟感"}); err != nil {
		t.Fatal(err)
	}

	state, err := svc.Snapshot(ctx, "t")
	if err != nil {
		t.Fatal(err)
	}
	if units := DeviceUnits(state); len(units) != 1 || units["panel"] != unit.ID {
		t.Fatalf("component points must not decide the device's unit: %v", units)
	}
	component := Locate(state, "panel", "loop1-7")
	if component == nil || component.PointName != "二楼烟感" || component.FloorID != "" || component.BuildingName != "1 号楼" {
		t.Fatalf("component location %+v", component)
	}
	fallback := Locate(state, "panel", "unknown")
	if fallback == nil || fallback.FloorName != "3F" || fallback.UnitName != "示例单位" || *fallback.X != 0.5 {
		t.Fatalf("unknown components fall back to the device point: %+v", fallback)
	}
	if Locate(state, "elsewhere", "") != nil {
		t.Fatal("unplaced device has no location")
	}

	if err = svc.DeleteUnit(ctx, "t", unit.ID, unit.Version); !errors.Is(err, ErrConflict) {
		t.Fatalf("unit with buildings: %v", err)
	}
	if _, err = svc.DeleteFloor(ctx, "t", floor.ID, floor.Version); !errors.Is(err, ErrConflict) {
		t.Fatalf("floor with points: %v", err)
	}
	if _, err = svc.DeletePoint(ctx, "t", device.ID, device.Version); err != nil {
		t.Fatal(err)
	}
	plan, err := svc.DeleteFloor(ctx, "t", floor.ID, floor.Version)
	if err != nil || plan == nil || plan.SHA256 != "h" {
		t.Fatalf("deleted floor returns its plan for removal: %+v %v", plan, err)
	}
}

func xlsx(t *testing.T, rows [][]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	add := func(name, body string) {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	add("xl/workbook.xml", `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="点位" sheetId="1" r:id="rId1"/></sheets></workbook>`)
	add("xl/_rels/workbook.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`)
	shared := `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`
	sheet := `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`
	index := 0
	for r, row := range rows {
		sheet += `<row>`
		for c, value := range row {
			if value == "" {
				continue
			}
			ref := string(rune('A'+c)) + strconv.Itoa(r+1)
			if value == "3" {
				sheet += `<c r="` + ref + `"><v>3</v></c>`
				continue
			}
			shared += `<si><t>` + value + `</t></si>`
			sheet += `<c r="` + ref + `" t="s"><v>` + strconv.Itoa(index) + `</v></c>`
			index++
		}
		sheet += `</row>`
	}
	add("xl/sharedStrings.xml", shared+`</sst>`)
	add("xl/worksheets/sheet1.xml", sheet+`</sheetData></worksheet>`)
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImportCreatesHierarchyAndRejectsInvalidRows(t *testing.T) {
	ctx := context.Background()
	svc := New(memory.NewRepository())
	allowed := func(id string) bool { return id != "secret" }

	gbk, err := simplifiedchinese.GB18030.NewEncoder().String("单位名称,建筑名称,楼层名称,楼层序号,设备编号,部件编号,点位名称\n示例单位,1 号楼,3F,3,panel,,消防主机\n示例单位,1 号楼,3F,3,panel,loop1-7,三层烟感\n示例单位,2 号楼,,,,,\n")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ParseImport("points.csv", []byte(gbk))
	if err != nil || len(rows) != 3 || rows[0].Level != 3 || rows[1].ComponentID != "loop1-7" {
		t.Fatalf("GB18030 CSV: %+v %v", rows, err)
	}
	result, err := svc.Import(ctx, "t", "admin", rows, allowed)
	if err != nil || result.Units != 1 || result.Buildings != 2 || result.Floors != 1 || result.PointsCreated != 2 {
		t.Fatalf("import %+v %v", result, err)
	}

	// The XLSX moves the device point to another floor and refuses the
	// device outside the importer's scope; nothing is saved.
	data := xlsx(t, [][]string{{"单位名称", "建筑名称", "楼层名称", "楼层序号", "设备编号"}, {"示例单位", "1 号楼", "5F", "", "panel"}, {"示例单位", "", "", "", "secret"}})
	rows, err = ParseImport("points.xlsx", data)
	if err != nil || len(rows) != 2 || rows[0].Floor != "5F" {
		t.Fatalf("xlsx rows %+v %v", rows, err)
	}
	result, err = svc.Import(ctx, "t", "admin", rows, allowed)
	if !errors.Is(err, ErrValidation) || len(result.Errors) != 1 || result.Errors[0].Line != 3 {
		t.Fatalf("invalid rows must reject the whole import: %+v %v", result, err)
	}
	state, _ := svc.Snapshot(ctx, "t")
	if len(state.Floors) != 1 {
		t.Fatalf("failed import saved records: %+v", state.Floors)
	}
	result, err = svc.Import(ctx, "t", "admin", rows[:1], allowed)
	if err != nil || result.Floors != 1 || result.PointsUpdated != 1 {
		t.Fatalf("move point: %+v %v", result, err)
	}
	state, _ = svc.Snapshot(ctx, "t")
	if location := Locate(state, "panel", ""); location == nil || location.FloorName != "5F" || location.PointName != "消防主机" {
		t.Fatalf("moved point keeps its name: %+v", location)
	}
	if _, err = ParseImport("x.csv", []byte("设备编号\npanel\n")); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing unit column: %v", err)
	}
}

// Instances work from their cached snapshots; a change made through another
// instance is picked up instead of being overwritten, and a failed change
// leaves the cached snapshot untouched.
func TestInstancesShareStoreWithoutLosingChanges(t *testing.T) {
	ctx := context.Background()
	store := memory.NewRepository()
	a, b := New(store), New(store)
	unit, err := a.SaveUnit(ctx, "t", "admin", model.SiteUnit{Name: "甲单位"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.Snapshot(ctx, "t"); err != nil {
		t.Fatal(err)
	}
	if _, err = a.SaveUnit(ctx, "t", "admin", model.SiteUnit{Name: "乙单位"}); err != nil {
		t.Fatal(err)
	}
	if _, err = b.SavePoint(ctx, "t", "admin", model.SitePoint{UnitID: unit.ID, DeviceID: "d1"}); err != nil {
		t.Fatal(err)
	}
	if _, err = b.SavePoint(ctx, "t", "admin", model.SitePoint{UnitID: "missing", DeviceID: "d2"}); err == nil {
		t.Fatal("point in a missing unit accepted")
	}
	for _, svc := range []*Service{a, b} {
		state, err := svc.Snapshot(ctx, "t")
		if err != nil || len(state.Units) != 2 || len(state.Points) != 1 || state.Revision != 3 {
			t.Fatalf("snapshot %+v %v", state, err)
		}
	}
}
