package repository

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"nexflow/internal/models"
)

type UserRepo struct {
	db *sql.DB
}

type menuPermissionDefault struct {
	Key                        string
	AdminView, AdminCreate     bool
	AdminUpdate, AdminDelete   bool
	StaffView, StaffCreate     bool
	StaffUpdate, StaffDelete   bool
	ViewerView, ViewerCreate   bool
	ViewerUpdate, ViewerDelete bool
}

var menuPermissionDefaults = []menuPermissionDefault{
	{"dashboard", true, false, false, false, true, false, false, false, true, false, false, false},
	{"nextstep_marketplace", true, false, false, false, true, false, false, false, false, false, false, false},
	{"shopee_operations", true, true, true, false, true, true, true, false, false, false, false, false},
	{"sale_invoices", true, true, true, true, true, true, true, false, true, false, false, false},
	{"sales_orders", true, true, true, true, true, true, true, false, true, false, false, false},
	{"purchase_orders", true, true, true, true, true, true, true, false, true, false, false, false},
	{"marketplace_aliases", true, false, true, false, true, false, true, false, false, false, false, false},
	{"bulk_send_jobs", true, true, true, false, true, true, true, false, false, false, false, false},
	{"import_shopee", true, true, false, false, true, true, false, false, false, false, false, false},
	{"import_lazada", true, true, false, false, true, true, false, false, false, false, false, false},
	{"import_tiktok", true, true, false, false, true, true, false, false, false, false, false, false},
	{"shopee_settlements", true, true, true, false, true, true, true, false, false, false, false, false},
	{"mappings", true, true, true, true, true, true, true, false, true, false, false, false},
	{"catalog", true, true, true, true, true, true, true, false, true, false, false, false},
	{"messages", true, true, true, false, true, true, true, false, false, false, false, false},
	{"line_notifications", true, true, true, true, false, false, false, false, false, false, false, false},
	{"line_oa", true, true, true, true, false, false, false, false, false, false, false, false},
	{"line_myshop", true, true, true, true, false, false, false, false, false, false, false, false},
	{"quick_replies", true, true, true, true, false, false, false, false, false, false, false, false},
	{"chat_tags", true, true, true, true, false, false, false, false, false, false, false, false},
	{"setup", true, false, true, false, false, false, false, false, false, false, false, false},
	{"channel_defaults", true, true, true, true, false, false, false, false, false, false, false, false},
	{"email_accounts", true, true, true, true, false, false, false, false, false, false, false, false},
	{"shopee_connections", true, true, true, true, false, false, false, false, false, false, false, false},
	{"instance_settings", true, false, true, false, false, false, false, false, false, false, false, false},
	{"settings_users", true, true, true, true, false, false, false, false, false, false, false, false},
	{"logs", true, false, false, false, true, false, false, false, false, false, false, false},
	{"ai_usage", true, false, false, false, false, false, false, false, false, false, false, false},
	{"old_data", true, false, true, true, false, false, false, false, false, false, false, false},
}

func NewUserRepo(db *sql.DB) *UserRepo {
	return &UserRepo{db: db}
}

func (r *UserRepo) FindByEmail(email string) (*models.User, error) {
	u := &models.User{}
	err := r.db.QueryRow(
		`SELECT id, email, name, role, password_hash, created_at
		 FROM users WHERE email = $1`, email,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.PasswordHash, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("FindByEmail: %w", err)
	}
	if err := r.attachMenuPermissions(u); err != nil {
		return nil, err
	}
	return u, nil
}

