package model

import (
	"sort"
	"time"
)

// AlarmDispositionStats summarizes the verification of the alarms in a
// report period.
type AlarmDispositionStats struct {
	Total                 int64              `json:"total"`
	Verified              int64              `json:"verified"`
	RequiringVerification int64              `json:"requiringVerification"`
	Unverified            int64              `json:"unverified"`
	ByResult              map[string]int64   `json:"byResult"`
	FalseAlarmRate        float64            `json:"falseAlarmRate"`
	Acknowledge           DurationStats      `json:"acknowledge"`
	Verify                DurationStats      `json:"verify"`
	TopFalseAlarmDevices  []AlarmDeviceCount `json:"topFalseAlarmDevices"`
}

// DurationStats holds how long a step took: the mean and the nearest-rank
// 90th percentile in milliseconds.
type DurationStats struct {
	Count int64 `json:"count"`
	AvgMs int64 `json:"avgMs"`
	P90Ms int64 `json:"p90Ms"`
}

type AlarmDeviceCount struct {
	DeviceID    string `json:"deviceId"`
	DeviceName  string `json:"deviceName"`
	FalseAlarms int64  `json:"falseAlarms"`
	Alarms      int64  `json:"alarms"`
}

// TopFalseAlarmDevices is how many devices a report lists by false alarms.
const TopFalseAlarmDevices = 10

