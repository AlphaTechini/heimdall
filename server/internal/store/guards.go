package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Guard is a known HeimdallGuard.
type Guard struct {
	Address      string
	Owner        string
	CreatedBlock uint64
}

// UpsertGuard records a guard (idempotent).
func (s *Store) UpsertGuard(ctx context.Context, g Guard) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO guards (address, owner, created_block) VALUES ($1,$2,$3)
		ON CONFLICT (address) DO UPDATE SET created_block = CASE WHEN guards.created_block = 0 THEN EXCLUDED.created_block ELSE guards.created_block END`,
		Lower(g.Address), Lower(g.Owner), int64(g.CreatedBlock))
	return err
}

// GetGuard returns a guard by address.
func (s *Store) GetGuard(ctx context.Context, addr string) (*Guard, error) {
	g := &Guard{}
	var b int64
	err := s.Pool.QueryRow(ctx, `SELECT address, owner, created_block FROM guards WHERE address=$1`, Lower(addr)).Scan(&g.Address, &g.Owner, &b)
	g.CreatedBlock = uint64(b)
	return g, notFound(err)
}

// GuardByOwner returns the guard of an owner.
func (s *Store) GuardByOwner(ctx context.Context, owner string) (*Guard, error) {
	g := &Guard{}
	var b int64
	err := s.Pool.QueryRow(ctx, `SELECT address, owner, created_block FROM guards WHERE owner=$1`, Lower(owner)).Scan(&g.Address, &g.Owner, &b)
	g.CreatedBlock = uint64(b)
	return g, notFound(err)
}

// ListGuards returns all known guards.
func (s *Store) ListGuards(ctx context.Context) ([]Guard, error) {
	rows, err := s.Pool.Query(ctx, `SELECT address, owner, created_block FROM guards ORDER BY created_block, address`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Guard
	for rows.Next() {
		var g Guard
		var b int64
		if err := rows.Scan(&g.Address, &g.Owner, &b); err != nil {
			return nil, err
		}
		g.CreatedBlock = uint64(b)
		out = append(out, g)
	}
	return out, rows.Err()
}

// GetPolicy returns the stored policy JSON for a guard/target (nil when none).
func (s *Store) GetPolicy(ctx context.Context, guard, target string) (json.RawMessage, error) {
	var p []byte
	err := s.Pool.QueryRow(ctx, `SELECT policy FROM policies WHERE guard=$1 AND target_id=$2`, Lower(guard), target).Scan(&p)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// PutPolicy saves a policy.
func (s *Store) PutPolicy(ctx context.Context, guard, target string, p json.RawMessage) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO policies (guard,target_id,policy) VALUES ($1,$2,$3)
		ON CONFLICT (guard,target_id) DO UPDATE SET policy=EXCLUDED.policy, updated_at=now()`, Lower(guard), target, []byte(p))
	return err
}
