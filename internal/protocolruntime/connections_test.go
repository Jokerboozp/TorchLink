package protocolruntime

import (
	"context"
	"iot-platform/internal/model"
	"reflect"
	"testing"
)

func TestConnectionCountsAcrossListeners(t *testing.T) {
	r := &Listeners{}
	var got []bool
	r.SetConnectionReporter(func(_ context.Context, tenant, product, device string, connected bool, at int64) error {
		got = append(got, connected)
		return nil
	})
	p := model.DeviceAccessProfile{TenantID: "t", ProductID: "p"}
	r.reportConnection(p, "d", true)
	r.reportConnection(p, "d", true)
	r.reportConnection(p, "d", false)
	if !reflect.DeepEqual(got, []bool{true}) {
		t.Fatal(got)
	}
	r.reportConnection(p, "d", false)
	r.reportConnection(p, "d", false)
	if !reflect.DeepEqual(got, []bool{true, false}) {
		t.Fatal(got)
	}
}
