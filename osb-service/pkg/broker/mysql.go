package broker

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
)

const (
	MySQLServiceID = "c5d8e2a1-4b7f-4c9e-8a3d-1f6b9e0c2d47"
	MySQLPlanID    = "e7f1a3b5-2c4d-4e8f-9a1b-3d5c7e9f0a12"
)

type mysqlOffering struct {
	everest *everestClient
}

func newMySQLOffering(_ Options, everest *everestClient) *mysqlOffering {
	return &mysqlOffering{everest: everest}
}

func (p *mysqlOffering) Catalog() Service {
	return Service{
		Name:                 "mysql",
		ID:                   MySQLServiceID,
		Description:          "Dedicated Percona XtraDB Cluster (MySQL)",
		Bindable:             true,
		PlanUpdateable:       true,
		InstancesRetrievable: true,
		BindingsRetrievable:  true,
		Tags:                 []string{"mysql", "pxc", "xtradb", "percona", "relational"},
		Metadata: map[string]any{
			"displayName":         "Percona XtraDB (MySQL)",
			"providerDisplayName": "osb-service",
			"longDescription":     "Provisions a dedicated Percona XtraDB Cluster via OpenEverest.",
		},
		Plans: []Plan{{
			Name:        "dedicated",
			ID:          MySQLPlanID,
			Description: "A dedicated Percona XtraDB Cluster",
			Free:        boolPtr(true),
		}},
	}
}

func (p *mysqlOffering) Healthy(ctx context.Context) error {
	if p.everest == nil {
		return nil
	}
	return p.everest.healthy(ctx)
}

func (p *mysqlOffering) Provision(id string, req ProvisionRequest) (instance, error) {
	if p.everest != nil {
		creds, err := p.everest.provision(context.Background(), id, mysqlEngine)
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
	creds := mysqlCredentials("localhost", 3306, dbname, username, password)
	return instance{req: req, credentials: creds}, nil
}

func (p *mysqlOffering) Deprovision(inst instance) error {
	if p.everest == nil {
		return nil
	}
	name, _ := inst.credentials["cluster"].(string)
	if name == "" {
		return nil
	}
	return p.everest.deprovision(context.Background(), name)
}

func mysqlCredentials(host string, port int, dbname, username, password string) map[string]any {
	uri := url.URL{
		Scheme:   "mysql",
		User:     url.UserPassword(username, password),
		Host:     net.JoinHostPort(host, strconv.Itoa(port)),
		Path:     "/" + dbname,
		RawQuery: "ssl-mode=" + url.QueryEscape("REQUIRED"),
	}
	jdbc := fmt.Sprintf(
		"jdbc:mysql://%s/%s?useSSL=true&requireSSL=true&user=%s&password=%s",
		net.JoinHostPort(host, strconv.Itoa(port)),
		dbname,
		url.QueryEscape(username),
		url.QueryEscape(password),
	)
	return map[string]any{
		"username": username,
		"password": password,
		"hostname": host,
		"host":     host,
		"port":     port,
		"database": dbname,
		"name":     dbname,
		"sslmode":  "REQUIRED",
		"uri":      uri.String(),
		"jdbcUrl":  jdbc,
	}
}
