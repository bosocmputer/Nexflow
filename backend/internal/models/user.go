package models

import (
	"time"
)

type User struct {
	ID              string               `json:"id" db:"id"`
	Email           string               `json:"email" db:"email"`
	Name            string               `json:"name" db:"name"`
	Role            string               `json:"role" db:"role"`
	PasswordHash    string               `json:"-" db:"password_hash"`
	CreatedAt       time.Time            `json:"created_at" db:"created_at"`
	MenuPermissions []UserMenuPermission `json:"menu_permissions,omitempty"`
}

type UserMenuPermission struct {
	MenuKey   string `json:"menu_key" db:"menu_key"`
	CanView   bool   `json:"can_view" db:"can_view"`
	CanCreate bool   `json:"can_create" db:"can_create"`
	CanUpdate bool   `json:"can_update" db:"can_update"`
	CanDelete bool   `json:"can_delete" db:"can_delete"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

type LoginResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

type UserUpsertRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Name     string `json:"name" binding:"required"`
	Role     string `json:"role" binding:"required"`
	Password string `json:"password,omitempty"`
}

type UserMenuPermissionsUpdateRequest struct {
	Permissions []UserMenuPermission `json:"permissions" binding:"required"`
}
