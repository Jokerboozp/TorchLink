// Package toolaccounts provides the same initialization scripts to Compose
// deployments and the standalone cluster renderer.
package toolaccounts

import _ "embed"

//go:embed postgres.sh
var Postgres []byte

//go:embed clickhouse.sh
var ClickHouse []byte
