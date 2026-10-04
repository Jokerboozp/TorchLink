package model

import "strings"

// Chinese names of alarm codes for messages, exports and reports; the
// console keeps the same names in iot_front/src/labels.js.
var (
	alarmLevelNames  = map[string]string{"CRITICAL": "紧急", "HIGH": "高", "MEDIUM": "中", "LOW": "低", "INFO": "提示"}
	alarmTypeNames   = map[string]string{"FIRE_RISK": "火灾风险", "FIRE": "火灾告警", "SMOKE_DETECTED": "检测到烟雾", "FLAME_DETECTED": "检测到火焰", "HIGH_TEMPERATURE": "温度过高", "DEVICE_FAULT": "设备故障", "DEVICE_OFFLINE": "设备离线", "WATER_PRESSURE_LOW": "水压过低", "WATER_LEVEL_ABNORMAL": "水位异常", "ELECTRICAL_FIRE": "电气火灾", "GAS_LEAK": "可燃气体泄漏", "MANUAL_ALARM": "手动报警"}
	alarmStatusNames = map[string]string{"ACTIVE": "活动", "ACKED": "已确认", "RECOVERED": "已恢复", "CLOSED": "已关闭", "SUPPRESSED": "已抑制"}
	dispositionNames = map[string]string{DispositionRealFire: "真实火警", DispositionFalseAlarm: "误报", DispositionTest: "测试", DispositionMaintenance: "检修", DispositionFault: "设备故障"}
)

func labelOf(names map[string]string, v string) string {
	if name := names[strings.ToUpper(strings.TrimSpace(v))]; name != "" {
		return name
	}
	return v
}

func AlarmLevelName(v string) string  { return labelOf(alarmLevelNames, v) }
func AlarmTypeName(v string) string   { return labelOf(alarmTypeNames, v) }
func AlarmStatusName(v string) string { return labelOf(alarmStatusNames, v) }
func DispositionName(v string) string { return labelOf(dispositionNames, v) }
