package postgres

import (
	"context"
	"reflect"
	"testing"

	"iot-platform/internal/model"
)

func TestRelationalAccessStateRoundTrip(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	if rev, err := r.AccessRevision(ctx, "t"); err != nil || rev != 0 {
		t.Fatalf("empty revision %d %v", rev, err)
	}
	state := model.AccessState{
		Users: []model.PlatformUser{
			{Username: "zhang", DisplayName: "张三", PasswordHash: "h1", Enabled: true, RoleIDs: []string{"duty"}, Permissions: []string{"menu:alarms"}, DeviceScope: "selected", DeviceIDs: []string{"d3", "d1", "d2"}, SessionVersion: 7, Email: "z@x.cn"},
			{Username: "li", Enabled: false, RoleIDs: []string{}, Permissions: []string{}, DeviceScope: "inherit", DeviceIDs: []string{}},
		},
		Roles:   []model.PlatformRole{{ID: "duty", Name: "值班", Permissions: []string{"menu:devices"}, DeviceScope: "selected", DeviceIDs: []string{"d9"}}},
		APIKeys: []model.APIKey{{ID: "k1", Name: "外部系统", Username: "zhang", Capabilities: []string{"alarms:read"}, SecretHash: "sh", Enabled: true, CreatedAt: 1}},
	}
	if ok, err := r.SaveAccessState(ctx, "t", state); err != nil || !ok {
		t.Fatalf("first save %v %v", ok, err)
	}
	loaded, err := r.LoadAccessState(ctx, "t")
	if err != nil || loaded.Revision != 1 {
		t.Fatalf("load %+v %v", loaded, err)
	}
	state.Revision = 1
	if !reflect.DeepEqual(loaded, state) {
		t.Fatalf("round trip\n got %+v\nwant %+v", loaded, state)
	}
	// A save based on an older revision is refused.
	stale := state
	stale.Revision = 0
	if ok, err := r.SaveAccessState(ctx, "t", stale); err != nil || ok {
		t.Fatalf("stale save accepted %v %v", ok, err)
	}
	// Changing a grant list and removing a user, role and key.
	next := loaded
	next.Users = []model.PlatformUser{loaded.Users[0]}
	next.Users[0].DeviceIDs = []string{"d1"}
	next.Roles, next.APIKeys = nil, nil
	if ok, err := r.SaveAccessState(ctx, "t", next); err != nil || !ok {
		t.Fatalf("second save %v %v", ok, err)
	}
	loaded, _ = r.LoadAccessState(ctx, "t")
	if loaded.Revision != 2 || len(loaded.Users) != 1 || !reflect.DeepEqual(loaded.Users[0].DeviceIDs, []string{"d1"}) || len(loaded.Roles) != 0 || len(loaded.APIKeys) != 0 {
		t.Fatalf("after update %+v", loaded)
	}
	var grants int
	if err = r.pool.QueryRow(ctx, `SELECT count(*) FROM access_device_grant WHERE tenant_id='t'`).Scan(&grants); err != nil || grants != 1 {
		t.Fatalf("orphan grants %d %v", grants, err)
	}
	if rev, _ := r.AccessRevision(ctx, "t"); rev != 2 {
		t.Fatalf("revision %d", rev)
	}
	if other, _ := r.LoadAccessState(ctx, "other"); len(other.Users) != 0 || other.Revision != 0 {
		t.Fatal("access state leaked to another tenant")
	}
}

