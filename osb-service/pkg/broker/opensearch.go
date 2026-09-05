package broker

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	OpenSearchServiceID = "5e0a1b34-8c6f-4d9a-9e3c-4f7b0d2a6c95"
	OpenSearchPlanID    = "6f1b2c45-9d7a-4e0b-8f4d-5a8c1e3b7d06"
)

type openSearchOffering struct {
	vc *vclusterClient
}

func newOpenSearchOffering(_ Options, vc *vclusterClient) *openSearchOffering {
	return &openSearchOffering{vc: vc}
}

func (p *openSearchOffering) Catalog() Service {
	return Service{
		Name:                 "opensearch",
		ID:                   OpenSearchServiceID,
		Description:          "Dedicated OpenSearch cluster",
		Bindable:             true,
		PlanUpdateable:       true,
		InstancesRetrievable: true,
		BindingsRetrievable:  true,
		Tags:                 []string{"opensearch", "search", "analytics", "elasticsearch"},
		Metadata: map[string]any{
			"displayName":         "OpenSearch",
			"providerDisplayName": "osb-service",
			"longDescription":     "Provisions a dedicated OpenSearch cluster with HTTPS via the OpenSearch operator.",
		},
		Plans: []Plan{{
			Name:        "dedicated",
			ID:          OpenSearchPlanID,
			Description: "A dedicated OpenSearch cluster",
			Free:        boolPtr(true),
		}},
	}
}

func (p *openSearchOffering) Healthy(ctx context.Context) error {
	if p.vc == nil {
		return nil
	}
	_, err := p.vc.dyn.Resource(openSearchGVR).Namespace(p.vc.namespace).List(ctx, metav1.ListOptions{Limit: 1})
	return err
}

func (p *openSearchOffering) Provision(id string, req ProvisionRequest) (instance, error) {
	if p.vc != nil {
		creds, err := p.vc.provisionOpenSearch(context.Background(), id)
		if err != nil {
			return instance{}, err
		}
		return instance{req: req, credentials: creds}, nil
	}
	user := "admin"
	password, err := randomPassword(24)
	if err != nil {
		return instance{}, err
	}
	creds := openSearchCredentials("localhost", 9200, user, password)
	creds["instance_id"] = id
	return instance{req: req, credentials: creds}, nil
}

func (p *openSearchOffering) Deprovision(inst instance) error {
	if p.vc == nil {
		return nil
	}
	name, _ := inst.credentials["cluster"].(string)
	id, _ := inst.credentials["instance_id"].(string)
	if name != "" {
		if err := p.vc.deprovisionOpenSearch(context.Background(), name); err != nil {
			return err
		}
	}
	if id != "" {
		return p.vc.deprovisionLabeled(context.Background(), id)
	}
	return nil
}

func openSearchCredentials(host string, port int, username, password string) map[string]any {
	uri := url.URL{
		Scheme: "https",
		User:   url.UserPassword(username, password),
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
	}
	return map[string]any{
		"username": username,
		"password": password,
		"hostname": host,
		"host":     host,
		"port":     port,
		"uri":      uri.String(),
		"tls":      true,
		"use_ssl":  true,
		"jdbcUrl": fmt.Sprintf(
			"jdbc:opensearch://https://%s?user=%s&password=%s",
			net.JoinHostPort(host, strconv.Itoa(port)),
			url.QueryEscape(username),
			url.QueryEscape(password),
		),
	}
}