func (r *UserRepo) FindByID(id string) (*models.User, error) {
	u := &models.User{}
	err := r.db.QueryRow(
		`SELECT id, email, name, role, password_hash, created_at
		 FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.PasswordHash, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("FindByID: %w", err)
	}
	if err := r.attachMenuPermissions(u); err != nil {
		return nil, err
	}
	return u, nil
}

func (r *UserRepo) Create(email, name, role, passwordHash string) (*models.User, error) {
	u := &models.User{}
	tx, err := r.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("Create user begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	err = tx.QueryRow(
		`INSERT INTO users (email, name, role, password_hash)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, email, name, role, password_hash, created_at`,
		email, name, role, passwordHash,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.PasswordHash, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("Create user: %w", err)
	}
	if err := upsertMenuPermissionsTx(tx, u.ID, u.Role, defaultMenuPermissionsForRole(u.Role), ""); err != nil {
		return nil, fmt.Errorf("Create user permissions: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("Create user commit: %w", err)
	}
	u.MenuPermissions = defaultMenuPermissionsForRole(u.Role)
	return u, nil
}

func (r *UserRepo) List() ([]models.User, error) {
	rows, err := r.db.Query(
		`SELECT id, email, name, role, created_at FROM users ORDER BY created_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("List users: %w", err)
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.attachMenuPermissionsToUsers(users); err != nil {
		return nil, err
	}
	return users, nil
}

func (r *UserRepo) Update(id, email, name, role string, passwordHash *string) (*models.User, error) {
	u := &models.User{}
	if passwordHash != nil {
		err := r.db.QueryRow(
			`UPDATE users
			 SET email = $2, name = $3, role = $4, password_hash = $5
			 WHERE id = $1
			 RETURNING id, email, name, role, password_hash, created_at`,
			id, email, name, role, *passwordHash,
		).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.PasswordHash, &u.CreatedAt)
		if err == sql.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("Update user: %w", err)
		}
		if err := r.attachMenuPermissions(u); err != nil {
			return nil, err
		}
		return u, nil
	}
	err := r.db.QueryRow(
		`UPDATE users
		 SET email = $2, name = $3, role = $4
		 WHERE id = $1
		 RETURNING id, email, name, role, password_hash, created_at`,
		id, email, name, role,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.PasswordHash, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("Update user: %w", err)
	}
	if err := r.attachMenuPermissions(u); err != nil {
		return nil, err
	}
	return u, nil
}

