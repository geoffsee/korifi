package broker

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const openSearchVersion = "2.19.2"

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
	password, err := randomPassword(24)
	if err != nil {
		return nil, err
	}
	user := "admin"
	ls := instanceLabels(instanceID, "opensearch")
	ls["osb.korifi/cluster"] = name

	admin := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-admin", Namespace: c.namespace, Labels: ls},
		Type:       corev1.SecretTypeOpaque,
		StringData: map[string]string{"username": user, "password": password},
	}
	if _, err := c.core.CoreV1().Secrets(c.namespace).Create(ctx, admin, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create opensearch admin secret: %w", err)
	}

	obj := openSearchCluster(name, c.namespace, ls)
	if _, err := c.dyn.Resource(openSearchGVR).Namespace(c.namespace).Create(ctx, obj, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create OpenSearchCluster: %w", err)
	}
	if err := c.waitSTS(ctx, name+"-nodes"); err != nil {
		return nil, err
	}
	host := c.syncedHost(name, name, name)
	creds := openSearchCredentials(host, 9200, user, password)
	creds["cluster"] = name
	creds["instance_id"] = instanceID
	creds["engine"] = "opensearch"
	return creds, nil
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
				},
				"tls": map[string]any{
					"http":      map[string]any{"generate": true},
					"transport": map[string]any{"generate": true, "perNode": true},
				},
			},
			"nodePools": []any{
				map[string]any{
					"component": "nodes",
					"replicas":  1,
					"diskSize":  "2Gi",
					"roles":     []any{"cluster_manager", "data", "ingest"},
					"resources": map[string]any{
						"requests": map[string]any{"cpu": "250m", "memory": "1Gi"},
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
