package broker

import (
	"strings"
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
		vclusterClient: &vclusterClient{
			namespace:    "everest",
			hostNS:       "everest-vcluster",
			vclusterName: "everest",
		},
	}
	secret := &corev1.Secret{Data: map[string][]byte{
		"user":     []byte("database-user"),
		"password": []byte("database-password"),
	}}
	tlsSecret := &corev1.Secret{Data: map[string][]byte{
		"ca.crt":  []byte("test-ca"),
		"tls.crt": []byte("test-certificate"),
		"tls.key": []byte("test-key"),
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
			credentials := client.credentialsFromSecret(tc.engine, tc.cluster, secret, tlsSecret, "", 0)

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

func TestMySQLCredentialsUseRootUsername(t *testing.T) {
	client := &everestClient{
		vclusterClient: &vclusterClient{
			namespace:    "everest",
			hostNS:       "everest-vcluster",
			vclusterName: "everest",
		},
	}
	secret := &corev1.Secret{Data: map[string][]byte{
		"root": []byte("root-password"),
	}}
	tlsSecret := &corev1.Secret{Data: map[string][]byte{
		"ca.crt": []byte("test-ca"),
	}}

	credentials := client.credentialsFromSecret(mysqlEngine, "xcluster", secret, tlsSecret, "", 0)

	if got := credentials["username"]; got != "root" {
		t.Fatalf("username = %q, want root", got)
	}
	if got := credentials["password"]; got != "root-password" {
		t.Fatalf("password = %q, want root-password", got)
	}
}

func TestEverestTLSCredentials(t *testing.T) {
	client := &everestClient{
		vclusterClient: &vclusterClient{
			namespace:    "everest",
			hostNS:       "everest-vcluster",
			vclusterName: "everest",
		},
	}
	userSecret := &corev1.Secret{Data: map[string][]byte{
		"MONGODB_DATABASE_ADMIN_USER":     []byte("database-user"),
		"MONGODB_DATABASE_ADMIN_PASSWORD": []byte("database-password"),
	}}
	tlsSecret := &corev1.Secret{Data: map[string][]byte{
		"ca.crt":  []byte("test-ca"),
		"tls.crt": []byte("test-certificate"),
		"tls.key": []byte("test-key"),
	}}

	credentials := client.credentialsFromSecret(
		mongoEngine,
		"mcluster",
		userSecret,
		tlsSecret,
		"mcluster-rs0.everest.svc.cluster.local",
		27017,
	)

	for key, want := range map[string]any{
		"ca_cert":         "test-ca",
		"tls_cert":        "test-certificate",
		"tls_key":         "test-key",
		"tls_server_name": "mcluster-rs0.everest.svc.cluster.local",
	} {
		if got := credentials[key]; got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	uri, ok := credentials["uri"].(string)
	if !ok || !strings.Contains(uri, "directConnection=true") || !strings.Contains(uri, "tlsAllowInvalidHostnames=true") {
		t.Fatalf("uri = %q, want direct connection and hostname relaxation", uri)
	}

	postgresCredentials := client.credentialsFromSecret(postgresEngine, "pcluster", userSecret, tlsSecret, "", 0)
	if _, ok := postgresCredentials["tls_cert"]; ok {
		t.Error("PostgreSQL binding unexpectedly contains a client certificate")
	}
	if _, ok := postgresCredentials["tls_key"]; ok {
		t.Error("PostgreSQL binding unexpectedly contains a client key")
	}
}

func TestTLSSecretReady(t *testing.T) {
	caOnly := &corev1.Secret{Data: map[string][]byte{"ca.crt": []byte("test-ca")}}
	withClientTLS := &corev1.Secret{Data: map[string][]byte{
		"ca.crt": []byte("test-ca"), "tls.crt": []byte("test-cert"), "tls.key": []byte("test-key"),
	}}

	if !tlsSecretReady(caOnly, false) {
		t.Error("CA-only TLS secret should be ready")
	}
	if tlsSecretReady(caOnly, true) {
		t.Error("CA-only TLS secret should not satisfy client TLS")
	}
	if !tlsSecretReady(withClientTLS, true) {
		t.Error("complete client TLS secret should be ready")
	}
}
