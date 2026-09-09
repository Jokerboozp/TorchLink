package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
)

var ErrShadowConflict = errors.New("device shadow desired version conflict")
var ErrShadowLimit = errors.New("shadow exceeds 256 properties or 128 KiB")
var ErrShadowCount = errors.New("device already has 16 named shadows")
var shadowNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

func ShadowName(names ...string) (string, error) {
	if len(names) > 1 {
		return "", errors.New("only one shadow name is allowed")
	}
	if len(names) == 0 || names[0] == "" {
		return "", nil
	}
	if !shadowNamePattern.MatchString(names[0]) {
		return "", errors.New("shadow name must contain 1 to 64 letters, digits, dots, underscores or hyphens")
	}
	return names[0], nil
}

type ShadowStamp struct {
	Timestamp int64  `json:"timestamp"`
	MessageID string `json:"messageId"`
}
type DeviceShadow struct {
	Name           string                 `json:"name,omitempty"`
	LastError      string                 `json:"lastError,omitempty"`
	ErrorMessageID string                 `json:"errorMessageId,omitempty"`
	TenantID       string                 `json:"tenantId"`
	DeviceID       string                 `json:"deviceId"`
	Version        int64                  `json:"version"`
	DesiredVersion int64                  `json:"desiredVersion"`
	Desired        map[string]any         `json:"desired"`
	Reported       map[string]any         `json:"reported"`
	ReportedAt     map[string]ShadowStamp `json:"reportedAt"`
	Delta          map[string]any         `json:"delta"`
	UpdatedAt      int64                  `json:"updatedAt"`
}
type ShadowUpdate struct {
	Name                                 string
	ProjectionError                      string
	TenantID, DeviceID, Actor, MessageID string
	ExpectedVersion, Timestamp           int64
	Desired, Reported                    map[string]any
}
type ShadowChange struct {
	Version   int64          `json:"version"`
	Timestamp int64          `json:"timestamp"`
	Actor     string         `json:"actor"`
	Desired   map[string]any `json:"desired"`
}

func (s *DeviceShadow) ComputeDelta() {
	s.Delta = map[string]any{}
	for key, desired := range s.Desired {
		reported, exists := s.Reported[key]
		if !exists || !shadowEqual(desired, reported) {
			s.Delta[key] = desired
		}
	}
}
func shadowEqual(a, b any) bool {
	x, e1 := json.Marshal(a)
	y, e2 := json.Marshal(b)
	return e1 == nil && e2 == nil && bytes.Equal(x, y)
}

// ApplyShadow is shared by transactional repositories. Report clocks are per
// property, so a delayed partial report cannot revert a newer property value.
func ApplyShadow(s *DeviceShadow, u ShadowUpdate) (bool, error) {
	if _, err := ShadowName(u.Name); err != nil {
		return false, err
	}
	if u.TenantID == "" || u.DeviceID == "" || u.Timestamp <= 0 || (u.ProjectionError == "" && ((u.Desired == nil) == (u.Reported == nil))) {
		return false, errors.New("invalid shadow update")
	}
	if u.Reported != nil && u.MessageID == "" {
		return false, errors.New("reported shadow requires message identity")
	}
	if s.TenantID != "" && (s.TenantID != u.TenantID || s.DeviceID != u.DeviceID || s.Name != u.Name) {
		return false, errors.New("shadow identity mismatch")
	}
	if u.Desired != nil && u.ExpectedVersion != s.DesiredVersion {
		return false, ErrShadowConflict
	}
	if s.Desired == nil {
		s.Desired = map[string]any{}
	}
	if s.Reported == nil {
		s.Reported = map[string]any{}
	}
	if s.ReportedAt == nil {
		s.ReportedAt = map[string]ShadowStamp{}
	}
	if u.ProjectionError != "" {
		if len(u.ProjectionError) > 256 || u.MessageID == "" {
			return false, errors.New("invalid shadow projection error")
		}
		s.TenantID, s.DeviceID, s.Name = u.TenantID, u.DeviceID, u.Name
		if s.ErrorMessageID == u.MessageID {
			return false, nil
		}
		s.LastError, s.ErrorMessageID = u.ProjectionError, u.MessageID
		s.Version++
		s.UpdatedAt = max(s.UpdatedAt, u.Timestamp)
		s.ComputeDelta()
		return true, nil
	}
	changed := false
	for key, value := range u.Desired {
		if value == nil {
			if _, ok := s.Desired[key]; ok {
				delete(s.Desired, key)
				changed = true
			}
		} else if previous, ok := s.Desired[key]; !ok || !shadowEqual(previous, value) {
			s.Desired[key] = value
			changed = true
		}
	}
	for key, value := range u.Reported {
		previous, exists := s.ReportedAt[key]
		if exists && (u.Timestamp < previous.Timestamp || (u.Timestamp == previous.Timestamp && u.MessageID <= previous.MessageID)) {
			continue
		}
		s.Reported[key] = value
		s.ReportedAt[key] = ShadowStamp{Timestamp: u.Timestamp, MessageID: u.MessageID}
		changed = true
	}
	if len(s.Desired) > 256 || len(s.Reported) > 256 {
		return false, ErrShadowLimit
	}
	if changed {
		if u.Reported != nil {
			s.LastError, s.ErrorMessageID = "", ""
		}
		s.Version++
		if u.Desired != nil {
			s.DesiredVersion++
		}
		s.UpdatedAt = max(s.UpdatedAt, u.Timestamp)
	}
	s.TenantID, s.DeviceID, s.Name = u.TenantID, u.DeviceID, u.Name
	s.ComputeDelta()
	data, err := json.Marshal(s)
	if err != nil {
		return false, err
	}
	if len(data) > 128<<10 {
		return false, ErrShadowLimit
	}
	return changed, nil
}
