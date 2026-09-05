package broker

import (
	"context"
	"net"
	"net/url"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	NATSServiceID = "3c8e9f12-6a4d-4b7e-9c1a-2d5f8b0e4a73"
	NATSPlanID    = "4d9f0a23-7b5e-4c8f-8d2b-3e6a9c1f5b84"
)

type natsOffering struct {
	vc *vclusterClient
}

func newNATSOffering(_ Options, vc *vclusterClient) *natsOffering {
	return &natsOffering{vc: vc}
}

func (p *natsOffering) Catalog() Service {
	return Service{
		Name:                 "nats",
		ID:                   NATSServiceID,
		Description:          "Dedicated NATS JetStream server",
		Bindable:             true,
		PlanUpdateable:       true,
		InstancesRetrievable: true,
		BindingsRetrievable:  true,
		Tags:                 []string{"nats", "jetstream", "messaging", "pubsub"},
		Metadata: map[string]any{
			"displayName":         "NATS",
			"providerDisplayName": "osb-service",
			"longDescription":     "Provisions a dedicated NATS server with JetStream and TLS.",
		},
		Plans: []Plan{{
			Name:        "dedicated",
			ID:          NATSPlanID,
			Description: "A dedicated NATS JetStream server",
			Free:        boolPtr(true),
		}},
	}
}

func (p *natsOffering) Healthy(ctx context.Context) error {
	if p.vc == nil {
		return nil
	}
	_, err := p.vc.core.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1})
	return err
}

func (p *natsOffering) Provision(id string, req ProvisionRequest) (instance, error) {
	if p.vc != nil {
		creds, err := p.vc.provisionNATS(context.Background(), id)
		if err != nil {
			return instance{}, err
		}
		return instance{req: req, credentials: creds}, nil
	}
	user, err := resourceName("u", id)
	if err != nil {
		return instance{}, err
	}
	password, err := randomPassword(24)
	if err != nil {
		return instance{}, err
	}
	creds := natsCredentials("localhost", 4222, user, password)
	creds["instance_id"] = id
	return instance{req: req, credentials: creds}, nil
}

func (p *natsOffering) Deprovision(inst instance) error {
	if p.vc == nil {
		return nil
	}
	id, _ := inst.credentials["instance_id"].(string)
	if id == "" {
		return nil
	}
	return p.vc.deprovisionLabeled(context.Background(), id)
}

func natsCredentials(host string, port int, username, password string) map[string]any {
	uri := url.URL{
		Scheme: "tls",
		User:   url.UserPassword(username, password),
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
	}
	return map[string]any{
		"username": username,
		"password": password,
		"hostname": host,
		"host":     host,
		"port":     port,
		"tls":      true,
		"uri":      uri.String(),
	}
}
