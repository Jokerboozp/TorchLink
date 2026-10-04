// Package prometheus embeds the platform alert rules, so the cluster
// renderer ships the same rules as the single-node deployment.
package prometheus

import _ "embed"

// Alerts is alerts.yml, the Prometheus rule file of the platform.
//
//go:embed alerts.yml
var Alerts string
