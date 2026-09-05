package broker

import (
	"net/http"
	"strings"
	"testing"
)

func TestAsyncProvision(t *testing.T) {
	b, err := NewBusinessLogic(Options{Async: true})
	if err != nil {
		t.Fatal(err)
	}
	out, status, err := b.Provision("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", ProvisionRequest{ServiceID: ServiceID, PlanID: PlanID, AcceptsIncomplete: true})
	if err != nil || status != http.StatusAccepted || out.Operation == "" {
		t.Fatalf("got %#v, %d, %v", out, status, err)
	}
}

func TestProvisionConflict(t *testing.T) {
	b, _ := NewBusinessLogic(Options{})
	id := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	_, _, _ = b.Provision(id, ProvisionRequest{ServiceID: ServiceID, PlanID: PlanID})
	_, _, err := b.Provision(id, ProvisionRequest{ServiceID: ServiceID, PlanID: "different"})
	if got := AsAPIError(err).Status; got != http.StatusConflict {
		t.Fatalf("got %d", got)
	}
}

func TestCatalogDedicatedEngines(t *testing.T) {
	b, err := NewBusinessLogic(Options{})
	if err != nil {
		t.Fatal(err)
	}
	c := b.Catalog()
	if len(c.Services) != 5 {
		t.Fatalf("unexpected catalog: %#v", c)
	}
	want := []string{"mongodb", "mysql", "nats", "ozone", "postgres"}
	for i, name := range want {
		if c.Services[i].Name != name || c.Services[i].Plans[0].Name != "dedicated" {
			t.Fatalf("service %d: %#v", i, c.Services[i])
		}
	}
}

func TestBindCredentials(t *testing.T) {
	b, err := NewBusinessLogic(Options{})
	if err != nil {
		t.Fatal(err)
	}
	id := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	_, status, err := b.Provision(id, ProvisionRequest{ServiceID: ServiceID, PlanID: PlanID})
	if err != nil || status != http.StatusCreated {
		t.Fatalf("provision: %d %v", status, err)
	}
	resp, status, err := b.Bind(id, "bbbbbbbb-cccc-dddd-eeee-ffffffffffff", BindRequest{ServiceID: ServiceID, PlanID: PlanID})
	if err != nil || status != http.StatusCreated {
		t.Fatalf("bind: %d %v", status, err)
	}
	for _, key := range []string{"uri", "jdbcUrl", "username", "password", "hostname", "database", "sslmode"} {
		if resp.Credentials[key] == nil || resp.Credentials[key] == "" {
			t.Fatalf("missing credential %q: %#v", key, resp.Credentials)
		}
	}
	if resp.Credentials["sslmode"] != "require" {
		t.Fatalf("sslmode: got %#v", resp.Credentials["sslmode"])
	}
	uri, _ := resp.Credentials["uri"].(string)
	if !strings.Contains(uri, "sslmode=require") {
		t.Fatalf("uri missing sslmode=require: %s", uri)
	}
}

func TestBindMySQLCredentials(t *testing.T) {
	b, err := NewBusinessLogic(Options{})
	if err != nil {
		t.Fatal(err)
	}
	id := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	_, status, err := b.Provision(id, ProvisionRequest{ServiceID: MySQLServiceID, PlanID: MySQLPlanID})
	if err != nil || status != http.StatusCreated {
		t.Fatalf("provision: %d %v", status, err)
	}
	resp, status, err := b.Bind(id, "bbbbbbbb-cccc-dddd-eeee-ffffffffffff", BindRequest{ServiceID: MySQLServiceID, PlanID: MySQLPlanID})
	if err != nil || status != http.StatusCreated {
		t.Fatalf("bind: %d %v", status, err)
	}
	for _, key := range []string{"uri", "jdbcUrl", "username", "password", "hostname", "database", "sslmode"} {
		if resp.Credentials[key] == nil || resp.Credentials[key] == "" {
			t.Fatalf("missing credential %q: %#v", key, resp.Credentials)
		}
	}
	if resp.Credentials["sslmode"] != "REQUIRED" {
		t.Fatalf("sslmode: got %#v", resp.Credentials["sslmode"])
	}
	uri, _ := resp.Credentials["uri"].(string)
	if !strings.Contains(uri, "ssl-mode=REQUIRED") {
		t.Fatalf("uri missing ssl-mode=REQUIRED: %s", uri)
	}
}

