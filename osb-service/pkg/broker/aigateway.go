package broker

import (
	"context"
	"net"
	"net/url"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	AIGatewayServiceID = "1a4f6c80-3e9b-4d2a-8c5e-7b0d1f3a6e92"
	AIGatewayPlanID    = "2b5e7d91-4f0c-4e3b-9d6f-8c1e2a4b7f03"
)

type aiGatewayOffering struct {
	vc *vclusterClient
}

func newAIGatewayOffering(_ Options, vc *vclusterClient) *aiGatewayOffering {
	return &aiGatewayOffering{vc: vc}
}

func (p *aiGatewayOffering) Catalog() Service {
	return Service{
		Name:                 "aigateway",
		ID:                   AIGatewayServiceID,
		Description:          "Envoy AI Gateway (OpenAI-compatible API)",
		Bindable:             true,
		PlanUpdateable:       true,
		InstancesRetrievable: true,
		BindingsRetrievable:  true,
		Tags:                 []string{"ai", "llm", "openai", "gateway"},
		Metadata: map[string]any{
			"displayName":         "AI Gateway",
			"providerDisplayName": "osb-service",
			"longDescription":     "Binds a tenant API key on the platform Envoy AI Gateway, which forwards to a configured OpenAI-compatible backend.",
		},
		Plans: []Plan{{
			Name:        "dedicated",
			ID:          AIGatewayPlanID,
			Description: "A dedicated tenant on the shared AI Gateway",
			Free:        boolPtr(true),
		}},
	}
}

func (p *aiGatewayOffering) Healthy(ctx context.Context) error {
	if p.vc == nil {
		return nil
	}
	_, err := p.vc.dyn.Resource(securityPolicyGVR).Namespace(p.vc.namespace).List(ctx, metav1.ListOptions{Limit: 1})
	return err
}

func (p *aiGatewayOffering) Provision(id string, req ProvisionRequest) (instance, error) {
	if p.vc != nil {
		creds, err := p.vc.provisionAIGatewayTenant(context.Background(), id)
		if err != nil {
			return instance{}, err
		}
		return instance{req: req, credentials: creds}, nil
	}
	apiKey, err := randomPassword(32)
	if err != nil {
		return instance{}, err
	}
	creds := aiGatewayCredentials("localhost", 443, apiKey)
	creds["instance_id"] = id
	return instance{req: req, credentials: creds}, nil
}

func (p *aiGatewayOffering) Deprovision(inst instance) error {
	if p.vc == nil {
		return nil
	}
	id, _ := inst.credentials["instance_id"].(string)
	if id == "" {
		return nil
	}
	return p.vc.deprovisionAIGatewayTenant(context.Background(), id)
}

func aiGatewayCredentials(host string, port int, apiKey string) map[string]any {
	base := url.URL{
		Scheme: "https",
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
		Path:   "/v1",
	}
	return map[string]any{
		"uri":             base.String(),
		"openai_api_base": base.String(),
		"api_key":         apiKey,
		"hostname":        host,
		"port":            port,
		"tls":             true,
	}
}
