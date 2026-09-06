package broker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type persistedInstance struct {
	Request     ProvisionRequest `json:"request"`
	Credentials map[string]any   `json:"credentials"`
}

type persistedStore struct {
	Instances map[string]persistedInstance `json:"instances"`
	Bindings  map[string]BindResponse      `json:"bindings"`
}

type fileBackend struct {
	path      string
	instances map[string]instance
	bindings  map[string]BindResponse
}

func newFileBackend(path string) (*fileBackend, error) {
	if path == "" {
		return nil, errors.New("store path must not be empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create store directory: %w", err)
	}

	b := &fileBackend{
		path:      path,
		instances: map[string]instance{},
		bindings:  map[string]BindResponse{},
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := b.persist(b.instances, b.bindings); err != nil {
			return nil, err
		}
		return b, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read store: %w", err)
	}

	var state persistedStore
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode store: %w", err)
	}
	for id, stored := range state.Instances {
		b.instances[id] = instance{req: stored.Request, credentials: stored.Credentials}
	}
	if state.Bindings != nil {
		b.bindings = state.Bindings
	}
	return b, nil
}

func (f *fileBackend) getInstance(id string) (instance, bool, error) {
	inst, ok := f.instances[id]
	return inst, ok, nil
}

func (f *fileBackend) putInstance(id string, inst instance) error {
	next := cloneInstances(f.instances)
	next[id] = inst
	if err := f.persist(next, f.bindings); err != nil {
		return err
	}
	f.instances = next
	return nil
}

func (f *fileBackend) deleteInstance(id string) (bool, error) {
	if _, ok := f.instances[id]; !ok {
		return false, nil
	}
	nextInstances := cloneInstances(f.instances)
	delete(nextInstances, id)
	nextBindings := cloneBindings(f.bindings)
	for key := range nextBindings {
		if len(key) > len(id) && key[:len(id)+1] == id+"/" {
			delete(nextBindings, key)
		}
	}
	if err := f.persist(nextInstances, nextBindings); err != nil {
		return false, err
	}
	f.instances = nextInstances
	f.bindings = nextBindings
	return true, nil
}

func (f *fileBackend) getBinding(instanceID, bindingID string) (BindResponse, bool, error) {
	binding, ok := f.bindings[instanceID+"/"+bindingID]
	return binding, ok, nil
}

func (f *fileBackend) putBinding(instanceID, bindingID string, response BindResponse) error {
	next := cloneBindings(f.bindings)
	next[instanceID+"/"+bindingID] = response
	if err := f.persist(f.instances, next); err != nil {
		return err
	}
	f.bindings = next
	return nil
}

func (f *fileBackend) deleteBinding(instanceID, bindingID string) (bool, error) {
	key := instanceID + "/" + bindingID
	if _, ok := f.bindings[key]; !ok {
		return false, nil
	}
	next := cloneBindings(f.bindings)
	delete(next, key)
	if err := f.persist(f.instances, next); err != nil {
		return false, err
	}
	f.bindings = next
	return true, nil
}

func (f *fileBackend) healthy(context.Context) error {
	_, err := os.Stat(f.path)
	return err
}

func (f *fileBackend) persist(instances map[string]instance, bindings map[string]BindResponse) (returnErr error) {
	state := persistedStore{
		Instances: make(map[string]persistedInstance, len(instances)),
		Bindings:  bindings,
	}
	for id, inst := range instances {
		state.Instances[id] = persistedInstance{Request: inst.req, Credentials: inst.credentials}
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode store: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(f.path), ".osb-store-*")
	if err != nil {
		return fmt.Errorf("create temporary store: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if returnErr != nil {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secure temporary store: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary store: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temporary store: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary store: %w", err)
	}
	if err := os.Rename(tmpName, f.path); err != nil {
		return fmt.Errorf("replace store: %w", err)
	}
	return nil
}

func cloneInstances(source map[string]instance) map[string]instance {
	result := make(map[string]instance, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneBindings(source map[string]BindResponse) map[string]BindResponse {
	result := make(map[string]BindResponse, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