func TestBindMongoCredentials(t *testing.T) {
	b, err := NewBusinessLogic(Options{})
	if err != nil {
		t.Fatal(err)
	}
	id := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	_, status, err := b.Provision(id, ProvisionRequest{ServiceID: MongoServiceID, PlanID: MongoPlanID})
	if err != nil || status != http.StatusCreated {
		t.Fatalf("provision: %d %v", status, err)
	}
	resp, status, err := b.Bind(id, "bbbbbbbb-cccc-dddd-eeee-ffffffffffff", BindRequest{ServiceID: MongoServiceID, PlanID: MongoPlanID})
	if err != nil || status != http.StatusCreated {
		t.Fatalf("bind: %d %v", status, err)
	}
	for _, key := range []string{"uri", "username", "password", "hostname", "database"} {
		if resp.Credentials[key] == nil || resp.Credentials[key] == "" {
			t.Fatalf("missing credential %q: %#v", key, resp.Credentials)
		}
	}
	if resp.Credentials["tls"] != true {
		t.Fatalf("tls: got %#v", resp.Credentials["tls"])
	}
	uri, _ := resp.Credentials["uri"].(string)
	if !strings.Contains(uri, "tls=true") || !strings.Contains(uri, "authSource=admin") {
		t.Fatalf("uri missing tls/authSource: %s", uri)
	}
}

func TestBindOzoneCredentials(t *testing.T) {
	b, err := NewBusinessLogic(Options{})
	if err != nil {
		t.Fatal(err)
	}
	id := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	_, status, err := b.Provision(id, ProvisionRequest{ServiceID: OzoneServiceID, PlanID: OzonePlanID})
	if err != nil || status != http.StatusCreated {
		t.Fatalf("provision: %d %v", status, err)
	}
	resp, status, err := b.Bind(id, "bbbbbbbb-cccc-dddd-eeee-ffffffffffff", BindRequest{ServiceID: OzoneServiceID, PlanID: OzonePlanID})
	if err != nil || status != http.StatusCreated {
		t.Fatalf("bind: %d %v", status, err)
	}
	for _, key := range []string{"uri", "endpoint", "access_key_id", "secret_access_key", "hostname", "bucket"} {
		if resp.Credentials[key] == nil || resp.Credentials[key] == "" {
			t.Fatalf("missing credential %q: %#v", key, resp.Credentials)
		}
	}
	if resp.Credentials["use_ssl"] != true || resp.Credentials["tls"] != true || resp.Credentials["path_style"] != true {
		t.Fatalf("tls flags: %#v", resp.Credentials)
	}
	endpoint, _ := resp.Credentials["endpoint"].(string)
	if !strings.HasPrefix(endpoint, "https://") {
		t.Fatalf("endpoint missing https: %s", endpoint)
	}
}

func TestBindNATSCredentials(t *testing.T) {
	b, err := NewBusinessLogic(Options{})
	if err != nil {
		t.Fatal(err)
	}
	id := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	_, status, err := b.Provision(id, ProvisionRequest{ServiceID: NATSServiceID, PlanID: NATSPlanID})
	if err != nil || status != http.StatusCreated {
		t.Fatalf("provision: %d %v", status, err)
	}
	resp, status, err := b.Bind(id, "bbbbbbbb-cccc-dddd-eeee-ffffffffffff", BindRequest{ServiceID: NATSServiceID, PlanID: NATSPlanID})
	if err != nil || status != http.StatusCreated {
		t.Fatalf("bind: %d %v", status, err)
	}
	for _, key := range []string{"uri", "username", "password", "hostname"} {
		if resp.Credentials[key] == nil || resp.Credentials[key] == "" {
			t.Fatalf("missing credential %q: %#v", key, resp.Credentials)
		}
	}
	if resp.Credentials["tls"] != true {
		t.Fatalf("tls: %#v", resp.Credentials["tls"])
	}
	uri, _ := resp.Credentials["uri"].(string)
	if !strings.HasPrefix(uri, "tls://") {
		t.Fatalf("uri missing tls scheme: %s", uri)
	}
}

func TestGenerateTLS(t *testing.T) {
	mat, err := generateTLS("example.test", []string{"example.test", "svc.cluster.local"})
	if err != nil {
		t.Fatal(err)
	}
	if len(mat.CertPEM) == 0 || len(mat.KeyPEM) == 0 || len(mat.PKCS12) == 0 || mat.Password == "" {
		t.Fatalf("incomplete tls material: %#v", mat)
	}
}

func TestSyncedHost(t *testing.T) {
	c := &vclusterClient{namespace: "everest", hostNS: "everest-vcluster", vclusterName: "everest"}
	got := c.syncedHost("", "pabc", "primary")
	want := "pabc-primary-everest-x-everest.everest-vcluster.svc.cluster.local"
	if got != want {
		t.Fatalf("fallback: got %q want %q", got, want)
	}
	got = c.syncedHost("foo-haproxy.everest.svc.cluster.local", "foo", "haproxy")
	want = "foo-haproxy-everest-x-everest.everest-vcluster.svc.cluster.local"
	if got != want {
		t.Fatalf("status host: got %q want %q", got, want)
	}
}

func TestResourceName(t *testing.T) {
	got, err := resourceName("d", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	if err != nil {
		t.Fatal(err)
	}
	if got != "daaaaaaaabbbbccccddddeeeeeeeeeeee" {
		t.Fatalf("got %q", got)
	}
	if _, err := resourceName("d", "not a uuid!"); err == nil {
		t.Fatal("expected error")
	}
}
