package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// User represents the persisted user entity.
type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// ErrUserNotFound is returned when no rows match the requested id.
var ErrUserNotFound = errors.New("user not found")

// Store encapsulates database access.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore connects to PostgreSQL using the provided DSN.
func NewStore(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	return &Store{pool: pool}, nil
}

// Close releases the connection pool resources.
func (s *Store) Close() {
	if s == nil || s.pool == nil {
		return
	}
	s.pool.Close()
}

// Init ensures schema exists and seeds baseline data.
func (s *Store) Init(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return errors.New("store not initialized")
	}

	ddl := `
CREATE TABLE IF NOT EXISTS users (
    id   INTEGER PRIMARY KEY,
    name TEXT NOT NULL
)`

	if _, err := s.pool.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("create table: %w", err)
	}

	seed := []User{
		{ID: 1, Name: "Ada Lovelace"},
		{ID: 2, Name: "Grace Hopper"},
		{ID: 3, Name: "Alan Turing"},
	}

	for _, u := range seed {
		if _, err := s.pool.Exec(ctx, `INSERT INTO users (id, name) VALUES ($1, $2) ON CONFLICT (id) DO NOTHING`, u.ID, u.Name); err != nil {
			return fmt.Errorf("seed user %d: %w", u.ID, err)
		}
	}

	return nil
}

// GetUser fetches a user by id.
func (s *Store) GetUser(ctx context.Context, id int) (User, error) {
	if s == nil || s.pool == nil {
		return User{}, errors.New("store not initialized")
	}

	var user User
	err := s.pool.QueryRow(ctx, `SELECT id, name FROM users WHERE id = $1`, id).Scan(&user.ID, &user.Name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrUserNotFound
		}
		return User{}, err
	}

	return user, nil
}

// RefreshUser updates the user's name with a timestamp suffix to simulate refreshing data.
func (s *Store) RefreshUser(ctx context.Context, id int) (User, error) {
	if s == nil || s.pool == nil {
		return User{}, errors.New("store not initialized")
	}

	refreshedAt := time.Now().Format(time.RFC3339)
	row := s.pool.QueryRow(ctx, `
        UPDATE users
           SET name = CONCAT(name, ' (refreshed at ', $2, ')')
         WHERE id = $1
         RETURNING id, name
    `, id, refreshedAt)

	var user User
	if err := row.Scan(&user.ID, &user.Name); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrUserNotFound
		}
		return User{}, err
	}

	return user, nil
}
