package auth /* 声明 auth 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"errors"  /* 执行当前语句并推进处理流程。 */
	"fmt"     /* 执行当前语句并推进处理流程。 */
	"strings" /* 执行当前语句并推进处理流程。 */
	"time"    /* 执行当前语句并推进处理流程。 */

	"github.com/golang-jwt/jwt/v5" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type contextKey string /* 定义 contextKey 类型。 */

const claimsContextKey contextKey = "iot-auth-claims" /* 声明 claimsContextKey。 */

const ( /* 执行当前语句并推进处理流程。 */
	HarnessAudience           = "iot-platform-mcp"                /* 更新 HarnessAudience 的值。 */
	ScopeQueryDeviceLatest    = "mcp:tool:query_device_latest"    /* 更新 ScopeQueryDeviceLatest 的值。 */
	ScopeQuerySystemOverview  = "mcp:tool:query_system_overview"  /* 更新 ScopeQuerySystemOverview 的值。 */
	ScopeQueryAlarmList       = "mcp:tool:query_alarm_list"       /* 更新 ScopeQueryAlarmList 的值。 */
	ScopeQueryPropertyHistory = "mcp:tool:query_property_history" /* 更新 ScopeQueryPropertyHistory 的值。 */
	ScopeQuerySimilarAlarms   = "mcp:tool:query_similar_alarms"   /* 更新 ScopeQuerySimilarAlarms 的值。 */
	ScopeQueryKnowledgeBase   = "mcp:tool:query_knowledge_base"   /* 更新 ScopeQueryKnowledgeBase 的值。 */
	ScopeCreateRuleDraft      = "mcp:tool:create_rule_draft"      /* 更新 ScopeCreateRuleDraft 的值。 */
) /* 结束当前表达式或代码块。 */