func (r *UserRepo) Delete(id string) error {
	_, err := r.db.Exec(`DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("Delete user: %w", err)
	}
	return nil
}

func (r *UserRepo) ResetMenuPermissionsToRole(userID, role, updatedBy string) ([]models.UserMenuPermission, error) {
	perms := defaultMenuPermissionsForRole(role)
	tx, err := r.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("ResetMenuPermissions begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := upsertMenuPermissionsTx(tx, userID, role, perms, updatedBy); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("ResetMenuPermissions commit: %w", err)
	}
	return perms, nil
}

func (r *UserRepo) UpdateMenuPermissions(userID, role, updatedBy string, in []models.UserMenuPermission) ([]models.UserMenuPermission, error) {
	perms := normalizeMenuPermissionsForRole(role, in)
	tx, err := r.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("UpdateMenuPermissions begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := upsertMenuPermissionsTx(tx, userID, role, perms, updatedBy); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("UpdateMenuPermissions commit: %w", err)
	}
	return perms, nil
}

func (r *UserRepo) attachMenuPermissions(u *models.User) error {
	if u == nil {
		return nil
	}
	rows, err := r.db.Query(
		`SELECT menu_key, can_view, can_create, can_update, can_delete
		 FROM user_menu_permissions
		 WHERE user_id = $1
		 ORDER BY menu_key`,
		u.ID,
	)
	if err != nil {
		return fmt.Errorf("List user menu permissions: %w", err)
	}
	defer rows.Close()

	perms, err := scanMenuPermissionRows(rows)
	if err != nil {
		return err
	}
	u.MenuPermissions = mergeMenuPermissions(u.Role, perms)
	return nil
}

func (r *UserRepo) attachMenuPermissionsToUsers(users []models.User) error {
	if len(users) == 0 {
		return nil
	}
	rows, err := r.db.Query(
		`SELECT user_id::text, menu_key, can_view, can_create, can_update, can_delete
		 FROM user_menu_permissions
		 ORDER BY user_id, menu_key`,
	)
	if err != nil {
		return fmt.Errorf("List menu permissions: %w", err)
	}
	defer rows.Close()

	byUser := map[string][]models.UserMenuPermission{}
	for rows.Next() {
		var userID string
		var p models.UserMenuPermission
		if err := rows.Scan(&userID, &p.MenuKey, &p.CanView, &p.CanCreate, &p.CanUpdate, &p.CanDelete); err != nil {
			return err
		}
		byUser[userID] = append(byUser[userID], p)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range users {
		users[i].MenuPermissions = mergeMenuPermissions(users[i].Role, byUser[users[i].ID])
	}
	return nil
}

func scanMenuPermissionRows(rows *sql.Rows) ([]models.UserMenuPermission, error) {
	var out []models.UserMenuPermission
	for rows.Next() {
		var p models.UserMenuPermission
		if err := rows.Scan(&p.MenuKey, &p.CanView, &p.CanCreate, &p.CanUpdate, &p.CanDelete); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func upsertMenuPermissionsTx(tx *sql.Tx, userID, role string, perms []models.UserMenuPermission, updatedBy string) error {
	for _, p := range normalizeMenuPermissionsForRole(role, perms) {
		var updatedByArg any = nil
		if updatedBy != "" {
			updatedByArg = updatedBy
		}
		_, err := tx.Exec(
			`INSERT INTO user_menu_permissions (
			   user_id, menu_key, can_view, can_create, can_update, can_delete, updated_by, updated_at
			 ) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
			 ON CONFLICT (user_id, menu_key) DO UPDATE
			 SET can_view = EXCLUDED.can_view,
			     can_create = EXCLUDED.can_create,
			     can_update = EXCLUDED.can_update,
			     can_delete = EXCLUDED.can_delete,
			     updated_by = EXCLUDED.updated_by,
			     updated_at = NOW()`,
			userID, p.MenuKey, p.CanView, p.CanCreate, p.CanUpdate, p.CanDelete, updatedByArg,
		)
		if err != nil {
			return fmt.Errorf("upsert menu permission %s: %w", p.MenuKey, err)
		}
	}
	return nil
}

func normalizeMenuPermissionsForRole(role string, in []models.UserMenuPermission) []models.UserMenuPermission {
	byKey := map[string]models.UserMenuPermission{}
	for _, p := range in {
		key := strings.ToLower(strings.TrimSpace(p.MenuKey))
		if !isKnownMenuKey(key) {
			continue
		}
		p.MenuKey = key
		if !p.CanView {
			p.CanCreate = false
			p.CanUpdate = false
			p.CanDelete = false
		}
		byKey[key] = p
	}

	out := defaultMenuPermissionsForRole(role)
	for i, p := range out {
		if override, ok := byKey[p.MenuKey]; ok {
			out[i] = override
		}
		if role == "admin" && out[i].MenuKey == "settings_users" {
			out[i].CanView = true
		}
		if !out[i].CanView {
			out[i].CanCreate = false
			out[i].CanUpdate = false
			out[i].CanDelete = false
		}
	}
	return out
}

func mergeMenuPermissions(role string, stored []models.UserMenuPermission) []models.UserMenuPermission {
	return normalizeMenuPermissionsForRole(role, stored)
}

func defaultMenuPermissionsForRole(role string) []models.UserMenuPermission {
	role = strings.ToLower(strings.TrimSpace(role))
	out := make([]models.UserMenuPermission, 0, len(menuPermissionDefaults))
	for _, d := range menuPermissionDefaults {
		p := models.UserMenuPermission{MenuKey: d.Key}
		switch role {
		case "admin":
			p.CanView, p.CanCreate, p.CanUpdate, p.CanDelete = d.AdminView, d.AdminCreate, d.AdminUpdate, d.AdminDelete
		case "staff":
			p.CanView, p.CanCreate, p.CanUpdate, p.CanDelete = d.StaffView, d.StaffCreate, d.StaffUpdate, d.StaffDelete
		default:
			p.CanView, p.CanCreate, p.CanUpdate, p.CanDelete = d.ViewerView, d.ViewerCreate, d.ViewerUpdate, d.ViewerDelete
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MenuKey < out[j].MenuKey })
	return out
}

func isKnownMenuKey(key string) bool {
	for _, d := range menuPermissionDefaults {
		if d.Key == key {
			return true
		}
	}
	return false
}

func (r *UserRepo) CountAdmins(exceptID string) (int, error) {
	var n int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM users WHERE role = 'admin' AND id <> $1`,
		exceptID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("CountAdmins: %w", err)
	}
	return n, nil
}
