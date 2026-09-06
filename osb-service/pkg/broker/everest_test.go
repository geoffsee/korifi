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

func TestPostgresCredentialsDefaultToExistingDatabase(t *testing.T) {
	client := &everestClient{
		namespace:    "everest",
		hostNS:       "everest-vcluster",
		vclusterName: "everest",
	}
	secret := &corev1.Secret{Data: map[string][]byte{
		"user":     []byte("database-user"),
		"password": []byte("database-password"),
	}}

	credentials := client.credentialsFromSecret("pcluster", secret)

	if got := credentials["database"]; got != "postgres" {
		t.Fatalf("database = %q, want postgres", got)
	}
}
