package broker

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
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
	// DefaultDatabase is used when the operator connection Secret omits a
	// logical database name. An empty value uses the cluster name.
	DefaultDatabase string
	DefaultUsername string
	CPU             string
	Memory          string
	// TLSSecretSuffix identifies the operator-managed Secret containing the
	// database CA. Engines that require a client certificate also expose the
	// certificate and key from this Secret in the service binding.
	TLSSecretSuffix  string
	IncludeClientTLS bool
	// Service is the in-vcluster Service name suffix used when status.hostname
	// is empty. Host DNS is rewritten to the vcluster-synced Service.
	Service string
	Prefix  string
	// MaxNameLength accommodates engine-operator limits stricter than DNS-1123.
	MaxNameLength int
}

var (
	postgresEngine = everestEngine{
		Type: "postgresql", ProxyType: "pgbouncer", Version: "17.10",
		DefaultPort: 5432, DefaultDatabase: "postgres", Service: "primary", Prefix: "p",
		CPU: "250m", Memory: "512Mi", TLSSecretSuffix: "-cluster-cert",
	}
	mysqlEngine = everestEngine{
		Type: "pxc", ProxyType: "haproxy", Version: "8.0.39-30.1",
		DefaultPort: 3306, DefaultDatabase: "mysql", Service: "haproxy", Prefix: "x", MaxNameLength: 22,
		DefaultUsername: "root", CPU: "500m", Memory: "1Gi", TLSSecretSuffix: "-ssl",
	}
	mongoEngine = everestEngine{
		Type: "psmdb", Version: "8.0.12-4",
		DefaultPort: 27017, Service: "rs0", Prefix: "m",
		CPU: "500m", Memory: "1Gi", TLSSecretSuffix: "-ssl", IncludeClientTLS: true,
	}
)

func (e everestEngine) resources() map[string]any {
	return map[string]any{"cpu": e.CPU, "memory": e.Memory}
}

type everestClient struct {
	dyn          dynamic.Interface
	core         kubernetes.Interface
	namespace    string
	hostNS       string
	vclusterName string
}

func newEverestClient(o Options) (*everestClient, error) {
	if o.EverestKubeconfig == "" {
		return nil, nil
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", o.EverestKubeconfig)
	if err != nil {
		return nil, fmt.Errorf("everest kubeconfig: %w", err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	core, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &everestClient{
		dyn:          dyn,
		core:         core,
		namespace:    o.EverestNamespace,
		hostNS:       o.EverestHostNamespace,
		vclusterName: o.EverestVClusterName,
	}, nil
}

func (c *everestClient) healthy(ctx context.Context) error {
	_, err := c.dyn.Resource(databaseClusterGVR).Namespace(c.namespace).List(ctx, metav1.ListOptions{Limit: 1})
	return err
}

func (c *everestClient) provision(ctx context.Context, instanceID string, eng everestEngine) (map[string]any, error) {
	name, err := everestResourceName(eng, instanceID)
	if err != nil {
		return nil, err
	}
	engineSpec := map[string]any{
		"type":      eng.Type,
		"replicas":  1,
		"storage":   map[string]any{"size": "2Gi"},
		"resources": eng.resources(),
	}
	if eng.Version != "" {
		engineSpec["version"] = eng.Version
	}
	proxy := map[string]any{
		"replicas": 1,
		"expose":   map[string]any{"type": "ClusterIP"},
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
	if err := c.waitReady(ctx, name); err != nil {
		return nil, err
	}
	secret, statusHost, statusPort, err := c.waitUserSecret(ctx, name)
	if err != nil {
		return nil, err
	}
	tlsSecret, err := c.waitTLSSecret(ctx, name, eng)
	if err != nil {
		return nil, err
	}
	return c.credentialsFromSecret(eng, name, secret, tlsSecret, statusHost, statusPort), nil
}

func (c *everestClient) waitReady(ctx context.Context, name string) error {
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		cluster, err := c.dyn.Resource(databaseClusterGVR).Namespace(c.namespace).Get(ctx, name, metav1.GetOptions{})
		if err == nil && databaseClusterReady(cluster) {
			return nil
		}
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("get OpenEverest DatabaseCluster %s: %w", name, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	return fmt.Errorf("timed out waiting for OpenEverest DatabaseCluster %s to become ready", name)
}

func databaseClusterReady(cluster *unstructured.Unstructured) bool {
	status, found, err := unstructured.NestedString(cluster.Object, "status", "status")
	return err == nil && found && status == "ready"
}

func everestResourceName(eng everestEngine, instanceID string) (string, error) {
	name, err := resourceName(eng.Prefix, instanceID)
	if err != nil {
		return "", err
	}
	if eng.MaxNameLength > 0 && len(name) > eng.MaxNameLength {
		name = name[:eng.MaxNameLength]
	}
	return name, nil
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

func (c *everestClient) waitTLSSecret(ctx context.Context, name string, eng everestEngine) (*corev1.Secret, error) {
	secretName := name + eng.TLSSecretSuffix
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		sec, err := c.core.CoreV1().Secrets(c.namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err == nil && tlsSecretReady(sec, eng.IncludeClientTLS) {
			return sec, nil
		}
		if err != nil && !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("get OpenEverest TLS secret %s: %w", secretName, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	return nil, fmt.Errorf("timed out waiting for OpenEverest TLS secret %s", secretName)
}

func tlsSecretReady(sec *corev1.Secret, requireClientTLS bool) bool {
	if len(sec.Data["ca.crt"]) == 0 {
		return false
	}
	return !requireClientTLS || len(sec.Data["tls.crt"]) > 0 && len(sec.Data["tls.key"]) > 0
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

func (c *everestClient) syncedHost(inClusterHost, cluster, svcSuffix string) string {
	svc := cluster + "-" + svcSuffix
	if inClusterHost != "" {
		svc = strings.Split(inClusterHost, ".")[0]
	}
	return fmt.Sprintf("%s-%s-x-%s.%s.svc.cluster.local", svc, c.namespace, c.vclusterName, c.hostNS)
}

func (c *everestClient) credentialsFromSecret(eng everestEngine, cluster string, sec, tlsSecret *corev1.Secret, statusHost string, statusPort int64) map[string]any {
	get := func(keys ...string) string {
		for _, k := range keys {
			if v := sec.Data[k]; len(v) > 0 {
				return string(v)
			}
		}
		return ""
	}
	user := get("user", "username", "MONGODB_DATABASE_ADMIN_USER")
	if user == "" {
		user = eng.DefaultUsername
	}
	pass := get("password", "root", "MONGODB_DATABASE_ADMIN_PASSWORD")
	db := get("dbname", "database")
	if db == "" {
		db = eng.DefaultDatabase
	}
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
	creds["ca_cert"] = string(tlsSecret.Data["ca.crt"])
	tlsServerName := statusHost
	if tlsServerName == "" {
		tlsServerName = fmt.Sprintf("%s-%s.%s.svc.cluster.local", cluster, eng.Service, c.namespace)
	}
	creds["tls_server_name"] = tlsServerName
	if eng.IncludeClientTLS {
		creds["tls_cert"] = string(tlsSecret.Data["tls.crt"])
		creds["tls_key"] = string(tlsSecret.Data["tls.key"])
	}
	return creds
}
