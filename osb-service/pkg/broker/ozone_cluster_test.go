package broker

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestOzoneComponentsUsePortsAvailableWithHTTPSOnly(t *testing.T) {
	components := ozoneComponents("test")
	ports := map[string]int32{}
	for _, component := range components {
		ports[component.comp] = component.port
		if component.comp == "om" && component.env[0].Value != "test-scm-0.test-scm:9861" {
			t.Fatalf("OM WAITFOR = %q, want SCM RPC port", component.env[0].Value)
		}
	}

	want := map[string]int32{"scm": 9861, "om": 9862, "datanode": 9883, "s3g": 9878}
	for component, port := range want {
		if ports[component] != port {
			t.Fatalf("%s port = %d, want %d", component, ports[component], port)
		}
	}
}

func TestOzoneTCPProbesGateReadinessAndAllowSlowStartup(t *testing.T) {
	startup, liveness, readiness := ozoneTCPProbes(9878)
	for name, probe := range map[string]*corev1.Probe{"startup": startup, "liveness": liveness, "readiness": readiness} {
		if probe == nil || probe.TCPSocket == nil || probe.TCPSocket.Port.IntValue() != 9878 {
			t.Fatalf("%s probe does not check TCP port 9878: %#v", name, probe)
		}
	}
	if startup.PeriodSeconds*startup.FailureThreshold < 300 {
		t.Fatalf("startup allowance = %ds, want at least 300s", startup.PeriodSeconds*startup.FailureThreshold)
	}
}

func TestOzoneHeadlessServicesPublishAddressesBeforePodsAreReady(t *testing.T) {
	client := &vclusterClient{core: fake.NewSimpleClientset(), namespace: "everest"}
	labels := map[string]string{"osb.korifi/cluster": "test"}

	if err := client.createHeadlessService(context.Background(), "test-om", "om", 9862, labels); err != nil {
		t.Fatalf("create OM service: %v", err)
	}
	service, err := client.core.CoreV1().Services("everest").Get(context.Background(), "test-om", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get OM service: %v", err)
	}
	if !service.Spec.PublishNotReadyAddresses {
		t.Fatal("headless service must publish not-ready addresses so StatefulSet pods can resolve their own hostnames during startup")
	}
}
