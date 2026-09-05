package broker

import (
	"context"
	"net"
	"net/url"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	RedisServiceID = "2b7c4e91-6d8a-4f1b-9c3e-5a0d8f2b4c67"
	RedisPlanID    = "3c8d5f02-7e9b-4a2c-8d4f-6b1e9a3c5d78"
)

type redisOffering struct {
	vc *vclusterClient
}

func newRedisOffering(_ Options, vc *vclusterClient) *redisOffering {
	return &redisOffering{vc: vc}
}

func (p *redisOffering) Catalog() Service {
	return Service{
		Name:                 "redis",
		ID:                   RedisServiceID,
		Description:          "Dedicated Redis server",
		Bindable:             true,
		PlanUpdateable:       true,
		InstancesRetrievable: true,
		BindingsRetrievable:  true,
		Tags:                 []string{"redis", "key-value", "cache", "datastore"},
		Metadata: map[string]any{
			"displayName":         "Redis",
			"providerDisplayName": "osb-service",
			"longDescription":     "Provisions a dedicated Redis server with TLS.",
		},
		Plans: []Plan{{
			Name:        "dedicated",
			ID:          RedisPlanID,
			Description: "A dedicated Redis server",
			Free:        boolPtr(true),
		}},
	}
}

func (p *redisOffering) Healthy(ctx context.Context) error {
	if p.vc == nil {
		return nil
	}
	_, err := p.vc.core.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1})
	return err
}

func (p *redisOffering) Provision(id string, req ProvisionRequest) (instance, error) {
	if p.vc != nil {
		creds, err := p.vc.provisionRedis(context.Background(), id)
		if err != nil {
			return instance{}, err
		}
		return instance{req: req, credentials: creds}, nil
	}
	password, err := randomPassword(24)
	if err != nil {
		return instance{}, err
	}
	creds := redisCredentials("localhost", 6379, password)
	creds["instance_id"] = id
	return instance{req: req, credentials: creds}, nil
}

func (p *redisOffering) Deprovision(inst instance) error {
	if p.vc == nil {
		return nil
	}
	id, _ := inst.credentials["instance_id"].(string)
	if id == "" {
		return nil
	}
	return p.vc.deprovisionLabeled(context.Background(), id)
}

func redisCredentials(host string, port int, password string) map[string]any {
	uri := url.URL{
		Scheme: "rediss",
		User:   url.UserPassword("default", password),
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
		Path:   "/0",
	}
	return map[string]any{
		"username": "default",
		"password": password,
		"hostname": host,
		"host":     host,
		"port":     port,
		"name":     "0",
		"tls":      true,
		"uri":      uri.String(),
	}
}
