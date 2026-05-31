package repository

import (
	"database/sql"
	"fmt"

	"nexflow/internal/models"
)

type UserRepo struct {
	db *sql.DB
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
	return u, nil
}

func (r *UserRepo) Create(email, name, role, passwordHash string) (*models.User, error) {
	u := &models.User{}
	err := r.db.QueryRow(
		`INSERT INTO users (email, name, role, password_hash)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, email, name, role, password_hash, created_at`,
		email, name, role, passwordHash,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.PasswordHash, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("Create user: %w", err)
	}
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
	return users, rows.Err()
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
	return u, nil
}

func (r *UserRepo) Delete(id string) error {
	_, err := r.db.Exec(`DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("Delete user: %w", err)
	}
	return nil
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
