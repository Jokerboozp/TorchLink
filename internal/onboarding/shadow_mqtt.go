package onboarding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MQTTShadowReply relies on authenticated Broker publisher identity and exact
// topic ACLs, then checks current tenant/product/device inventory again.
func (s *Service) MQTTShadowReply(ctx context.Context, tenant, product, device string, payload []byte) (string, []byte, error) {
	if len(payload) == 0 || len(payload) > 1024 {
		return "", nil, errors.New("shadow query exceeds 1 KiB")
	}
	if !s.Allow("shadow/" + tenant + "/" + device) {
		return "", nil, ErrRate
	}
	d, err := s.Repo.GetManagedDevice(ctx, tenant, device)
	if err != nil || d.ProductID != product || d.Status != "ENABLED" || d.SecretHash == "" || (d.Tags["connector"] != "MQTT" && d.Tags["connector"] != "HTTP") {
		return "", nil, ErrAuth
	}
	p, err := s.Repo.GetProduct(ctx, tenant, product)
	if err != nil || p.Status != "ENABLED" {
		return "", nil, ErrAuth
	}
	var request struct {
		ID string `json:"id"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || !segment.MatchString(request.ID) {
		return "", nil, errors.New("shadow query requires an id and no device identity overrides")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return "", nil, errors.New("trailing shadow query JSON")
	}
	shadow, err := s.Repo.GetDeviceShadow(ctx, tenant, device)
	response := map[string]any{"id": request.ID, "status": "ok", "shadow": shadow}
	if err != nil {
		response = map[string]any{"id": request.ID, "status": "error", "error": "device shadow is unavailable"}
	}
	data, err := json.Marshal(response)
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("/iot/down/%s/%s/%s/shadow", tenant, product, device), data, nil
}
