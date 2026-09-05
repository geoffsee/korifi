package broker

import (
	"context"
	"net"
	"net/url"
	"strconv"
)

const (
	MongoServiceID = "9b3e7d21-5a8c-4f6e-b2d4-8c1a0e7f3b56"
	MongoPlanID    = "d4a6c8e0-1b3f-4d5e-9c7a-2e8f0b4d6a19"
)

type mongoOffering struct {
	everest *everestClient
}

func newMongoOffering(_ Options, everest *everestClient) *mongoOffering {
	return &mongoOffering{everest: everest}
}

func (p *mongoOffering) Catalog() Service {
	return Service{
		Name:                 "mongodb",
		ID:                   MongoServiceID,
		Description:          "Dedicated Percona Server for MongoDB",
		Bindable:             true,
		PlanUpdateable:       true,
		InstancesRetrievable: true,
		BindingsRetrievable:  true,
		Tags:                 []string{"mongodb", "psmdb", "percona", "document"},
		Metadata: map[string]any{
			"displayName":         "MongoDB",
			"providerDisplayName": "osb-service",
			"longDescription":     "Provisions a dedicated Percona Server for MongoDB cluster via OpenEverest.",
		},
		Plans: []Plan{{
			Name:        "dedicated",
			ID:          MongoPlanID,
			Description: "A dedicated MongoDB replica set",
			Free:        boolPtr(true),
		}},
	}
}

func (p *mongoOffering) Healthy(ctx context.Context) error {
	if p.everest == nil {
		return nil
	}
	return p.everest.healthy(ctx)
}

func (p *mongoOffering) Provision(id string, req ProvisionRequest) (instance, error) {
	if p.everest != nil {
		creds, err := p.everest.provision(context.Background(), id, mongoEngine)
		if err != nil {
			return instance{}, err
		}
		return instance{req: req, credentials: creds}, nil
	}
	dbname, err := resourceName("d", id)
	if err != nil {
		return instance{}, err
	}
	username, err := resourceName("u", id)
	if err != nil {
		return instance{}, err
	}
	password, err := randomPassword(24)
	if err != nil {
		return instance{}, err
	}
	creds := mongoCredentials("localhost", 27017, dbname, username, password)
	return instance{req: req, credentials: creds}, nil
}

func (p *mongoOffering) Deprovision(inst instance) error {
	if p.everest == nil {
		return nil
	}
	name, _ := inst.credentials["cluster"].(string)
	if name == "" {
		return nil
	}
	return p.everest.deprovision(context.Background(), name)
}

func mongoCredentials(host string, port int, dbname, username, password string) map[string]any {
	uri := url.URL{
		Scheme: "mongodb",
		User:   url.UserPassword(username, password),
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
		Path:   "/" + dbname,
		RawQuery: url.Values{
			"tls":        []string{"true"},
			"authSource": []string{"admin"},
		}.Encode(),
	}
	return map[string]any{
		"username": username,
		"password": password,
		"hostname": host,
		"host":     host,
		"port":     port,
		"database": dbname,
		"name":     dbname,
		"tls":      true,
		"uri":      uri.String(),
	}
}
