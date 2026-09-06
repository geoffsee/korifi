package broker

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestFileBackendPersistsInstancesAndBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	store, err := newFileBackend(path)
	if err != nil {
		t.Fatal(err)
	}

	instanceID := "instance-id"
	bindingID := "binding-id"
	inst := instance{
		req: ProvisionRequest{ServiceID: PostgresServiceID, PlanID: PostgresPlanID},
		credentials: map[string]any{
			"hostname": "example.internal",
			"password": "secret",
		},
	}
	binding := BindResponse{Credentials: inst.credentials}
	if err := store.putInstance(instanceID, inst); err != nil {
		t.Fatal(err)
	}
	if err := store.putBinding(instanceID, bindingID, binding); err != nil {
		t.Fatal(err)
	}

	reopened, err := newFileBackend(path)
	if err != nil {
		t.Fatal(err)
	}
	gotInstance, ok, err := reopened.getInstance(instanceID)
	if err != nil || !ok {
		t.Fatalf("get instance: ok=%t err=%v", ok, err)
	}
	if !reflect.DeepEqual(gotInstance, inst) {
		t.Fatalf("instance mismatch: got %#v want %#v", gotInstance, inst)
	}
	gotBinding, ok, err := reopened.getBinding(instanceID, bindingID)
	if err != nil || !ok {
		t.Fatalf("get binding: ok=%t err=%v", ok, err)
	}
	if !reflect.DeepEqual(gotBinding, binding) {
		t.Fatalf("binding mismatch: got %#v want %#v", gotBinding, binding)
	}

	deleted, err := reopened.deleteInstance(instanceID)
	if err != nil || !deleted {
		t.Fatalf("delete instance: deleted=%t err=%v", deleted, err)
	}
	final, err := newFileBackend(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := final.getInstance(instanceID); err != nil || ok {
		t.Fatalf("instance remained after reopen: ok=%t err=%v", ok, err)
	}
	if _, ok, err := final.getBinding(instanceID, bindingID); err != nil || ok {
		t.Fatalf("binding remained after instance deletion: ok=%t err=%v", ok, err)
	}
}
