// Package logkey names the structured log attributes shared across the
// platform, so one tenant's or device's records can be found with a single
// field name in the log center.
package logkey

const (
	// RequestID is the HTTP request ID (also returned as X-Request-ID).
	RequestID = "requestId"
	// Tenant is the tenant ID.
	Tenant = "tenantId"
	// User is the authenticated username.
	User = "user"
	// Device is the device ID.
	Device = "deviceId"
)
