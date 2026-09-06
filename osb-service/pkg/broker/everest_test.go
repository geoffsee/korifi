package broker

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestDatabaseClusterReady(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status any
		ready  bool
	}{
		{name: "ready", status: "ready", ready: true},
		{name: "initializing", status: "initializing", ready: false},
		{name: "missing status", ready: false},
		{name: "unexpected status type", status: true, ready: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cluster := &unstructured.Unstructured{Object: map[string]any{}}
			if tc.status != nil {
				cluster.Object["status"] = map[string]any{"status": tc.status}
			}
			if got := databaseClusterReady(cluster); got != tc.ready {
				t.Fatalf("databaseClusterReady() = %t, want %t", got, tc.ready)
			}
		})
	}
}

func TestCredentialsUseEngineDefaultDatabase(t *testing.T) {
	client := &everestClient{
		namespace:    "everest",
		hostNS:       "everest-vcluster",
		vclusterName: "everest",
	}
	secret := &corev1.Secret{Data: map[string][]byte{
		"user":     []byte("database-user"),
		"password": []byte("database-password"),
	}}

	for _, tc := range []struct {
		name     string
		engine   everestEngine
		cluster  string
		expected string
	}{
		{name: "postgres", engine: postgresEngine, cluster: "pcluster", expected: "postgres"},
		{name: "mysql", engine: mysqlEngine, cluster: "xcluster", expected: "mysql"},
		{name: "mongodb", engine: mongoEngine, cluster: "mcluster", expected: "mcluster"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			credentials := client.credentialsFromSecret(tc.engine, tc.cluster, secret, "", 0)

			if got := credentials["database"]; got != tc.expected {
				t.Fatalf("database = %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestEverestEngineResources(t *testing.T) {
	for _, tc := range []struct {
		name   string
		engine everestEngine
		cpu    string
		memory string
	}{
		{name: "postgres", engine: postgresEngine, cpu: "250m", memory: "512Mi"},
		{name: "mysql", engine: mysqlEngine, cpu: "500m", memory: "1Gi"},
		{name: "mongodb", engine: mongoEngine, cpu: "500m", memory: "1Gi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resources := tc.engine.resources()
			if resources["cpu"] != tc.cpu || resources["memory"] != tc.memory {
				t.Fatalf("resources = %#v, want cpu=%s memory=%s", resources, tc.cpu, tc.memory)
			}
		})
	}
}
