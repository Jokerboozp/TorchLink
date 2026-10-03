package messagetopics

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"iot-platform/internal/model"
)

// MessageTopicIdentity is resolved from an open API key and its bound user.
// Version covers every policy input, so any change invalidates credentials.
type MessageTopicIdentity struct {
	Permissions map[string]bool
	DeviceScope string
	DeviceIDs   []string
	Version     string
	// ExpiresAt is the API key expiry in Unix seconds; zero never expires.
	ExpiresAt int64
}

// SetAccessResolver installs the API key identity lookup. A key that is
// disabled, expired or lacks the subscription capability must return an error.
func (s *Service) SetAccessResolver(resolve func(context.Context, string, string) (MessageTopicIdentity, error)) {
	s.mu.Lock()
	s.resolver = resolve
	s.mu.Unlock()
}

func permission(identity MessageTopicIdentity, menu string) bool {
	return identity.Permissions["*"] || identity.Permissions["menu:"+menu]
}

// SourceAllowed checks the business permission needed to read one platform
// data source. Device coverage is checked separately against the exposure.
func SourceAllowed(sourceID string, identity MessageTopicIdentity) bool {
	source, ok := topicByID(sourceID)
	if !ok || !source.Editable || (identity.DeviceScope != "all" && identity.DeviceScope != "selected") || !permission(identity, "devices") {
		return false
	}
	switch {
	case strings.HasSuffix(sourceID, ".video-alarm"):
		return permission(identity, "alarms") && permission(identity, "cameras") && identity.DeviceScope == "all"
	case strings.Contains(sourceID, ".alarm-") || strings.HasSuffix(sourceID, ".ui-action"):
		return permission(identity, "alarms")
	default:
		return true
	}
}

// RouteAllowed checks the complete historical exposure of a topic, not only
// its current query: older broker messages stay readable to new subscribers.
func RouteAllowed(route model.MessageTopicRoute, identity MessageTopicIdentity) bool {
	if !route.Enabled || route.Query == nil || !permission(identity, "messageTopics") {
		return false
	}
	for _, exposure := range route.Exposure {
		if !SourceAllowed(exposure.SourceID, identity) || !scopeCovers(identity.DeviceScope, identity.DeviceIDs, exposure.DeviceScope, exposure.DeviceIDs) {
			return false
		}
		// Alarm records can carry camera details.
		if strings.Contains(exposure.SourceID, ".alarm-") && !permission(identity, "cameras") {
			return false
		}
	}
	return true
}

func scopeCovers(scope string, ids []string, requiredScope string, requiredIDs []string) bool {
	if scope == "all" {
		return true
	}
	if scope != "selected" || requiredScope != "selected" {
		return false
	}
	for _, id := range requiredIDs {
		if !slices.Contains(ids, id) {
			return false
		}
	}
	return true
}

var managedID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)

func validScope(scope string, ids []string) bool {
	return (scope == "all" && len(ids) == 0) || (scope == "selected" && len(ids) > 0 && uniqueValues(ids, 10000, 256))
}

func cleanText(v string, max int, required bool) bool {
	if !utf8.ValidString(v) || utf8.RuneCountInString(v) > max || (required && strings.TrimSpace(v) == "") {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func uniqueValues(values []string, maxCount, maxLength int) bool {
	if len(values) > maxCount {
		return false
	}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if !cleanText(value, maxLength, true) || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}
