package core

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

func TestProtocolAssistantRejectsInvalidGoMapping(t *testing.T) {
	err := parser.ValidateModbusCoilConfig(map[string]any{"fields": []any{map[string]any{"name": "x", "coilAddress": -1}}})
	if err == nil {
		t.Fatal("invalid coil address was accepted")
	}
}

func TestParseAndCompileModbusPointTable(t *testing.T) {
	csv := "标识,名称,功能码,地址,数据类型,倍率,轮询周期\n" +
		"temperature,温度,03,40001,int16,0.1,10\n" +
		"pressure,压力,03,40002,uint16,1,10\n" +
		"smoke,烟感,01,0,bool,1,5\n"
	table, warnings, err := ParseModbusPointTable("points.csv", []byte(csv), 15)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 || len(table.Points) != 3 {
		t.Fatalf("unexpected import result: points=%d warnings=%v", len(table.Points), warnings)
	}
	if table.Points[0].Address != 0 || table.Points[0].FunctionCode != 3 || table.Points[0].Scale != 0.1 {
		t.Fatalf("first point was not normalized: %+v", table.Points[0])
	}
	blocks, err := CompileModbusReadBlocks(table.Points)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 {
		t.Fatalf("want two function/interval blocks, got %+v", blocks)
	}
}

func TestPointTableRejectsAmbiguousAddress(t *testing.T) {
	_, _, err := ParseModbusPointTable("points.csv", []byte("名称,地址,数据类型\n温度,100,int16\n"), 10)
	if err == nil || !strings.Contains(err.Error(), "functionCode is required") {
		t.Fatalf("expected explicit function-code error, got %v", err)
	}
}

type countingProtocolRepo struct {
	*memory.Repository
	bindingReads, releaseReads atomic.Int32
}

func (r *countingProtocolRepo) GetProductProtocolBinding(ctx context.Context, tenant, product string) (model.ProductProtocolBinding, error) {
	r.bindingReads.Add(1)
	return r.Repository.GetProductProtocolBinding(ctx, tenant, product)
}

func (r *countingProtocolRepo) GetProtocolRelease(ctx context.Context, tenant, protocol, version string) (model.ProtocolRelease, error) {
	r.releaseReads.Add(1)
	return r.Repository.GetProtocolRelease(ctx, tenant, protocol, version)
}

// Bindings and releases are read once per window, missing bindings included,
// and a change made through this process is visible at once.
func TestProtocolMetadataIsCachedUntilChanged(t *testing.T) {
	ctx := context.Background()
	repo := &countingProtocolRepo{Repository: memory.NewRepository()}
	e := &Engine{Repo: repo}
	for i := 0; i < 3; i++ {
		if _, err := e.productBinding(ctx, "t1", "p1"); !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("missing binding: %v", err)
		}
	}
	if repo.bindingReads.Load() != 1 {
		t.Fatalf("missing binding read %d times, want once", repo.bindingReads.Load())
	}
	if err := repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "t1", ProtocolID: "proto", Version: "1", Status: "PUBLISHED"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "t1", ProductID: "p1", ProtocolID: "proto", Version: "1"}); err != nil {
		t.Fatal(err)
	}
	e.ProtocolsChanged("t1")
	binding, err := e.productBinding(ctx, "t1", "p1")
	if err != nil || binding.Version != "1" {
		t.Fatalf("binding change not visible after ProtocolsChanged: %#v %v", binding, err)
	}
	for i := 0; i < 3; i++ {
		if _, err = e.protocolRelease(ctx, "t1", "proto", "1"); err != nil {
			t.Fatal(err)
		}
	}
	if repo.releaseReads.Load() != 1 {
		t.Fatalf("release read %d times, want once", repo.releaseReads.Load())
	}
	if err = repo.UpdateProtocolReleaseStatus(ctx, "t1", "proto", "1", "REVOKED", 0); err != nil {
		t.Fatal(err)
	}
	e.ProtocolsChanged("t1")
	if release, _ := e.protocolRelease(ctx, "t1", "proto", "1"); release.Status != "REVOKED" {
		t.Fatalf("revocation not visible after ProtocolsChanged: %s", release.Status)
	}
}
