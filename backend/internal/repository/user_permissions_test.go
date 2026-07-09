package repository

import (
	"testing"

	"nexflow/internal/models"
)

func TestDefaultMenuPermissionsForRole(t *testing.T) {
	admin := defaultMenuPermissionsForRole("admin")
	if !permissionForKey(admin, "settings_users").CanView {
		t.Fatal("admin must always see settings_users")
	}
	if !permissionForKey(admin, "old_data").CanDelete {
		t.Fatal("admin delete default should be true for old_data")
	}

	staff := defaultMenuPermissionsForRole("staff")
	if !permissionForKey(staff, "nextstep_marketplace").CanView {
		t.Fatal("staff should see NextStep Marketplace")
	}
	if permissionForKey(staff, "settings_users").CanView {
		t.Fatal("staff should not see user settings by default")
	}

	viewer := defaultMenuPermissionsForRole("viewer")
	if !permissionForKey(viewer, "dashboard").CanView {
		t.Fatal("viewer should see dashboard")
	}
	if permissionForKey(viewer, "nextstep_marketplace").CanView {
		t.Fatal("viewer should not see NextStep Marketplace because API remains admin/staff")
	}
}

func TestNormalizeMenuPermissionsForRole(t *testing.T) {
	perms := normalizeMenuPermissionsForRole("staff", []models.UserMenuPermission{
		{MenuKey: "dashboard", CanView: false, CanCreate: true, CanUpdate: true, CanDelete: true},
		{MenuKey: "unknown_menu", CanView: true},
	})
	dashboard := permissionForKey(perms, "dashboard")
	if dashboard.CanView || dashboard.CanCreate || dashboard.CanUpdate || dashboard.CanDelete {
		t.Fatalf("hidden dashboard should force all actions false: %+v", dashboard)
	}
	if permissionForKey(perms, "unknown_menu").MenuKey != "" {
		t.Fatal("unknown menu key should be ignored")
	}
}

func TestNormalizeMenuPermissionsKeepsAdminUsersVisible(t *testing.T) {
	perms := normalizeMenuPermissionsForRole("admin", []models.UserMenuPermission{
		{MenuKey: "settings_users", CanView: false, CanCreate: false, CanUpdate: false, CanDelete: false},
	})
	got := permissionForKey(perms, "settings_users")
	if !got.CanView {
		t.Fatalf("admin settings_users can_view should be forced true: %+v", got)
	}
}

func permissionForKey(perms []models.UserMenuPermission, key string) models.UserMenuPermission {
	for _, p := range perms {
		if p.MenuKey == key {
			return p
		}
	}
	return models.UserMenuPermission{}
}
