package broker

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestDisabledOpenSearchDashboardsSatisfyOperatorSchema(t *testing.T) {
	cluster := openSearchCluster("search", "everest", map[string]string{"test": "true"})

	dashboards, found, err := unstructured.NestedMap(cluster.Object, "spec", "dashboards")
	if err != nil || !found {
		t.Fatalf("dashboards = %#v, found %t, error %v", dashboards, found, err)
	}
	if dashboards["enable"] != false {
		t.Fatalf("dashboards.enable = %#v, want false", dashboards["enable"])
	}
	if dashboards["replicas"] != int64(0) {
		t.Fatalf("dashboards.replicas = %#v, want 0", dashboards["replicas"])
	}
	if dashboards["version"] != openSearchVersion {
		t.Fatalf("dashboards.version = %#v, want %q", dashboards["version"], openSearchVersion)
	}
}
