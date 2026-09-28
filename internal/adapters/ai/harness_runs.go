package aiadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"iot-platform/internal/ports"
)

func (h *HarnessClient) ListWorkflowRuns(ctx context.Context, tenant string) ([]ports.AIWorkflowRun, error) {
	if strings.TrimSpace(tenant) == "" {
		return nil, errors.New("tenant is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.baseURL+"/v1/runs", nil)
	if err != nil {
		return nil, err
	}
	h.authorizeService(req)
	req.Header.Set("X-IOT-Tenant-ID", tenant)
	res, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil, ports.ErrAIWorkflowManagementUnavailable
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list AI workflow runs: status %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxHarnessResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxHarnessResponseBytes {
		return nil, errors.New("workflow run list too large")
	}
	var envelope struct {
		Items []ports.AIWorkflowRun `json:"items"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	items := []ports.AIWorkflowRun{}
	for _, item := range envelope.Items {
		if item.TenantID == tenant {
			items = append(items, item)
		}
	}
	return items, nil
}

func (h *HarnessClient) StopWorkflowRun(ctx context.Context, tenant, runID string) error {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(runID) == "" {
		return errors.New("tenant and run ID are required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.baseURL+"/v1/runs/"+url.PathEscape(runID)+"/stop", bytes.NewReader(nil))
	if err != nil {
		return err
	}
	h.authorizeService(req)
	req.Header.Set("X-IOT-Tenant-ID", tenant)
	res, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	if res.StatusCode == http.StatusNotFound {
		return ports.ErrAIWorkflowRunNotFound
	}
	if res.StatusCode != http.StatusAccepted {
		return fmt.Errorf("stop AI workflow run: status %d", res.StatusCode)
	}
	return nil
}

func (p *HarnessPool) ListWorkflowRuns(ctx context.Context, tenant string) ([]ports.AIWorkflowRun, error) {
	items := []ports.AIWorkflowRun{}
	for _, member := range p.members {
		part, err := member.ListWorkflowRuns(ctx, tenant)
		if err != nil {
			return nil, err
		}
		items = append(items, part...)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].StartedAt < items[j].StartedAt })
	return items, nil
}

func (p *HarnessPool) StopWorkflowRun(ctx context.Context, tenant, runID string) error {
	for _, member := range p.members {
		err := member.StopWorkflowRun(ctx, tenant, runID)
		if errors.Is(err, ports.ErrAIWorkflowRunNotFound) {
			continue
		}
		return err
	}
	return ports.ErrAIWorkflowRunNotFound
}
