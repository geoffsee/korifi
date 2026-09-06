package broker

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/retry"
)

const (
	aiGatewayServiceName      = "aigw"
	aiGatewayServiceNamespace = "envoy-gateway-system"
	aiGatewayTLSSecretName    = "aigw-tls"
	aiGatewayPolicyName       = "aigw-clients"
)

var securityPolicyGVR = schema.GroupVersionResource{
	Group:    "gateway.envoyproxy.io",
	Version:  "v1alpha1",
	Resource: "securitypolicies",
}

func (c *vclusterClient) provisionAIGatewayTenant(ctx context.Context, instanceID string) (map[string]any, error) {
	name, err := resourceName("t", instanceID)
	if err != nil {
		return nil, err
	}
	gatewayTLS, err := c.core.CoreV1().Secrets(c.namespace).Get(ctx, aiGatewayTLSSecretName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get AI Gateway TLS secret: %w", err)
	}
	caCert := gatewayTLS.Data["ca.crt"]
	if len(caCert) == 0 {
		return nil, fmt.Errorf("AI Gateway TLS secret %s is missing ca.crt", aiGatewayTLSSecretName)
	}
	apiKey, err := randomPassword(32)
	if err != nil {
		return nil, err
	}
	ls := instanceLabels(instanceID, "aigateway")
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: c.namespace, Labels: ls},
		Type:       corev1.SecretTypeOpaque,
		StringData: map[string]string{instanceID: apiKey},
	}
	if _, err := c.core.CoreV1().Secrets(c.namespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create aigateway tenant secret: %w", err)
	}
	if err := c.patchAPIKeyCredential(ctx, name, true); err != nil {
		return nil, err
	}
	host := c.syncedHostInNamespace(aiGatewayServiceName, aiGatewayServiceNamespace, "", "")
	creds := aiGatewayCredentials(host, 443, apiKey)
	creds["ca_cert"] = string(caCert)
	creds["instance_id"] = instanceID
	creds["engine"] = "aigateway"
	return creds, nil
}

func (c *vclusterClient) deprovisionAIGatewayTenant(ctx context.Context, instanceID string) error {
	name, err := resourceName("t", instanceID)
	if err != nil {
		return err
	}
	_ = c.patchAPIKeyCredential(ctx, name, false)
	return c.deprovisionLabeled(ctx, instanceID)
}

func (c *vclusterClient) patchAPIKeyCredential(ctx context.Context, secretName string, add bool) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		pol, err := c.dyn.Resource(securityPolicyGVR).Namespace(c.namespace).Get(ctx, aiGatewayPolicyName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get SecurityPolicy %s: %w", aiGatewayPolicyName, err)
		}
		refs, _, _ := unstructured.NestedSlice(pol.Object, "spec", "apiKeyAuth", "credentialRefs")
		next := make([]any, 0, len(refs)+1)
		found := false
		for _, r := range refs {
			m, ok := r.(map[string]any)
			if !ok {
				continue
			}
			if fmt.Sprint(m["name"]) == secretName {
				found = true
				if !add {
					continue
				}
			}
			next = append(next, m)
		}
		if add && !found {
			next = append(next, map[string]any{
				"group": "",
				"kind":  "Secret",
				"name":  secretName,
			})
		}
		if err := unstructured.SetNestedSlice(pol.Object, next, "spec", "apiKeyAuth", "credentialRefs"); err != nil {
			return err
		}
		_, err = c.dyn.Resource(securityPolicyGVR).Namespace(c.namespace).Update(ctx, pol, metav1.UpdateOptions{})
		return err
	})
}
