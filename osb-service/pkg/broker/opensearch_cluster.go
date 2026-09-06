package broker

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	openSearchVersion          = "2.19.2"
	openSearchNodeReplicas     = 3
	openSearchProvisionTimeout = 20 * time.Minute
)

var openSearchGVR = schema.GroupVersionResource{
	Group:    "opensearch.opster.io",
	Version:  "v1",
	Resource: "opensearchclusters",
}

func (c *vclusterClient) provisionOpenSearch(ctx context.Context, instanceID string) (map[string]any, error) {
	name, err := resourceName("s", instanceID)
	if err != nil {
		return nil, err
	}
	ls := instanceLabels(instanceID, "opensearch")
	ls["osb.korifi/cluster"] = name
	user, password, err := c.ensureOpenSearchAdminSecret(ctx, name, ls)
	if err != nil {
		return nil, err
	}
	if err := c.ensureOpenSearchSecurityConfig(ctx, name, password, ls); err != nil {
		return nil, err
	}

	obj := openSearchCluster(name, c.namespace, ls)
	if err := c.ensureOpenSearchCluster(ctx, obj); err != nil {
		return nil, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, openSearchProvisionTimeout)
	defer cancel()
	httpTLS, err := c.waitOpenSearchHTTPSecret(waitCtx, name)
	if err != nil {
		return nil, err
	}
	host := c.syncedHost(name, name, name)
	tlsServerName := fmt.Sprintf("%s.%s.svc.cluster.local", name, c.namespace)
	if err := c.waitOpenSearchReady(waitCtx, name, host, tlsServerName, httpTLS.Data["ca.crt"], user, password); err != nil {
		return nil, err
	}
	creds := openSearchCredentials(host, 9200, user, password)
	creds["ca_cert"] = string(httpTLS.Data["ca.crt"])
	creds["tls_server_name"] = tlsServerName
	creds["cluster"] = name
	creds["instance_id"] = instanceID
	creds["engine"] = "opensearch"
	return creds, nil
}

func (c *vclusterClient) ensureOpenSearchAdminSecret(ctx context.Context, name string, ls map[string]string) (string, string, error) {
	user := "admin"
	password, err := randomPassword(24)
	if err != nil {
		return "", "", err
	}
	admin := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-admin", Namespace: c.namespace, Labels: ls},
		Type:       corev1.SecretTypeOpaque,
		StringData: map[string]string{"username": user, "password": password},
	}
	if _, err := c.core.CoreV1().Secrets(c.namespace).Create(ctx, admin, metav1.CreateOptions{}); err == nil {
		return user, password, nil
	} else if !apierrors.IsAlreadyExists(err) {
		return "", "", fmt.Errorf("create opensearch admin secret: %w", err)
	}

	existing, err := c.core.CoreV1().Secrets(c.namespace).Get(ctx, admin.Name, metav1.GetOptions{})
	if err != nil {
		return "", "", fmt.Errorf("get existing opensearch admin secret: %w", err)
	}
	user = string(existing.Data["username"])
	password = string(existing.Data["password"])
	if user == "" || password == "" {
		return "", "", fmt.Errorf("existing opensearch admin secret %s is missing username or password", admin.Name)
	}
	return user, password, nil
}

func (c *vclusterClient) ensureOpenSearchSecurityConfig(ctx context.Context, name, password string, ls map[string]string) error {
	secretName := name + "-securityconfig"
	if _, err := c.core.CoreV1().Secrets(c.namespace).Get(ctx, secretName, metav1.GetOptions{}); err == nil {
		return nil
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get opensearch security config secret: %w", err)
	}

	internalUsers, err := openSearchInternalUsers(password)
	if err != nil {
		return err
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: c.namespace, Labels: ls},
		Type:       corev1.SecretTypeOpaque,
		StringData: map[string]string{"internal_users.yml": internalUsers},
	}
	if _, err := c.core.CoreV1().Secrets(c.namespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create opensearch security config secret: %w", err)
	}
	return nil
}

func openSearchInternalUsers(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash opensearch admin password: %w", err)
	}
	return fmt.Sprintf(`_meta:
  type: "internalusers"
  config_version: 2
admin:
  hash: %q
  reserved: true
  backend_roles:
  - "admin"
  description: "OSB service instance administrator"
`, string(hash)), nil
}

