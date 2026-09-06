package broker

import (
	"fmt"
	"strings"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	osbInstanceLabel = "osb.korifi/instance-id"
	osbOfferingLabel = "osb.korifi/offering"
)

// vclusterClient talks to the isolated data-plane vcluster (the same
// kubeconfig Everest uses). Offerings that are not OpenEverest engines
// still create dedicated resources here so Services sync to the host.
type vclusterClient struct {
	dyn          dynamic.Interface
	core         kubernetes.Interface
	namespace    string
	hostNS       string
	vclusterName string
}

func newVClusterClient(o Options) (*vclusterClient, error) {
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
	return &vclusterClient{
		dyn:          dyn,
		core:         core,
		namespace:    o.EverestNamespace,
		hostNS:       o.EverestHostNamespace,
		vclusterName: o.EverestVClusterName,
	}, nil
}

func (c *vclusterClient) syncedHost(inClusterHost, cluster, svcSuffix string) string {
	svc := cluster + "-" + svcSuffix
	if inClusterHost != "" {
		svc = strings.Split(inClusterHost, ".")[0]
	}
	return fmt.Sprintf("%s-x-%s-x-%s.%s.svc.cluster.local", svc, c.namespace, c.vclusterName, c.hostNS)
}

func instanceLabels(instanceID, offering string) map[string]string {
	return map[string]string{
		osbInstanceLabel: instanceID,
		osbOfferingLabel: offering,
	}
}