// FireAlarmTypes lists the alarm types that need verification before closing.
func FireAlarmTypes() []string {
	out := make([]string, 0, len(fireAlarmTypes))
	for t := range fireAlarmTypes {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// SummarizeAlarms computes the statistics in memory; alarms are expected
// newest first, so a device is named after its latest alarm. Stores that can
// aggregate in the database return the same result.
func SummarizeAlarms(alarms []Alarm) AlarmDispositionStats {
	out := AlarmDispositionStats{Total: int64(len(alarms)), ByResult: map[string]int64{}, TopFalseAlarmDevices: []AlarmDeviceCount{}}
	acks, verifies := []int64{}, []int64{}
	devices := map[string]*AlarmDeviceCount{}
	for _, a := range alarms {
		d := devices[a.DeviceID]
		if d == nil {
			d = &AlarmDeviceCount{DeviceID: a.DeviceID, DeviceName: a.DeviceName}
			devices[a.DeviceID] = d
		}
		d.Alarms++
		if a.AckedAt > 0 && a.AckedAt >= a.FirstTriggeredAt {
			acks = append(acks, a.AckedAt-a.FirstTriggeredAt)
		}
		if a.RequiresVerification() {
			out.RequiringVerification++
		}
		if a.Disposition == nil {
			if a.RequiresVerification() {
				out.Unverified++
			}
			continue
		}
		out.Verified++
		out.ByResult[a.Disposition.Result]++
		if a.Disposition.Result == DispositionFalseAlarm {
			d.FalseAlarms++
		}
		if a.Disposition.VerifiedAt >= a.FirstTriggeredAt {
			verifies = append(verifies, a.Disposition.VerifiedAt-a.FirstTriggeredAt)
		}
	}
	for _, d := range devices {
		if d.FalseAlarms > 0 {
			out.TopFalseAlarmDevices = append(out.TopFalseAlarmDevices, *d)
		}
	}
	sort.Slice(out.TopFalseAlarmDevices, func(i, j int) bool {
		a, b := out.TopFalseAlarmDevices[i], out.TopFalseAlarmDevices[j]
		if a.FalseAlarms != b.FalseAlarms {
			return a.FalseAlarms > b.FalseAlarms
		}
		return a.DeviceID < b.DeviceID
	})
	if len(out.TopFalseAlarmDevices) > TopFalseAlarmDevices {
		out.TopFalseAlarmDevices = out.TopFalseAlarmDevices[:TopFalseAlarmDevices]
	}
	out.FalseAlarmRate = FalseAlarmRate(out.ByResult[DispositionFalseAlarm], out.Verified)
	out.Acknowledge, out.Verify = summarizeDurations(acks), summarizeDurations(verifies)
	return out
}

func FalseAlarmRate(falseAlarms, verified int64) float64 {
	if verified == 0 {
		return 0
	}
	return float64(falseAlarms) / float64(verified)
}

func summarizeDurations(values []int64) DurationStats {
	if len(values) == 0 {
		return DurationStats{}
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	var sum int64
	for _, v := range values {
		sum += v
	}
	index := (len(values)*9+9)/10 - 1 // nearest-rank 90th percentile
	return DurationStats{Count: int64(len(values)), AvgMs: sum / int64(len(values)), P90Ms: values[index]}
}

// ReportZone is the time zone of report days and months (China Standard
// Time, a fixed offset so no time zone database is needed).
var ReportZone = time.FixedZone("CST", 8*3600)

// AlarmBreakdown counts the alarms of a report period by level, type,
// status and day, and lists the devices with most alarms.
type AlarmBreakdown struct {
	ByLevel    map[string]int64   `json:"byLevel"`
	ByType     map[string]int64   `json:"byType"`
	ByStatus   map[string]int64   `json:"byStatus"`
	ByDay      []AlarmDayCount    `json:"byDay"`
	TopDevices []AlarmDeviceCount `json:"topDevices"`
}

type AlarmDayCount struct {
	Day   string `json:"day"`
	Count int64  `json:"count"`
}

// TopAlarmDevices is how many devices a report lists by alarm count.
const TopAlarmDevices = 10

// BreakdownAlarms computes the breakdown in memory; alarms are expected
// newest first. Stores that aggregate in the database return the same.
func BreakdownAlarms(alarms []Alarm) AlarmBreakdown {
	out := AlarmBreakdown{ByLevel: map[string]int64{}, ByType: map[string]int64{}, ByStatus: map[string]int64{}, ByDay: []AlarmDayCount{}, TopDevices: []AlarmDeviceCount{}}
	days := map[string]int64{}
	devices := map[string]*AlarmDeviceCount{}
	for _, a := range alarms {
		out.ByLevel[a.AlarmLevel]++
		out.ByType[a.AlarmType]++
		out.ByStatus[a.Status]++
		days[time.UnixMilli(a.LastTriggeredAt).In(ReportZone).Format(time.DateOnly)]++
		d := devices[a.DeviceID]
		if d == nil {
			d = &AlarmDeviceCount{DeviceID: a.DeviceID, DeviceName: a.DeviceName}
			devices[a.DeviceID] = d
		}
		d.Alarms++
	}
	for day, n := range days {
		out.ByDay = append(out.ByDay, AlarmDayCount{Day: day, Count: n})
	}
	sort.Slice(out.ByDay, func(i, j int) bool { return out.ByDay[i].Day < out.ByDay[j].Day })
	for _, d := range devices {
		out.TopDevices = append(out.TopDevices, *d)
	}
	sort.Slice(out.TopDevices, func(i, j int) bool {
		a, b := out.TopDevices[i], out.TopDevices[j]
		if a.Alarms != b.Alarms {
			return a.Alarms > b.Alarms
		}
		return a.DeviceID < b.DeviceID
	})
	if len(out.TopDevices) > TopAlarmDevices {
		out.TopDevices = out.TopDevices[:TopAlarmDevices]
	}
	return out
}

// AlarmMonthlyReport is the content of the monthly PDF report.
type AlarmMonthlyReport struct {
	TenantID    string                `json:"tenantId"`
	Month       string                `json:"month"`
	Start       int64                 `json:"start"`
	End         int64                 `json:"end"`
	GeneratedAt int64                 `json:"generatedAt"`
	Stats       AlarmDispositionStats `json:"stats"`
	Breakdown   AlarmBreakdown        `json:"breakdown"`
	// Highlights are the confirmed real fires and the fire alarms still
	// unverified, newest first; Omitted counts those not listed.
	Highlights []Alarm `json:"highlights"`
	Omitted    int     `json:"omitted"`
}
