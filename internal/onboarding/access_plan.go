package onboarding

import (
	"context"
	"errors"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/protocolworker"
)

// StandardPackageID is the product protocol reference of the built-in HTTP/MQTT protocol.
const StandardPackageID = parser.StandardProtocolID + "@1.0.0"

// Access modes decide what the wizard asks for and how devices reach the platform.
const (
	ModeStandard    = "standard"    // platform credentials and standard HTTP/MQTT envelopes
	ModeManaged     = "managed"     // platform credentials and raw payloads through managed HTTP ingest
	ModeListener    = "listener"    // Go protocol over a TCP/UDP listener; TCP may also be dialled
	ModePoll        = "poll"        // Modbus point table polled by the platform
	ModeUnsupported = "unsupported" // the template protocol cannot onboard devices directly
)

type ProtocolSummary struct {
	ID            string   `json:"id"`
	Version       string   `json:"version"`
	ParserType    string   `json:"parserType"`
	Transport     string   `json:"transport"`
	PayloadFormat string   `json:"payloadFormat"`
	Status        string   `json:"status"`
	Capabilities  []string `json:"capabilities,omitempty"`
	Published     bool     `json:"published"`
}

// AccessPlan is derived from a template and its bound protocol version.
type AccessPlan struct {
	Mode      string          `json:"mode"`
	Connector string          `json:"connector"`
	Networks  []string        `json:"networks,omitempty"`
	Dial      bool            `json:"dial"`
	Reason    string          `json:"reason,omitempty"`
	Protocol  ProtocolSummary `json:"protocol"`
	// Release is the resolved protocol version; nil only for the standard
	// protocol before its tenant release exists.
	Release *model.ProtocolRelease `json:"-"`
}

func standardRelease(tenant string, now int64) model.ProtocolRelease {
	return model.ProtocolRelease{TenantID: tenant, ProtocolID: parser.StandardProtocolID, Version: "1.0.0", Transport: "MQTT_HTTP", PayloadFormat: "json", ParserType: parser.StandardParserName, Status: "PUBLISHED", CreatedAt: now, PublishedAt: now}
}

func splitPackageID(id string) (string, string, bool) {
	index := strings.LastIndex(id, "@")
	if index <= 0 || index == len(id)-1 {
		return "", "", false
	}
	return id[:index], id[index+1:], true
}

// productRelease resolves the protocol version that parses a template's data:
// the explicit binding first, then the template's protocol reference.
func (s *Service) productRelease(ctx context.Context, tenant string, product model.Product) (model.ProtocolRelease, error) {
	if product.ID != "" {
		if binding, err := s.Repo.GetProductProtocolBinding(ctx, tenant, product.ID); err == nil {
			return s.Repo.GetProtocolRelease(ctx, tenant, binding.ProtocolID, binding.Version)
		} else if !errors.Is(err, model.ErrNotFound) {
			return model.ProtocolRelease{}, err
		}
	}
	if protocol, version, ok := splitPackageID(product.ProtocolPackageID); ok {
		if release, err := s.Repo.GetProtocolRelease(ctx, tenant, protocol, version); err == nil || !errors.Is(err, model.ErrNotFound) {
			return release, err
		}
	}
	pkg, err := s.Repo.GetProtocolPackage(ctx, tenant, product.ProtocolPackageID)
	if err != nil {
		return model.ProtocolRelease{}, err
	}
	if pkg.ParserType == parser.GoProtocolParserName {
		return s.Repo.GetProtocolRelease(ctx, tenant, pkg.Protocol, pkg.Version)
	}
	// Declarative packages from before versioned releases are described by the package itself.
	return model.ProtocolRelease{TenantID: tenant, ProtocolID: pkg.Protocol, Version: pkg.Version, Transport: pkg.Transport, PayloadFormat: pkg.PayloadFormat, ParserType: pkg.ParserType, Status: pkg.Status, Config: pkg.Config}, nil
}

func hasNetwork(networks []string, network string) bool {
	for _, value := range networks {
		if value == network {
			return true
		}
	}
	return false
}