func TestLegacyAccessDocumentMigration(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	if _, err := pool.Exec(ctx, `CREATE TABLE platform_access (tenant_id text PRIMARY KEY, revision bigint NOT NULL DEFAULT 1, body jsonb NOT NULL);
INSERT INTO platform_access VALUES ('t', 42, '{"revision":42,"users":[{"username":"u1","passwordHash":"h","enabled":true,"roleIds":["r1"],"permissions":["menu:devices"],"deviceScope":"selected","deviceIds":["d1","d2"],"sessionVersion":3}],"roles":[{"id":"r1","name":"角色","permissions":[],"deviceScope":"all","deviceIds":[]}],"apiKeys":[{"id":"k","name":"n","username":"u1","capabilities":[],"secretHash":"s","enabled":true,"createdAt":5}]}')`); err != nil {
		t.Fatal(err)
	}
	r := &Repository{pool: pool}
	if err := r.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	state, err := r.LoadAccessState(ctx, "t")
	if err != nil || state.Revision != 42 || len(state.Users) != 1 || !reflect.DeepEqual(state.Users[0].DeviceIDs, []string{"d1", "d2"}) || state.Users[0].SessionVersion != 3 || len(state.Roles) != 1 || len(state.APIKeys) != 1 {
		t.Fatalf("migrated %+v %v", state, err)
	}
	var legacy bool
	if err = pool.QueryRow(ctx, `SELECT to_regclass('platform_access_legacy') IS NOT NULL AND to_regclass('platform_access') IS NULL`).Scan(&legacy); err != nil || !legacy {
		t.Fatalf("legacy table kept=%v %v", legacy, err)
	}
	// Restarting does not migrate again.
	if err = r.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestFireSafetyRecordsMigrationAndPartialWrites(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	if _, err := pool.Exec(ctx, `CREATE TABLE platform_fire_safety (tenant_id text PRIMARY KEY, revision bigint NOT NULL DEFAULT 1, body jsonb NOT NULL);
INSERT INTO platform_fire_safety VALUES ('t', 9, '{"revision":9,"stations":[{"id":"s1","name":"一站","enabled":true,"version":1,"createdAt":1,"updatedAt":1}],"personnel":[{"id":"p1","name":"王五","phone":"138","stationId":"s1","enabled":true,"version":1,"createdAt":2,"updatedAt":2}],"inspections":[{"id":"i1","extinguisherId":"e1","status":"completed","version":2,"createdAt":3,"updatedAt":4}]}')`); err != nil {
		t.Fatal(err)
	}
	r := &Repository{pool: pool}
	if err := r.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	state, err := r.LoadFireSafetyState(ctx, "t")
	if err != nil || state.Revision != 9 || len(state.Stations) != 1 || len(state.Personnel) != 1 || state.Personnel[0].Phone != "138" || len(state.Inspections) != 1 {
		t.Fatalf("migrated %+v %v", state, err)
	}
	if rev, _ := r.FireSafetyRevision(ctx, "t"); rev != 9 {
		t.Fatalf("revision %d", rev)
	}
	// Changing one record rewrites only that row.
	var xminBefore, xminAfter string
	if err = pool.QueryRow(ctx, `SELECT xmin::text FROM fire_safety_record WHERE id='i1'`).Scan(&xminBefore); err != nil {
		t.Fatal(err)
	}
	state.Personnel[0].Phone = "139"
	if ok, err := r.SaveFireSafetyState(ctx, "t", state); err != nil || !ok {
		t.Fatalf("save %v %v", ok, err)
	}
	if err = pool.QueryRow(ctx, `SELECT xmin::text FROM fire_safety_record WHERE id='i1'`).Scan(&xminAfter); err != nil || xminAfter != xminBefore {
		t.Fatalf("unchanged inspection was rewritten: %s -> %s %v", xminBefore, xminAfter, err)
	}
	var station string
	if err = pool.QueryRow(ctx, `SELECT station_id FROM fire_safety_record WHERE id='p1'`).Scan(&station); err != nil || station != "s1" {
		t.Fatalf("indexed station column %q %v", station, err)
	}
	reloaded, _ := r.LoadFireSafetyState(ctx, "t")
	if reloaded.Revision != 10 || reloaded.Personnel[0].Phone != "139" {
		t.Fatalf("reloaded %+v", reloaded)
	}
}
