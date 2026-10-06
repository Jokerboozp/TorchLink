package model

// ErrBindingChanged reports that a template's protocol binding changed after it was read.
var ErrBindingChanged error = Conflict("产品协议绑定已被其他操作修改，请刷新后重试")

// ProtocolSwitch changes the protocol version that parses a template's data. The
// binding is the single source of that version; the template's protocol
// reference and its compatibility package are written with it atomically.
type ProtocolSwitch struct {
	Product Product
	Package ProtocolPackage
	Binding ProductProtocolBinding
	// Expected is the binding read before the switch; nil means there was none.
	Expected *ProductProtocolBinding
	// RequireUnused prevents a direct initial bind from racing with enrollment.
	// Prepared and verified configuration switches do not set this flag.
	RequireUnused bool
	// Preparation changes the complete template configuration and its durable
	// revision together. Nil retains the protocol-only internal operation.
	Preparation *TemplateSwitch `json:"-"`
}

type TemplateSwitch struct {
	// CreateProduct creates an isolated template within this same atomic switch.
	// It rejects an existing identity instead of replacing an existing template.
	CreateProduct    bool
	ExpectedProduct  Product
	ExpectedProfiles []DeviceAccessProfile
	Profiles         []DeviceAccessProfile
	Record           OnboardingRecord
	ExpectedRevision int64
}
