package model

import "strings"

// DeviceOverview counts a tenant's registered devices and their latest
// states. Empty values are counted under "UNKNOWN".
type DeviceOverview struct {
	Total                  int            `json:"total"`
	ByStatus               map[string]int `json:"byStatus"`
	ByRole                 map[string]int `json:"byRole"`
	AutoRegistered         int            `json:"autoRegistered"`
	Reported               int            `json:"reported"`
	DiscoveredUnregistered int            `json:"discoveredUnregistered"`
	ConnectionStatus       map[string]int `json:"connectionStatus"`
	DataStatus             map[string]int `json:"dataStatus"`
	BusinessStatus         map[string]int `json:"businessStatus"`
	LatestSeenAt           int64          `json:"latestSeenAt"`
}

// AlarmOverview counts the alarms matching a filter; Recent counts those
// triggered at or after the requested time.
type AlarmOverview struct {
	Total          int            `json:"total"`
	Active         int            `json:"active"`
	HighRiskActive int            `json:"highRiskActive"`
	Recent         int            `json:"recent"`
	ByStatus       map[string]int `json:"byStatus"`
	ByLevel        map[string]int `json:"byLevel"`
	BySource       map[string]int `json:"bySource"`
}

// HighRiskAlarmLevels are the alarm levels counted as high risk.
var HighRiskAlarmLevels = []string{"HIGH", "CRITICAL", "EMERGENCY"}

// OverviewKey normalizes a counted value.
func OverviewKey(value string) string {
	if value = strings.TrimSpace(value); value == "" {
		return "UNKNOWN"
	}
	return value
}

func NewDeviceOverview() DeviceOverview {
	return DeviceOverview{ByStatus: map[string]int{}, ByRole: map[string]int{}, ConnectionStatus: map[string]int{}, DataStatus: map[string]int{}, BusinessStatus: map[string]int{}}
}

func NewAlarmOverview() AlarmOverview {
	return AlarmOverview{ByStatus: map[string]int{}, ByLevel: map[string]int{}, BySource: map[string]int{}}
}

// AddAlarm counts one alarm; stores that aggregate in the database return
// the same totals.
func (o *AlarmOverview) AddAlarm(a Alarm, since int64) {
	o.Total++
	o.ByStatus[OverviewKey(a.Status)]++
	o.ByLevel[OverviewKey(a.AlarmLevel)]++
	o.BySource[OverviewKey(a.Source)]++
	if a.Status == "ACTIVE" {
		o.Active++
		for _, level := range HighRiskAlarmLevels {
			if a.AlarmLevel == level {
				o.HighRiskActive++
			}
		}
	}
	if a.LastTriggeredAt >= since {
		o.Recent++
	}
}
