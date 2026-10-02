package model

import (
	"bytes"
	"encoding/json"
	"sort"
)

// SameTemplateSnapshot checks configuration, never transient listener health.
func SameTemplateSnapshot(current, expected Product, profiles, expectedProfiles []DeviceAccessProfile) bool {
	current.PreparationStatus = ""
	current.Reusable = false
	expected.PreparationStatus = ""
	expected.Reusable = false
	a, _ := json.Marshal(current)
	b, _ := json.Marshal(expected)
	if !bytes.Equal(a, b) {
		return false
	}
	normalize := func(input []DeviceAccessProfile) []DeviceAccessProfile {
		output := make([]DeviceAccessProfile, 0, len(input))
		for _, p := range input {
			if p.DeviceID == "" {
				p.RuntimeStatus = ""
				p.LastError = ""
				p.LastSuccessAt = 0
				p.LastErrorAt = 0
				output = append(output, p)
			}
		}
		sort.Slice(output, func(i, j int) bool { return output[i].ID < output[j].ID })
		return output
	}
	a, _ = json.Marshal(normalize(profiles))
	b, _ = json.Marshal(normalize(expectedProfiles))
	return bytes.Equal(a, b)
}
