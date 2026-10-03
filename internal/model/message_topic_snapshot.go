package model

// MessageTopicDeviceRecord joins a device with its optional runtime state in
// one repository read so a bounded topic query does not depend on UI paging.
type MessageTopicDeviceRecord struct {
	Device ManagedDevice
	State  *DeviceState
}
