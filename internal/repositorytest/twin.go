package repositorytest

import (
	"context"
	"errors"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"sync"
	"testing"
	"time"
)

func TwinTopology(t *testing.T, r ports.Repository) {
	t.Helper()
	ctx := context.Background()
	tenant := "twin-tenant"
	for _, id := range []string{"a", "b", "c"} {
		if err := r.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: tenant, ID: id, Name: "Device " + id, ProductID: "twin-product", Status: "ENABLED", AccessKey: "twin-key-" + id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "foreign-twin", ID: "foreign", AccessKey: "foreign-twin-key", ProductID: "p", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	relation := model.TwinRelation{Source: "a", Target: "b", Kind: "contains"}
	request := model.TwinUpdate{TenantID: tenant, Timestamp: time.Now().UnixMilli(), Add: []model.TwinRelation{relation}}
	var wg sync.WaitGroup
	success := make(chan bool, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.UpdateTwinTopology(ctx, request)
			if err == nil {
				success <- true
			} else if !errors.Is(err, model.ErrTwinConflict) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(success) != 1 {
		t.Fatal("topology writers", len(success))
	}
	graph, err := r.GetTwinTopology(ctx, tenant)
	if err != nil || graph.Version != 1 || len(graph.Relations) != 1 {
		t.Fatal(graph, err)
	}
	request.ExpectedVersion = 1
	graph, err = r.UpdateTwinTopology(ctx, request)
	if err != nil || graph.Version != 1 {
		t.Fatal("duplicate edge changed version", graph, err)
	}
	for _, bad := range []model.TwinRelation{{Source: "b", Target: "a", Kind: "contains"}, {Source: "c", Target: "b", Kind: "contains"}, {Source: "a", Target: "foreign", Kind: "monitors"}, {Source: "a", Target: "a", Kind: "depends_on"}} {
		update := request
		update.Add = []model.TwinRelation{bad}
		if _, err := r.UpdateTwinTopology(ctx, update); err == nil {
			t.Fatal("invalid relation", bad)
		}
	}
	nodes, err := r.GetTwinNodes(ctx, tenant, []string{"a", "b", "foreign"})
	if err != nil || len(nodes) != 2 {
		t.Fatal("node tenant isolation", nodes, err)
	}
	for _, n := range nodes {
		if n.ConnectionStatus != "UNKNOWN" {
			t.Fatal("invented connection state", n)
		}
	}
	request.Add = []model.TwinRelation{{Source: "a", Target: "c", Kind: "depends_on"}}
	graph, err = r.UpdateTwinTopology(ctx, request)
	if err != nil || graph.Version != 2 {
		t.Fatal(graph, err)
	}
	request.ExpectedVersion = 2
	request.Add = []model.TwinRelation{{Source: "c", Target: "a", Kind: "depends_on"}}
	if _, err := r.UpdateTwinTopology(ctx, request); err == nil {
		t.Fatal("dependency cycle accepted")
	}
	request.Add = nil
	request.Remove = []string{relation.Key()}
	graph, err = r.UpdateTwinTopology(ctx, request)
	if err != nil || graph.Version != 3 || len(graph.Relations) != 1 {
		t.Fatal("remove relation", graph, err)
	}
	foreign, err := r.GetTwinTopology(ctx, "foreign-twin")
	if err != nil || len(foreign.Relations) != 0 {
		t.Fatal("foreign graph exposed", err)
	}
}
