package model

type ONVIFCandidate struct {
	EndpointID string   `json:"endpointId"`
	SourceIP   string   `json:"sourceIp"`
	XAddrs     []string `json:"xAddrs"`
	Scopes     []string `json:"scopes"`
	ObservedAt int64    `json:"observedAt"`
}
type ONVIFDiscovery struct {
	Items         []ONVIFCandidate `json:"items"`
	Discarded     int              `json:"discarded"`
	Truncated     bool             `json:"truncated"`
	Authenticated bool             `json:"authenticated"`
}
