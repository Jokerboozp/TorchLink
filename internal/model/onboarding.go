package model

// OnboardingBundle is persisted atomically; existing products/releases are
// never replaced by an onboarding request.
type OnboardingBundle struct {
	Product      *Product
	ReuseProfile bool
	Device       ManagedDevice
	Profile      *DeviceAccessProfile
	Release      *ProtocolRelease
	PointTable   *PointTableRelease
	Binding      *ProductProtocolBinding
}
