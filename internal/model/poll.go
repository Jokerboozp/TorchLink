package model

type PollPoint struct {
	Identifier string  `json:"identifier"`
	Address    string  `json:"address"`
	Scale      float64 `json:"scale,omitempty"`
	Offset     float64 `json:"offset,omitempty"`
}
type PollValue struct {
	Address   string `json:"address"`
	Value     any    `json:"value"`
	Quality   string `json:"quality"`
	Timestamp int64  `json:"timestamp,omitempty"`
}
type PollResponse struct {
	Transport string      `json:"transport"`
	Values    []PollValue `json:"values"`
	Response  any         `json:"response"`
}
