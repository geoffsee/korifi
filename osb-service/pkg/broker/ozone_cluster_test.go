package broker

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
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
