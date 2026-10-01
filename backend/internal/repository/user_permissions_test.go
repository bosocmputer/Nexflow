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
	if !permissionForKey(admin, "settings_menu_permissions").CanView {
		t.Fatal("admin must always see settings_menu_permissions")
	}
	if !permissionForKey(admin, "tiktok_shop_connections").CanView {
		t.Fatal("admin should see TikTok Shop connections")
	}
	if !permissionForKey(admin, "tiktok_shop_operations").CanView {
		t.Fatal("admin should see TikTok Shop operations")
	}
	if !permissionForKey(admin, "marketplace_operations").CanView {
		t.Fatal("admin should see unified Marketplace operations")
	}
	if permission := permissionForKey(admin, "tiktok_shop_stock"); !permission.CanView || !permission.CanUpdate {
		t.Fatalf("admin should view and update TikTok Shop stock workspace: %+v", permission)
	}
	for _, key := range []string{"line_myshop", "setup", "instance_settings", "old_data"} {
		if permissionForKey(admin, key).MenuKey != "" {
			t.Fatalf("retired menu %s should not be granted to new users", key)
		}
	}

	staff := defaultMenuPermissionsForRole("staff")
	if !permissionForKey(staff, "nextstep_marketplace").CanView {
		t.Fatal("staff should see NextStep Marketplace")
	}
	if permissionForKey(staff, "settings_users").CanView {
		t.Fatal("staff should not see user settings by default")
	}
	if permissionForKey(staff, "tiktok_shop_connections").CanView {
		t.Fatal("staff should not manage TikTok Shop connections by default")
	}
	if !permissionForKey(staff, "tiktok_shop_operations").CanView {
		t.Fatal("staff should see read-only TikTok Shop operations by default")
	}
	if !permissionForKey(staff, "marketplace_operations").CanView {
		t.Fatal("staff should see unified Marketplace operations by default")
	}
	if permissionForKey(staff, "tiktok_shop_stock").CanView {
		t.Fatal("staff should not see TikTok Shop stock workspace by default")
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

func TestNormalizeMenuPermissionsRejectsRetiredAndAdminOnlyStaffGrants(t *testing.T) {
	perms := normalizeMenuPermissionsForRole("staff", []models.UserMenuPermission{
		{MenuKey: "line_myshop", CanView: true},
		{MenuKey: "old_data", CanView: true},
		{MenuKey: "channel_defaults", CanView: true, CanUpdate: true},
		{MenuKey: "line_notifications", CanView: true},
	})
	for _, key := range []string{"line_myshop", "old_data"} {
		if permissionForKey(perms, key).MenuKey != "" {
			t.Fatalf("retired menu %s should be ignored", key)
		}
	}
	for _, key := range []string{"channel_defaults", "line_notifications"} {
		got := permissionForKey(perms, key)
		if got.CanView || got.CanCreate || got.CanUpdate || got.CanDelete {
			t.Fatalf("staff must not gain admin-only menu %s: %+v", key, got)
		}
	}
}

func TestNormalizeMenuPermissionsKeepsAdminUsersVisible(t *testing.T) {
	perms := normalizeMenuPermissionsForRole("admin", []models.UserMenuPermission{
		{MenuKey: "settings_users", CanView: false, CanCreate: false, CanUpdate: false, CanDelete: false},
		{MenuKey: "settings_menu_permissions", CanView: false, CanCreate: false, CanUpdate: false, CanDelete: false},
	})
	for _, key := range []string{"settings_users", "settings_menu_permissions"} {
		got := permissionForKey(perms, key)
		if !got.CanView {
			t.Fatalf("admin %s can_view should be forced true: %+v", key, got)
		}
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
