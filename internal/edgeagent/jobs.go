package edgeagent

import (
	"context"
	"encoding/json"
	"errors"
	"iot-platform/internal/model"
	"iot-platform/internal/protocolruntime"
	"net/url"
	"time"
)

func (a *Agent) readJobs(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.record("diagnostic", a.readJob(ctx))
		}
	}
}
func (a *Agent) readJob(ctx context.Context) error {
	var j model.EdgeReadJob
	if err := a.request(ctx, "GET", "/read-jobs", nil, &j); err != nil {
		return err
	}
	if j.ID == "" {
		return nil
	}
	if j.TenantID != a.options.TenantID || j.NodeID != a.options.NodeID || j.Task.Profile.TenantID != j.TenantID || j.Task.Profile.EdgeNodeID != j.NodeID || j.Task.Release.TenantID != j.TenantID {
		return errors.New("foreign diagnostic")
	}
	deadline := time.UnixMilli(j.ExpiresAt)
	if maximum := time.Now().Add(10 * time.Second); deadline.After(maximum) {
		deadline = maximum
	}
	jobCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	var blocks []model.ModbusReadBlock
	b, err := json.Marshal(j.Task.Release.Config["blocks"])
	if err == nil {
		err = json.Unmarshal(b, &blocks)
	}
	if err == nil && (len(blocks) == 0 || len(blocks) > 256) {
		if j.Task.Release.Transport == "MODBUS_TCP" || j.Task.Release.Transport == "MODBUS_RTU" {
			err = errors.New("invalid diagnostic read plan")
		}
	}
	p := j.Task.Profile
	p.EdgeNodeID = ""
	if err == nil {
		switch j.Task.Release.Transport {
		case "ONVIF_DISCOVERY":
			j.Raw, err = a.discoverONVIF(jobCtx, p, j.Task.Release)
		case "OPC_UA", "SNMP", "BACNET", "ONVIF":
			j.Raw, err = a.collector.Read(jobCtx, p, j.Task.Release)
		case "MODBUS_TCP":
			j.Raw, err = protocolruntime.ReadModbusTCPWithPolicy(jobCtx, p, j.Task.Release, blocks, a.options.AllowedCIDRs)
		case "MODBUS_RTU":
			j.Raw, err = protocolruntime.ReadModbusRTU(jobCtx, p, j.Task.Release, blocks, a.options.AllowedSerialPorts)
		default:
			err = errors.New("unsupported diagnostic transport")
		}
	}
	if err != nil {
		j.Error = err.Error()
		if len(j.Error) > 512 {
			j.Error = j.Error[:512]
		}
		j.Raw = nil
	}
	return a.request(ctx, "POST", "/read-jobs/"+url.PathEscape(j.ID), j, nil)
}

func supportsRead(transport string) bool {
	return transport == "MODBUS_TCP" || transport == "MODBUS_RTU" || transport == "OPC_UA" || (transport == "SNMP" || transport == "BACNET")
}
