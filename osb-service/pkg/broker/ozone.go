package broker

import (
	"context"
	"net"
	"net/url"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	OzoneServiceID = "7e2f1a90-4c6b-4d8e-a1b3-5c7d9e0f2a14"
	OzonePlanID    = "8f3a2b01-5d7c-4e9f-b2c4-6d8e0f1a3b25"
)

type ozoneOffering struct {
	vc *vclusterClient
}

func newOzoneOffering(_ Options, vc *vclusterClient) *ozoneOffering {
	return &ozoneOffering{vc: vc}
}

func (p *ozoneOffering) Catalog() Service {
	return Service{
		Name:                 "ozone",
		ID:                   OzoneServiceID,
		Description:          "Dedicated Apache Ozone object store (S3 API)",
		Bindable:             true,
		PlanUpdateable:       true,
		InstancesRetrievable: true,
		BindingsRetrievable:  true,
		Tags:                 []string{"ozone", "s3", "object-store", "apache"},
		Metadata: map[string]any{
			"displayName":         "Apache Ozone",
			"providerDisplayName": "osb-service",
			"longDescription":     "Provisions a dedicated Apache Ozone cluster with an HTTPS S3 gateway.",
		},
		Plans: []Plan{{
			Name:        "dedicated",
			ID:          OzonePlanID,
			Description: "A dedicated Apache Ozone cluster",
			Free:        boolPtr(true),
		}},
	}
}

func (p *ozoneOffering) Healthy(ctx context.Context) error {
	if p.vc == nil {
		return nil
	}
	_, err := p.vc.core.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1})
	return err
}

func (p *ozoneOffering) Provision(id string, req ProvisionRequest) (instance, error) {
	if p.vc != nil {
		creds, err := p.vc.provisionOzone(context.Background(), id)
		if err != nil {
			return instance{}, err
		}
		return instance{req: req, credentials: creds}, nil
	}
	bucket, err := resourceName("b", id)
	if err != nil {
		return instance{}, err
	}
	access, err := resourceName("a", id)
	if err != nil {
		return instance{}, err
	}
	secret, err := randomPassword(32)
	if err != nil {
		return instance{}, err
	}
	creds := ozoneCredentials("localhost", 9878, bucket, access, secret)
	creds["instance_id"] = id
	return instance{req: req, credentials: creds}, nil
}

func (p *ozoneOffering) Deprovision(inst instance) error {
	if p.vc == nil {
		return nil
	}
	id, _ := inst.credentials["instance_id"].(string)
	if id == "" {
		return nil
	}
	return p.vc.deprovisionLabeled(context.Background(), id)
}

func ozoneCredentials(host string, port int, bucket, accessKey, secretKey string) map[string]any {
	endpoint := "https://" + net.JoinHostPort(host, strconv.Itoa(port))
	uri := url.URL{
		Scheme: "https",
		User:   url.UserPassword(accessKey, secretKey),
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
		Path:   "/" + bucket,
	}
	return map[string]any{
		"access_key_id":     accessKey,
		"secret_access_key": secretKey,
		"username":          accessKey,
		"password":          secretKey,
		"hostname":          host,
		"host":              host,
		"port":              port,
		"endpoint":          endpoint,
		"bucket":            bucket,
		"region":            "us-east-1",
		"path_style":        true,
		"use_ssl":           true,
		"tls":               true,
		"uri":               uri.String(),
	}
}