func HarnessReadScopes() []string { /* 定义 HarnessReadScopes 函数。 */
	return []string{ScopeQuerySystemOverview, ScopeQueryDeviceLatest, ScopeQueryAlarmList, ScopeQueryPropertyHistory, ScopeQuerySimilarAlarms, ScopeQueryKnowledgeBase, ScopeCreateRuleDraft} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func ContextWithClaims(ctx context.Context, claims Claims) context.Context { /* 定义 ContextWithClaims 函数。 */
	return context.WithValue(ctx, claimsContextKey, claims) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func ClaimsFromContext(ctx context.Context) (Claims, bool) { /* 定义 ClaimsFromContext 函数。 */
	claims, ok := ctx.Value(claimsContextKey).(Claims) /* 更新 ok 的值。 */
	return claims, ok                                  /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type Claims struct {
	ManagedUser          bool            `json:"managedUser,omitempty"`
	Permissions          []string        `json:"permissions,omitempty"`    /* 定义 Claims 类型。 */
	Username             string          `json:"username"`                 /* 执行当前语句并推进处理流程。 */
	TenantID             string          `json:"tenantId"`                 /* 执行当前语句并推进处理流程。 */
	Role                 string          `json:"role"`                     /* 执行当前语句并推进处理流程。 */
	Scopes               []string        `json:"scopes,omitempty"`         /* 执行当前语句并推进处理流程。 */
	ACL                  []ACLRule       `json:"acl,omitempty"`            /* 执行当前语句并推进处理流程。 */
	TokenUse             string          `json:"tokenUse,omitempty"`       /* 执行当前语句并推进处理流程。 */
	SessionVersion       int64           `json:"sessionVersion,omitempty"` /* 执行当前语句并推进处理流程。 */
	RunID                string          `json:"runId,omitempty"`          /* 执行当前语句并推进处理流程。 */
	Knowledge            *KnowledgeScope `json:"knowledge,omitempty"`      /* 执行当前语句并推进处理流程。 */
	jwt.RegisteredClaims                 /* 执行当前语句并推进处理流程。 */
}                            /* 结束当前表达式或代码块。 */
type KnowledgeScope struct { /* 定义 KnowledgeScope 类型。 */
	WorkflowID string  `json:"workflowId,omitempty"` /* 执行当前语句并推进处理流程。 */
	TopK       int     `json:"topK,omitempty"`       /* 执行当前语句并推进处理流程。 */
	MinScore   float64 `json:"minScore,omitempty"`   /* 执行当前语句并推进处理流程。 */
}                     /* 结束当前表达式或代码块。 */
type ACLRule struct { /* 定义 ACLRule 类型。 */
	Permission string `json:"permission"` /* 执行当前语句并推进处理流程。 */
	Action     string `json:"action"`     /* 执行当前语句并推进处理流程。 */
	Topic      string `json:"topic"`      /* 执行当前语句并推进处理流程。 */
}                     /* 结束当前表达式或代码块。 */
type Manager struct { /* 定义 Manager 类型。 */
	secret []byte /* 执行当前语句并推进处理流程。 */
	issuer string /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func New(secret string) *Manager { return &Manager{[]byte(secret), "iot-platform"} } /* 定义 New 函数。 */
func (m *Manager) Issue(user, tenant, role string, scopes []string, ttl time.Duration) (string, error) { /* 定义 Issue 函数。 */
	acl := make([]ACLRule, 0, len(scopes)) /* 更新 acl 的值。 */
	for _, scope := range scopes {         /* 循环处理当前数据。 */
		acl = append(acl, ACLRule{Permission: "allow", Action: "subscribe", Topic: scope}) /* 更新 acl 的值。 */
	} /* 结束当前表达式或代码块。 */
	return m.IssueWithACL(user, tenant, role, scopes, acl, ttl) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (m *Manager) IssueWithACL(user, tenant, role string, scopes []string, acl []ACLRule, ttl time.Duration) (string, error) { /* 定义 IssueWithACL 函数。 */
	now := time.Now()                                                                                                                                                                                                                                                                        /* 更新 now 的值。 */
	claims := Claims{Username: user, TenantID: tenant, Role: role, Scopes: scopes, ACL: acl, RegisteredClaims: jwt.RegisteredClaims{Issuer: m.issuer, Subject: user, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(ttl)), ID: fmt.Sprintf("%d", now.UnixNano())}} /* 更新 claims 的值。 */
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)                                                                                                                                                                                                          /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (m *Manager) IssueBrowserMQTT(user, tenant string, scopes []string, ttl time.Duration) (string, error) { /* 定义 IssueBrowserMQTT 函数。 */
	now := time.Now()              /* 更新 now 的值。 */
	acl := []ACLRule{}             /* 更新 acl 的值。 */
	for _, scope := range scopes { /* 循环处理当前数据。 */
		acl = append(acl, ACLRule{Permission: "allow", Action: "subscribe", Topic: scope}) /* 更新 acl 的值。 */
	} /* 结束当前表达式或代码块。 */
	c := Claims{Username: user, TenantID: tenant, Role: "viewer", TokenUse: "browser-mqtt", Scopes: scopes, ACL: acl, RegisteredClaims: jwt.RegisteredClaims{Issuer: m.issuer, Subject: user, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(ttl))}} /* 更新 c 的值。 */
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(m.secret)                                                                                                                                                                                                 /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (m *Manager) IssueHarness(user, tenant, runID string, scopes []string, ttl time.Duration) (string, error) { /* 定义 IssueHarness 函数。 */
	return m.IssueHarnessWithKnowledge(user, tenant, runID, scopes, nil, ttl) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (m *Manager) IssueUser(user, tenant string, version int64, ttl time.Duration) (string, error) { /* 定义 IssueUser 函数。 */
	now := time.Now()                                                                                                                                                                                                                                                        /* 更新 now 的值。 */
	claims := Claims{Username: user, TenantID: tenant, Role: "operator", TokenUse: "user", SessionVersion: version, RegisteredClaims: jwt.RegisteredClaims{Issuer: m.issuer, Subject: user, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(ttl))}} /* 更新 claims 的值。 */
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)                                                                                                                                                                                          /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (m *Manager) IssueHarnessWithKnowledge(user, tenant, runID string, scopes []string, knowledge *KnowledgeScope, ttl time.Duration) (string, error) { /* 定义 IssueHarnessWithKnowledge 函数。 */
	return m.issueHarness(Claims{Username: user, TenantID: tenant}, runID, scopes, knowledge, ttl)
}

// IssueHarnessForIdentity preserves managed account identity across the HTTP bridge.
func (m *Manager) IssueHarnessForIdentity(parent Claims, runID string, scopes []string, knowledge *KnowledgeScope, ttl time.Duration) (string, error) {
	return m.issueHarness(parent, runID, scopes, knowledge, ttl)
}

func (m *Manager) issueHarness(parent Claims, runID string, scopes []string, knowledge *KnowledgeScope, ttl time.Duration) (string, error) {
	user, tenant := parent.Username, parent.TenantID
	if user == "" || tenant == "" || runID == "" || ttl <= 0 { /* 判断条件并选择处理分支。 */
		return "", errors.New("user, tenant, runId and positive ttl are required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	now := time.Now() /* 更新 now 的值。 */
	claims := Claims{ /* 更新 claims 的值。 */
		Username:       user,                             /* 执行当前语句并推进处理流程。 */
		TenantID:       tenant,                           /* 执行当前语句并推进处理流程。 */
		Role:           "viewer",                         /* 执行当前语句并推进处理流程。 */
		Scopes:         append([]string(nil), scopes...), /* 执行当前语句并推进处理流程。 */
		TokenUse:       "harness",                        /* 执行当前语句并推进处理流程。 */
		RunID:          runID,                            /* 执行当前语句并推进处理流程。 */
		Knowledge:      knowledge,
		ManagedUser:    parent.TokenUse == "user",
		SessionVersion: parent.SessionVersion, /* 执行当前语句并推进处理流程。 */
		RegisteredClaims: jwt.RegisteredClaims{ /* 执行当前语句并推进处理流程。 */
			Issuer:    m.issuer,                          /* 执行当前语句并推进处理流程。 */
			Subject:   "harness:" + user,                 /* 执行当前语句并推进处理流程。 */
			Audience:  jwt.ClaimStrings{HarnessAudience}, /* 执行当前语句并推进处理流程。 */
			IssuedAt:  jwt.NewNumericDate(now),           /* 执行当前语句并推进处理流程。 */
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),  /* 执行当前语句并推进处理流程。 */
			ID:        fmt.Sprintf("%d", now.UnixNano()), /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (m *Manager) Parse(token string) (Claims, error) { /* 定义 Parse 函数。 */
	var claims Claims                                                               /* 声明 claims。 */
	t, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) { /* 更新 err 的值。 */
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("unexpected signing method %s", t.Method.Alg()) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return m.secret, nil /* 返回当前处理结果。 */
	}, jwt.WithIssuer(m.issuer), jwt.WithExpirationRequired()) /* 结束当前表达式或代码块。 */
	if err != nil || !t.Valid { /* 判断条件并选择处理分支。 */
		return claims, errors.New("invalid or expired token") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return claims, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func Bearer(v string) string { /* 定义 Bearer 函数。 */
	parts := strings.SplitN(v, " ", 2)                            /* 更新 parts 的值。 */
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") { /* 判断条件并选择处理分支。 */
		return parts[1] /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return "" /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (c Claims) Can(role string) bool { /* 定义 Can 函数。 */
	if c.Role == "admin" { /* 判断条件并选择处理分支。 */
		return true /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return c.Role == role /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (c Claims) HasScope(scope string) bool { /* 定义 HasScope 函数。 */
	for _, candidate := range c.Scopes { /* 循环处理当前数据。 */
		if candidate == scope { /* 判断条件并选择处理分支。 */
			return true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (c Claims) HasAudience(audience string) bool { /* 定义 HasAudience 函数。 */
	for _, candidate := range c.Audience { /* 循环处理当前数据。 */
		if candidate == audience { /* 判断条件并选择处理分支。 */
			return true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
