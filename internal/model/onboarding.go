package model

// OnboardingBundle is persisted atomically; existing products/releases are
// never replaced by an onboarding request.
type OnboardingBundle struct {
	Prepared     *PreparedEnrollment
	Product      *Product
	ReuseProfile bool
	Device       ManagedDevice
	Profile      *DeviceAccessProfile
	Release      *ProtocolRelease
	PointTable   *PointTableRelease
	Binding      *ProductProtocolBinding
}

// PreparedEnrollment pins both the acceptance record and the actual template
// configuration checked by the onboarding planner until the device commits.
type PreparedEnrollment struct {
	Product        Product
	Profiles       []DeviceAccessProfile
	RecordRevision int64
}
