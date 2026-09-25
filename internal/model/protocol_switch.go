package model

import "errors"

// ErrBindingChanged reports that a template's protocol binding changed after it was read.
var ErrBindingChanged = errors.New("产品协议绑定已被其他操作修改，请刷新后重试")

// ProtocolSwitch changes the protocol version that parses a template's data. The
// binding is the single source of that version; the template's protocol
// reference and its compatibility package are written with it atomically.
type ProtocolSwitch struct {
	Product Product
	Package ProtocolPackage
	Binding ProductProtocolBinding
	// Expected is the binding read before the switch; nil means there was none.
	Expected *ProductProtocolBinding
}
