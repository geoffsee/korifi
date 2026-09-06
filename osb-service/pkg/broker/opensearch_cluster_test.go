package broker

import (
	"regexp"
	"testing"

	"golang.org/x/crypto/bcrypt"
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

func TestOpenSearchClusterUsesCredentialAndSecurityConfigSecrets(t *testing.T) {
	cluster := openSearchCluster("search", "everest", map[string]string{"test": "true"})

	config, found, err := unstructured.NestedMap(cluster.Object, "spec", "security", "config")
	if err != nil || !found {
		t.Fatalf("security.config = %#v, found %t, error %v", config, found, err)
	}
	if got := config["adminCredentialsSecret"]; !mapsEqual(got, map[string]any{"name": "search-admin"}) {
		t.Fatalf("adminCredentialsSecret = %#v", got)
	}
	if got := config["securityConfigSecret"]; !mapsEqual(got, map[string]any{"name": "search-securityconfig"}) {
		t.Fatalf("securityConfigSecret = %#v", got)
	}
}

func TestOpenSearchClusterHasAQuorumAfterBootstrapExits(t *testing.T) {
	cluster := openSearchCluster("search", "everest", map[string]string{"test": "true"})

	nodePools, found, err := unstructured.NestedSlice(cluster.Object, "spec", "nodePools")
	if err != nil || !found || len(nodePools) != 1 {
		t.Fatalf("nodePools = %#v, found %t, error %v", nodePools, found, err)
	}
	nodePool, ok := nodePools[0].(map[string]any)
	if !ok {
		t.Fatalf("nodePools[0] = %#v", nodePools[0])
	}
	if nodePool["replicas"] != int64(openSearchNodeReplicas) {
		t.Fatalf("nodePools[0].replicas = %#v, want %d", nodePool["replicas"], openSearchNodeReplicas)
	}
}

func TestOpenSearchInternalUsersHashesAdminPassword(t *testing.T) {
	password := "not-the-demo-password"
	config, err := openSearchInternalUsers(password)
	if err != nil {
		t.Fatal(err)
	}
	hashMatch := regexp.MustCompile(`(?m)^  hash: "([^"]+)"$`).FindStringSubmatch(config)
	if len(hashMatch) != 2 {
		t.Fatalf("internal_users.yml has no admin hash: %q", config)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hashMatch[1]), []byte(password)); err != nil {
		t.Fatalf("admin hash does not match password: %v", err)
	}
	if password == "admin" || hashMatch[1] == "admin" {
		t.Fatal("internal_users.yml retained demo credentials")
	}
}

func mapsEqual(got any, want map[string]any) bool {
	m, ok := got.(map[string]any)
	if !ok || len(m) != len(want) {
		return false
	}
	for key, value := range want {
		if m[key] != value {
			return false
		}
	}
	return true
}
