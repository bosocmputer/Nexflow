package shopeegateway

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTenantRegistryDerivesBackendURLFromPort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instances.json")
	body := []byte(`{"instances":[{"name":"aoy","public_url":"https://nexflow-aoy.nextstep-soft.com","backend_port":8111}]}`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	tenants, err := LoadTenantRegistry(path)
	if err != nil {
		t.Fatalf("LoadTenantRegistry() error = %v", err)
	}
	if len(tenants) != 1 || tenants[0].BackendURL != "http://172.17.0.1:8111" {
		t.Fatalf("tenants = %+v", tenants)
	}
}

func TestLoadTenantRegistryRejectsNonHTTPSPublicURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instances.json")
	body := []byte(`{"instances":[{"name":"demo","public_url":"http://nexflow.example.com","backend_port":8110}]}`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	if _, err := LoadTenantRegistry(path); err == nil {
		t.Fatal("expected insecure public URL to be rejected")
	}
}
