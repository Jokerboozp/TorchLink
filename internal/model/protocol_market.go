package model

import (
	"errors"
	"time"
)

var ErrMarketConflict = errors.New("protocol market entry changed or already exists")
var ErrMarketLimit = errors.New("protocol market supports at most 1000 versions per organization")
var ErrMarketReview = errors.New("market review requires another user and a valid state transition")

type ProtocolMarketReview struct {
	Actor    string `json:"actor"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
	At       int64  `json:"at"`
}
type ProtocolMarketEntry struct {
	TenantID      string                 `json:"tenantId"`
	ProtocolID    string                 `json:"protocolId"`
	Version       string                 `json:"version"`
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	License       string                 `json:"license"`
	Tags          []string               `json:"tags"`
	SourceSHA256  string                 `json:"sourceSha256"`
	PackageSHA256 string                 `json:"packageSha256"`
	SourceSize    int64                  `json:"sourceSize"`
	SubmittedBy   string                 `json:"submittedBy"`
	SubmittedAt   int64                  `json:"submittedAt"`
	Generation    int64                  `json:"generation"`
	Status        string                 `json:"status"`
	Reviews       []ProtocolMarketReview `json:"reviews"`
}

func (v ProtocolMarketEntry) Review(actor, decision, reason string, expected int64) (ProtocolMarketEntry, error) {
	if v.Generation != expected {
		return v, ErrMarketConflict
	}
	if actor == "" || len(reason) > 4096 || reason == "" {
		return v, ErrMarketReview
	}
	if decision == "WITHDRAWN" {
		if v.Status != "APPROVED" && v.Status != "SUBMITTED" {
			return v, ErrMarketReview
		}
	} else if (decision != "APPROVED" && decision != "REJECTED") || v.Status != "SUBMITTED" || v.SubmittedBy == actor {
		return v, ErrMarketReview
	}
	v.Generation++
	v.Status = decision
	v.Reviews = append(append([]ProtocolMarketReview{}, v.Reviews...), ProtocolMarketReview{Actor: actor, Decision: decision, Reason: reason, At: time.Now().UnixMilli()})
	return v, nil
}
