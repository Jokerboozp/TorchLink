package memory

import (
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"slices"
	"strings"
)

// Caller holds r.mu. Build once per query rather than scanning messages per row.
func (r *Repository) rawFilterStandards(f ports.RawFilter) map[string]model.StandardMessage {
	if f.ParseStatus == "" && f.MessageType == "" && f.Parser == "" {
		return nil
	}
	out := map[string]model.StandardMessage{}
	for _, message := range r.standard {
		if f.TenantID != "" && message.TenantID != f.TenantID {
			continue
		}
		k := key(message.TenantID, message.RawMessageID)
		previous, exists := out[k]
		if !exists || message.Timestamp > previous.Timestamp || message.Timestamp == previous.Timestamp && message.MessageID > previous.MessageID {
			out[k] = message
		}
	}
	return out
}

func matchesRawFilter(v model.RawArchiveIndex, message model.StandardMessage, f ports.RawFilter) bool {
	if f.TenantID != "" && v.TenantID != f.TenantID || f.ProductID != "" && v.ProductID != f.ProductID || f.DeviceID != "" && v.DeviceID != f.DeviceID || f.MessageID != "" && v.MessageID != f.MessageID || f.Start > 0 && v.ReceivedAt < f.Start || f.End > 0 && v.ReceivedAt > f.End {
		return false
	}
	if f.DeviceIDs != nil && !slices.Contains(f.DeviceIDs, v.DeviceID) {
		return false
	}
	if f.Protocol != "" && !strings.EqualFold(v.Protocol, f.Protocol) || f.PayloadFormat != "" && !strings.EqualFold(v.PayloadFormat, f.PayloadFormat) {
		return false
	}
	status := "UNPARSED"
	if message.MessageID != "" {
		status = "PARSED"
	} else if v.ParseError != "" {
		status = "FAILED"
	}
	return (f.ParseStatus == "" || f.ParseStatus == status) && (f.MessageType == "" || f.MessageType == string(message.MessageType)) && (f.Parser == "" || f.Parser == message.Parser)
}
