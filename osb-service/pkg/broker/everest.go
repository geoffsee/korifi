package broker

import (
	"context"
	"fmt"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var databaseClusterGVR = schema.GroupVersionResource{
	Group:    "everest.percona.com",
	Version:  "v1alpha1",
	Resource: "databaseclusters",
}

// everestEngine is one OpenEverest DatabaseCluster engine. Each OSB offering
// provisions a dedicated cluster of this type — never a shared instance.
type everestEngine struct {
	Type        string
	ProxyType   string
	Version     string
	DefaultPort int
	// Service is the in-vcluster Service name suffix used when status.hostname
	// is empty. Host DNS is rewritten to the vcluster-synced Service.
	Service string
	Prefix  string
}

var (
	postgresEngine = everestEngine{
		Type: "postgresql", ProxyType: "pgbouncer", Version: "17.4",
		DefaultPort: 5432, Service: "primary", Prefix: "p",
	}
	mysqlEngine = everestEngine{
		Type: "pxc", ProxyType: "haproxy", Version: "8.0.39-30.1",
		DefaultPort: 3306, Service: "haproxy", Prefix: "x",
	}
	mongoEngine = everestEngine{
		Type: "psmdb", Version: "8.0.12-4",
		DefaultPort: 27017, Service: "rs0", Prefix: "m",
	}
)

type everestClient struct {
	*vclusterClient
}

func newEverestClient(o Options) (*everestClient, error) {
	vc, err := newVClusterClient(o)
	if err != nil || vc == nil {
		return nil, err
	}
	return &everestClient{vclusterClient: vc}, nil
}

func (c *everestClient) healthy(ctx context.Context) error {
	_, err := c.dyn.Resource(databaseClusterGVR).Namespace(c.namespace).List(ctx, metav1.ListOptions{Limit: 1})
	return err
}

func (c *everestClient) provision(ctx context.Context, instanceID string, eng everestEngine) (map[string]any, error) {
	name, err := resourceName(eng.Prefix, instanceID)
	if err != nil {
		return nil, err
	}
	engineSpec := map[string]any{
		"type":     eng.Type,
		"replicas": 1,
		"storage":  map[string]any{"size": "2Gi"},
		"resources": map[string]any{
			"cpu":    "250m",
			"memory": "512Mi",
		},
	}
	if eng.Version != "" {
		engineSpec["version"] = eng.Version
	}
	proxy := map[string]any{
		"replicas": 1,
		"expose":   map[string]any{"type": "internal"},
	}
	if eng.ProxyType != "" {
		proxy["type"] = eng.ProxyType
	}
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "everest.percona.com/v1alpha1",
		"kind":       "DatabaseCluster",
		"metadata": map[string]any{
			"name":      name,
			"namespace": c.namespace,
			"labels": map[string]any{
				"osb.korifi/instance-id": instanceID,
				"osb.korifi/engine":      eng.Type,
			},
		},
		"spec": map[string]any{
			"backup": map[string]any{"pitr": map[string]any{"enabled": false}},
			"engine": engineSpec,
			"proxy":  proxy,
		},
	}}
	_, err = c.dyn.Resource(databaseClusterGVR).Namespace(c.namespace).Create(ctx, obj, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create DatabaseCluster %s: %w", eng.Type, err)
	}
	secret, statusHost, statusPort, err := c.waitUserSecret(ctx, name)
	if err != nil {
		return nil, err
	}
	return c.credentialsFromSecret(eng, name, secret, statusHost, statusPort), nil
}

func (c *everestClient) deprovision(ctx context.Context, name string) error {
	err := c.dyn.Resource(databaseClusterGVR).Namespace(c.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (c *everestClient) waitUserSecret(ctx context.Context, name string) (*corev1.Secret, string, int64, error) {
	candidates := []string{
		"everest-secrets-" + name,
		name + "-pguser-" + name,
		name + "-secrets",
	}
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		for _, secretName := range candidates {
			sec, err := c.core.CoreV1().Secrets(c.namespace).Get(ctx, secretName, metav1.GetOptions{})
			if err == nil && secretHasPassword(sec) {
				host, port := c.clusterEndpoint(ctx, name)
				return sec, host, port, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, "", 0, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	return nil, "", 0, fmt.Errorf("timed out waiting for OpenEverest user secret for %s", name)
}

func secretHasPassword(sec *corev1.Secret) bool {
	for _, k := range []string{"password", "root", "MONGODB_DATABASE_ADMIN_PASSWORD"} {
		if len(sec.Data[k]) > 0 {
			return true
		}
	}
	return false
}

func (c *everestClient) clusterEndpoint(ctx context.Context, name string) (string, int64) {
	obj, err := c.dyn.Resource(databaseClusterGVR).Namespace(c.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", 0
	}
	host, _, _ := unstructured.NestedString(obj.Object, "status", "hostname")
	port, _, _ := unstructured.NestedInt64(obj.Object, "status", "port")
	return host, port
}

func (c *everestClient) credentialsFromSecret(eng everestEngine, cluster string, sec *corev1.Secret, statusHost string, statusPort int64) map[string]any {
	get := func(keys ...string) string {
		for _, k := range keys {
			if v := sec.Data[k]; len(v) > 0 {
				return string(v)
			}
		}
		return ""
	}
	user := get("user", "username", "root", "MONGODB_DATABASE_ADMIN_USER")
	pass := get("password", "root", "MONGODB_DATABASE_ADMIN_PASSWORD")
	db := get("dbname", "database")
	if db == "" {
		db = cluster
	}
	port := eng.DefaultPort
	if statusPort > 0 {
		port = int(statusPort)
	} else if p := get("port"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}
	host := c.syncedHost(statusHost, cluster, eng.Service)
	var creds map[string]any
	switch eng.Type {
	case "pxc":
		creds = mysqlCredentials(host, port, db, user, pass)
	case "psmdb":
		creds = mongoCredentials(host, port, db, user, pass)
	default:
		creds = postgresCredentials(host, port, "require", db, user, pass)
	}
	creds["cluster"] = cluster
	creds["engine"] = eng.Type
	return creds
}