func (c *vclusterClient) ensureOpenSearchCluster(ctx context.Context, desired *unstructured.Unstructured) error {
	resources := c.dyn.Resource(openSearchGVR).Namespace(c.namespace)
	current, err := resources.Get(ctx, desired.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if _, err := resources.Create(ctx, desired, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create OpenSearchCluster: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get OpenSearchCluster: %w", err)
	}
	desiredSpec, _, err := unstructured.NestedMap(desired.Object, "spec")
	if err != nil {
		return fmt.Errorf("read desired OpenSearchCluster spec: %w", err)
	}
	if err := unstructured.SetNestedMap(current.Object, desiredSpec, "spec"); err != nil {
		return fmt.Errorf("set OpenSearchCluster spec: %w", err)
	}
	labels := current.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	for key, value := range desired.GetLabels() {
		labels[key] = value
	}
	current.SetLabels(labels)
	if _, err := resources.Update(ctx, current, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update OpenSearchCluster: %w", err)
	}
	return nil
}

func (c *vclusterClient) waitOpenSearchHTTPSecret(ctx context.Context, name string) (*corev1.Secret, error) {
	secretName := name + "-http-cert"
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		secret, err := c.core.CoreV1().Secrets(c.namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err == nil && len(secret.Data["ca.crt"]) > 0 {
			return secret, nil
		}
		if err != nil && !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("get OpenSearch HTTP TLS secret %s: %w", secretName, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	return nil, fmt.Errorf("timed out waiting for OpenSearch HTTP TLS secret %s", secretName)
}

func (c *vclusterClient) waitOpenSearchReady(ctx context.Context, name, host, tlsServerName string, caCert []byte, username, password string) error {
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caCert) {
		return fmt.Errorf("OpenSearch HTTP TLS secret contains an invalid ca.crt")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
		ServerName: tlsServerName,
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	healthURL := fmt.Sprintf("https://%s/_cluster/health?wait_for_status=yellow&timeout=5s", net.JoinHostPort(host, "9200"))

	for {
		topologyReady, err := c.openSearchTopologyReady(ctx, name)
		if err != nil {
			return err
		}
		if topologyReady {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
			if err != nil {
				return fmt.Errorf("create OpenSearch health request: %w", err)
			}
			request.SetBasicAuth(username, password)
			response, err := client.Do(request)
			if err == nil {
				var health struct {
					Status string `json:"status"`
				}
				decodeErr := json.NewDecoder(response.Body).Decode(&health)
				response.Body.Close()
				if response.StatusCode == http.StatusOK && decodeErr == nil && (health.Status == "yellow" || health.Status == "green") {
					return nil
				}
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for OpenSearch cluster %s to become healthy: %w", name, ctx.Err())
		case <-time.After(5 * time.Second):
		}
	}
}

func (c *vclusterClient) openSearchTopologyReady(ctx context.Context, name string) (bool, error) {
	sts, err := c.core.AppsV1().StatefulSets(c.namespace).Get(ctx, name+"-nodes", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get OpenSearch StatefulSet %s-nodes: %w", name, err)
	}
	if sts.Status.ReadyReplicas != openSearchNodeReplicas {
		return false, nil
	}
	_, err = c.core.CoreV1().Pods(c.namespace).Get(ctx, name+"-bootstrap-0", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("get OpenSearch bootstrap pod %s-bootstrap-0: %w", name, err)
	}
	return false, nil
}

func openSearchCluster(name, namespace string, ls map[string]string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "opensearch.opster.io/v1",
		"kind":       "OpenSearchCluster",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
			"labels":    ls,
		},
		"spec": map[string]any{
			"general": map[string]any{
				"serviceName": name,
				"version":     openSearchVersion,
				"httpPort":    9200,
			},
			"dashboards": map[string]any{
				"enable":   false,
				"replicas": int64(0),
				"version":  openSearchVersion,
			},
			"security": map[string]any{
				"config": map[string]any{
					"adminCredentialsSecret": map[string]any{"name": name + "-admin"},
					"securityConfigSecret":   map[string]any{"name": name + "-securityconfig"},
				},
				"tls": map[string]any{
					"http":      map[string]any{"generate": true},
					"transport": map[string]any{"generate": true, "perNode": true},
				},
			},
			"nodePools": []any{
				map[string]any{
					"component": "nodes",
					"replicas":  int64(openSearchNodeReplicas),
					"diskSize":  "2Gi",
					"roles":     []any{"cluster_manager", "data", "ingest"},
					"resources": map[string]any{
						"requests": map[string]any{"cpu": "100m", "memory": "512Mi"},
					},
				},
			},
		},
	}}
}

func (c *vclusterClient) deprovisionOpenSearch(ctx context.Context, name string) error {
	err := c.dyn.Resource(openSearchGVR).Namespace(c.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
