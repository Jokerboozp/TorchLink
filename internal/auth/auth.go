package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"iot-platform/internal/ports"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const claimsContextKey contextKey = "iot-auth-claims"

const (
	HarnessAudience           = "iot-platform-mcp"
	ScopeQueryDeviceLatest    = "mcp:tool:query_device_latest"
	ScopeQuerySystemOverview  = "mcp:tool:query_system_overview"
	ScopeQueryAlarmList       = "mcp:tool:query_alarm_list"
	ScopeQueryPropertyHistory = "mcp:tool:query_property_history"
	ScopeQuerySimilarAlarms   = "mcp:tool:query_similar_alarms"
	ScopeQueryKnowledgeBase   = "mcp:tool:query_knowledge_base"
	ScopeCreateRuleDraft      = "mcp:tool:create_rule_draft"
)

func HarnessReadScopes() []string {
	return []string{ScopeQuerySystemOverview, ScopeQueryDeviceLatest, ScopeQueryAlarmList, ScopeQueryPropertyHistory, ScopeQuerySimilarAlarms, ScopeQueryKnowledgeBase, ScopeCreateRuleDraft}
}

func ContextWithClaims(ctx context.Context, claims Claims) context.Context {
	return context.WithValue(ctx, claimsContextKey, claims)
}

func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(Claims)
	return claims, ok
}

type Claims struct {
	ManagedUser    bool            `json:"managedUser,omitempty"`
	Permissions    []string        `json:"permissions,omitempty"`
	Username       string          `json:"username"`
	TenantID       string          `json:"tenantId"`
	Role           string          `json:"role"`
	Scopes         []string        `json:"scopes,omitempty"`
	ACL            []ACLRule       `json:"acl,omitempty"`
	TokenUse       string          `json:"tokenUse,omitempty"`
	SessionVersion int64           `json:"sessionVersion,omitempty"`
	RunID          string          `json:"runId,omitempty"`
	Knowledge      *KnowledgeScope `json:"knowledge,omitempty"`
	// Workflow marks a Harness business run (alarm analysis, inspection, ...);
	// the MCP endpoint then checks that feature's permission instead of chat.
	Workflow string `json:"workflow,omitempty"`
	jwt.RegisteredClaims
}
type KnowledgeScope struct {
	WorkflowID string  `json:"workflowId,omitempty"`
	TopK       int     `json:"topK,omitempty"`
	MinScore   float64 `json:"minScore,omitempty"`
}
type ACLRule struct {
	Permission string `json:"permission"`
	Action     string `json:"action"`
	Topic      string `json:"topic"`
}
type Manager struct {
	secret []byte
	issuer string
}

func New(secret string) *Manager { return &Manager{[]byte(secret), "iot-platform"} }
func (m *Manager) Issue(user, tenant, role string, scopes []string, ttl time.Duration) (string, error) {
	acl := make([]ACLRule, 0, len(scopes))
	for _, scope := range scopes {
		acl = append(acl, ACLRule{Permission: "allow", Action: "subscribe", Topic: scope})
	}
	return m.IssueWithACL(user, tenant, role, scopes, acl, ttl)
}
func (m *Manager) IssueWithACL(user, tenant, role string, scopes []string, acl []ACLRule, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{Username: user, TenantID: tenant, Role: role, Scopes: scopes, ACL: acl, RegisteredClaims: jwt.RegisteredClaims{Issuer: m.issuer, Subject: user, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(ttl)), ID: fmt.Sprintf("%d", now.UnixNano())}}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

func (m *Manager) IssueBrowserMQTT(user, tenant string, scopes []string, ttl time.Duration) (string, error) {
	now := time.Now()
	acl := []ACLRule{}
	for _, scope := range scopes {
		acl = append(acl, ACLRule{Permission: "allow", Action: "subscribe", Topic: scope})
	}
	c := Claims{Username: user, TenantID: tenant, Role: "viewer", TokenUse: "browser-mqtt", Scopes: scopes, ACL: acl, RegisteredClaims: jwt.RegisteredClaims{Issuer: m.issuer, Subject: user, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(ttl))}}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(m.secret)
}

