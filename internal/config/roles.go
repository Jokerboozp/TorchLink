package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Process roles. combined runs everything in one process; the others split
// responsibilities so each can be scaled and budgeted on its own.
const (
	RoleCombined  = "combined"
	RoleAPI       = "api"
	RoleGateway   = "gateway"
	RoleParser    = "parser"
	RoleProcessor = "processor"
	RoleJobs      = "jobs"
)

// Components a process can run. A role maps to a fixed set; see Runs.
const (
	ComponentAccess     = "access"     // device transports, MQTT ingress, raw archive
	ComponentManagement = "management" // management HTTP API, ops center, video control
	ComponentParser     = "parser"     // raw → standard message
	ComponentProcessor  = "processor"  // device business stream, rules, alarms, state, outbox
	ComponentJobs       = "jobs"       // periodic scans, retries and notifications
	ComponentAIRuntime  = "ai-runtime" // Harness/Provider clients of the AI features (management)
)

var workerRoles = []string{RoleParser, RoleProcessor, RoleJobs}

var instanceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// Runs reports whether this process runs component. The api role keeps its
// historical embedded workers unless IOT_API_EMBEDDED_WORKERS=false, which
// must be deployed together with the dedicated worker roles.
func (c Config) Runs(component string) bool {
	role := c.ProcessRole
	if role == "" {
		role = RoleCombined
	}
	embedded := role == RoleCombined || (role == RoleAPI && c.APIEmbeddedWorkers)
	switch component {
	case ComponentAccess:
		return role == RoleCombined || role == RoleGateway
	case ComponentManagement:
		return role == RoleCombined || role == RoleAPI
	case ComponentAIRuntime:
		return role == RoleCombined || role == RoleAPI
	case ComponentParser:
		return embedded || role == RoleParser
	case ComponentProcessor:
		return embedded || role == RoleProcessor
	case ComponentJobs:
		return embedded || role == RoleJobs
	}
	return false
}

// IsWorkerRole reports a role that serves no business HTTP routes.
func (c Config) IsWorkerRole() bool {
	for _, r := range workerRoles {
		if c.ProcessRole == r {
			return true
		}
	}
	return false
}

// instanceID is IOT_INSTANCE_ID or a sanitized host name. explicit reports
// whether it was configured; only an explicit ID changes durable paths, so
// existing single-instance data directories keep working.
func instanceID() (id string, explicit bool) {
	if v := strings.TrimSpace(os.Getenv("IOT_INSTANCE_ID")); v != "" {
		return v, true
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "local", false
	}
	// Hostnames may contain characters outside the identifier set.
	host = strings.Trim(regexp.MustCompile(`[^A-Za-z0-9_.-]+`).ReplaceAllString(host, "-"), "-.")
	if len(host) > 64 {
		host = host[:64]
	}
	if host == "" {
		host = "local"
	}
	return host, false
}

func (c Config) validateRole() error {
	switch c.ProcessRole {
	case "", RoleCombined:
	case RoleAPI, RoleGateway, RoleParser, RoleProcessor, RoleJobs:
		if c.PostgresDSN == "" || len(c.KafkaBrokers) == 0 {
			return fmt.Errorf("split process roles require shared IOT_POSTGRES_DSN and IOT_KAFKA_BROKERS")
		}
		if c.ProcessRole == RoleAPI && c.AccessGatewayURL == "" {
			return fmt.Errorf("api role requires IOT_ACCESS_GATEWAY_URL")
		}
	case "ai":
		// The former automatic alarm analysis consumer.
		return fmt.Errorf("IOT_PROCESS_ROLE=ai has been removed: alarm analysis runs on request in the api role; delete this process")
	default:
		return fmt.Errorf("IOT_PROCESS_ROLE must be combined, api, gateway, parser, processor or jobs")
	}
	if c.InstanceID != "" && !instanceIDPattern.MatchString(c.InstanceID) {
		return fmt.Errorf("IOT_INSTANCE_ID must be 1-64 letters, digits, '.', '_' or '-'")
	}
	return nil
}
