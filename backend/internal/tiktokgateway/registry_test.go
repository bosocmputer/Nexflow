package tiktokgateway

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTenantRegistrySupportsMultipleNexflowTenants(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instances.json")
	body := []byte(`{"instances":[{"name":"aoy","public_url":"https://nexflow-aoy.nextstep-soft.com","backend_port":8111},{"name":"demo","public_url":"https://nexflow.nextstep-soft.com","gateway_backend_url":"http://nexflow-demo-backend:8080"}]}`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	tenants, err := LoadTenantRegistry(path)
	if err != nil {
		t.Fatalf("LoadTenantRegistry() error = %v", err)
	}
	if len(tenants) != 2 || tenants[0].Slug != "aoy" || tenants[0].BackendURL != "http://172.17.0.1:8111" || tenants[1].BackendURL != "http://nexflow-demo-backend:8080" {
		t.Fatalf("tenants = %+v", tenants)
	}
}

func TestLoadTenantRegistryRejectsDuplicateTenant(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instances.json")
	body := []byte(`{"instances":[{"name":"AOY","public_url":"https://nexflow-aoy.nextstep-soft.com","backend_port":8111},{"name":"aoy","public_url":"https://nexflow-aoy.nextstep-soft.com","backend_port":8111}]}`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	if _, err := LoadTenantRegistry(path); err == nil {
		t.Fatal("expected duplicate tenant to be rejected")
	}
}

func TestLoadTenantRegistryRejectsInsecurePublicURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instances.json")
	body := []byte(`{"instances":[{"name":"aoy","public_url":"http://nexflow-aoy.nextstep-soft.com","backend_port":8111}]}`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	if _, err := LoadTenantRegistry(path); err == nil {
		t.Fatal("expected insecure public URL to be rejected")
	}
}