// Plan resolves how devices of a saved template connect to the platform.
func (s *Service) Plan(ctx context.Context, tenant string, product model.Product) (AccessPlan, error) {
	return s.plan(ctx, tenant, product, false)
}

// DraftPlan resolves a template that is still being defined in the wizard. New
// templates use the built-in standard protocol or a published protocol version.
func (s *Service) DraftPlan(ctx context.Context, tenant string, product model.Product) (AccessPlan, error) {
	return s.plan(ctx, tenant, product, true)
}

func (s *Service) plan(ctx context.Context, tenant string, product model.Product, draft bool) (AccessPlan, error) {
	var release model.ProtocolRelease
	var err error
	switch protocol, version, ok := splitPackageID(product.ProtocolPackageID); {
	case product.ProtocolPackageID == StandardPackageID:
		release, err = s.Repo.GetProtocolRelease(ctx, tenant, parser.StandardProtocolID, "1.0.0")
		if errors.Is(err, model.ErrNotFound) {
			release, err = standardRelease(tenant, time.Now().UnixMilli()), nil
		}
	case draft && !ok:
		err = model.ErrNotFound
	case draft:
		release, err = s.Repo.GetProtocolRelease(ctx, tenant, protocol, version)
	default:
		release, err = s.productRelease(ctx, tenant, product)
	}
	if errors.Is(err, model.ErrNotFound) {
		reason := "模板尚未绑定已发布的通信协议"
		if draft {
			reason = "请选择内置标准上报或已发布的协议版本"
		}
		return AccessPlan{Mode: ModeUnsupported, Reason: reason}, nil
	}
	if err != nil {
		return AccessPlan{}, err
	}
	return planFor(product, release), nil
}

func planFor(product model.Product, release model.ProtocolRelease) AccessPlan {
	plan := AccessPlan{Release: &release, Protocol: summarize(release)}
	transport := strings.ToUpper(release.Transport)
	switch {
	case !plan.Protocol.Published:
		plan.Mode, plan.Reason = ModeUnsupported, "模板绑定的协议版本未发布或已撤销"
	case release.ParserType == parser.StandardParserName:
		plan.Mode, plan.Connector = ModeStandard, "MQTT"
		if strings.EqualFold(product.Transport, "HTTP") {
			plan.Connector = "HTTP"
		}
	case release.ParserType == parser.ModbusTCPParserName:
		plan.Mode, plan.Connector = ModePoll, "MODBUS_TCP"
	case release.ParserType == parser.ModbusRTUParserName:
		plan.Mode, plan.Connector = ModePoll, "MODBUS_RTU_TCP"
	case transport == "TCP" || transport == "UDP" || transport == "TCP_UDP":
		if release.ParserType != parser.GoProtocolParserName || release.Artifact["runtime"] != protocolworker.Runtime || !protocolworker.HasCapability(release, "ingress") {
			plan.Mode, plan.Reason = ModeUnsupported, "TCP / UDP 接入需要支持 ingress 拆帧能力的 Go 协议"
			break
		}
		plan.Mode = ModeListener
		switch product := strings.ToUpper(product.Transport); {
		case transport == "TCP_UDP" && (product == "TCP" || product == "UDP"):
			plan.Networks = []string{strings.ToLower(product)}
		case transport == "TCP_UDP":
			plan.Networks = []string{"tcp", "udp"}
		default:
			plan.Networks = []string{strings.ToLower(transport)}
		}
		plan.Connector = strings.ToUpper(plan.Networks[0])
		plan.Dial = hasNetwork(plan.Networks, "tcp")
	case transport == "MQTT" || transport == "HTTP" || transport == "MQTT_HTTP":
		plan.Mode, plan.Connector = ModeManaged, "HTTP"
	default:
		plan.Mode, plan.Reason = ModeUnsupported, "该协议的传输方式不支持直接接入设备"
	}
	return plan
}

func summarize(release model.ProtocolRelease) ProtocolSummary {
	return ProtocolSummary{ID: release.ProtocolID, Version: release.Version, ParserType: release.ParserType, Transport: release.Transport, PayloadFormat: release.PayloadFormat, Status: release.Status, Capabilities: release.Capabilities, Published: release.Status == "PUBLISHED"}
}
