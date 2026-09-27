package deploycheck

import (
	"fmt"
	"sort"
	"strings"
)

// RolePool is one process role's PostgreSQL pool in a cluster plan.
type RolePool struct {
	Role      string `json:"role" yaml:"role"`
	Instances int    `json:"instances" yaml:"instances"`
	PoolMax   int    `json:"poolMax" yaml:"poolMax"`
}

// ConnectionBudget is the per-role PostgreSQL connection arithmetic of a
// deployment (plan §4.3): steady state plus the largest extra instance a
// rolling update starts before stopping an old one.
type ConnectionBudget struct {
	Roles     []RolePool `json:"roles"`
	Steady    int        `json:"steady"`
	Surge     int        `json:"surge"`
	Reserved  int        `json:"reserved"`
	Limit     int        `json:"limit"`
	Headroom  int        `json:"headroom"`
	SurgeRole string     `json:"surgeRole"`
}

// AssessConnectionBudget sums every role's pools. Rolling one role at a time
// adds at most one extra instance of the largest pool; rolling several
// roles in parallel needs its own recalculation.
func AssessConnectionBudget(roles []RolePool, reserved, limit int) (ConnectionBudget, CapacityCheck) {
	b := ConnectionBudget{Roles: roles, Reserved: reserved, Limit: limit}
	sort.Slice(b.Roles, func(i, j int) bool { return b.Roles[i].Role < b.Roles[j].Role })
	var parts []string
	for _, r := range b.Roles {
		b.Steady += r.Instances * r.PoolMax
		if r.Instances > 0 && r.PoolMax > b.Surge {
			b.Surge, b.SurgeRole = r.PoolMax, r.Role
		}
		parts = append(parts, fmt.Sprintf("%s %d×%d", r.Role, r.Instances, r.PoolMax))
	}
	b.Headroom = limit - b.Steady - b.Surge - reserved
	check := CapacityCheck{Name: "PostgreSQL 角色连接预算"}
	switch {
	case limit <= 0:
		check.Status, check.Detail = "unverified", "未提供 PostgreSQL max_connections"
	case b.Headroom < 0:
		check.Status = "blocked"
	default:
		check.Status = "passed"
	}
	if check.Detail == "" {
		check.Detail = fmt.Sprintf("%s = 常态 %d，滚动升级额外 %d（%s），预留 %d，上限 %d，余量 %d；只证明连接数不超限，不代表锁、WAL 或 CPU 不先成为瓶颈", strings.Join(parts, " + "), b.Steady, b.Surge, b.SurgeRole, reserved, limit, b.Headroom)
	}
	return b, check
}
