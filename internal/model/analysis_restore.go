package model

// Restored object addresses are an overlay. Immutable attachment/config bodies
// retain their original bucket/key and hash; only this trusted mapping changes.
type AnalysisRestoredObjectLocation struct {
	TenantID     string `json:"tenantId"`
	RestoreID    string `json:"restoreId"`
	SourceBucket string `json:"sourceBucket"`
	SourceKey    string `json:"sourceKey"`
	Bucket       string `json:"bucket"`
	Key          string `json:"key"`
	SHA256       string `json:"sha256"`
	Size         int64  `json:"size"`
}