func (m *Manager) IssueHarness(user, tenant, runID string, scopes []string, ttl time.Duration) (string, error) {
	return m.IssueHarnessWithKnowledge(user, tenant, runID, scopes, nil, ttl)
}

func (m *Manager) IssueUser(user, tenant string, version int64, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{Username: user, TenantID: tenant, Role: "operator", TokenUse: "user", SessionVersion: version, RegisteredClaims: jwt.RegisteredClaims{Issuer: m.issuer, Subject: user, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(ttl))}}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}
func (m *Manager) IssueHarnessWithKnowledge(user, tenant, runID string, scopes []string, knowledge *KnowledgeScope, ttl time.Duration) (string, error) {
	return m.issueHarness(Claims{Username: user, TenantID: tenant}, runID, scopes, knowledge, ttl)
}

// IssueHarnessForIdentity preserves managed account identity across the HTTP bridge.
func (m *Manager) IssueHarnessForIdentity(parent Claims, runID string, scopes []string, knowledge *KnowledgeScope, ttl time.Duration) (string, error) {
	return m.issueHarness(parent, runID, scopes, knowledge, ttl)
}

// IssueBusinessRunToken implements ports.HarnessTokenIssuer.
func (m *Manager) IssueBusinessRunToken(tenantID string, identity ports.AIRunIdentity, runID, workflowID string, scopes []string, knowledge *ports.AIKnowledgeRunScope, ttl time.Duration) (string, error) {
	if strings.TrimSpace(workflowID) == "" {
		return "", errors.New("business workflow is required")
	}
	parent := Claims{Username: identity.Username, TenantID: tenantID, SessionVersion: identity.SessionVersion, Workflow: workflowID}
	if identity.ManagedUser {
		parent.TokenUse = "user"
	}
	var scope *KnowledgeScope
	if knowledge != nil {
		scope = &KnowledgeScope{WorkflowID: knowledge.WorkflowID, TopK: knowledge.TopK, MinScore: knowledge.MinScore}
	}
	return m.issueHarness(parent, runID, scopes, scope, ttl)
}

func (m *Manager) issueHarness(parent Claims, runID string, scopes []string, knowledge *KnowledgeScope, ttl time.Duration) (string, error) {
	user, tenant := parent.Username, parent.TenantID
	if user == "" || tenant == "" || runID == "" || ttl <= 0 {
		return "", errors.New("user, tenant, runId and positive ttl are required")
	}
	now := time.Now()
	claims := Claims{
		Username:       user,
		TenantID:       tenant,
		Role:           "viewer",
		Scopes:         append([]string(nil), scopes...),
		TokenUse:       "harness",
		RunID:          runID,
		Knowledge:      knowledge,
		Workflow:       parent.Workflow,
		ManagedUser:    parent.TokenUse == "user",
		SessionVersion: parent.SessionVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   "harness:" + user,
			Audience:  jwt.ClaimStrings{HarnessAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			ID:        fmt.Sprintf("%d", now.UnixNano()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}
func (m *Manager) Parse(token string) (Claims, error) {
	var claims Claims
	t, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method %s", t.Method.Alg())
		}
		return m.secret, nil
	}, jwt.WithIssuer(m.issuer), jwt.WithExpirationRequired())
	if err != nil || !t.Valid {
		return claims, errors.New("invalid or expired token")
	}
	return claims, nil
}
func Bearer(v string) string {
	parts := strings.SplitN(v, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}
func (c Claims) Can(role string) bool {
	if c.Role == "admin" {
		return true
	}
	return c.Role == role
}

func (c Claims) HasScope(scope string) bool {
	for _, candidate := range c.Scopes {
		if candidate == scope {
			return true
		}
	}
	return false
}

func (c Claims) HasAudience(audience string) bool {
	for _, candidate := range c.Audience {
		if candidate == audience {
			return true
		}
	}
	return false
}
