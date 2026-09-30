package store

import (
	"context"
	"math/big"
	"time"
)

// ExitTx is one transaction of an exit job (docs/api.md §5 "Exit").
type ExitTx struct {
	Hash                    string  `json:"hash"`
	Block                   *uint64 `json:"block"`
	SentBlock               uint64  `json:"sentBlock"`
	Nonce                   uint64  `json:"nonce"`
	MaxPriorityFeePerGasWei string  `json:"maxPriorityFeePerGasWei"`
	MaxFeePerGasWei         string  `json:"maxFeePerGasWei"`
	GasLimit                uint64  `json:"gasLimit"`
	TipUSD                  float64 `json:"tipUsd"`
	AmountOut               string  `json:"amountOut"`
	Burned                  string  `json:"burned"`
	Remaining               string  `json:"remaining"`
	Result                  string  `json:"result"` // exited | deferred | reverted | pending | replaced
}

// Exit is one exit job for a (guard, target).
type Exit struct {
	ID                    int64     `json:"id"`
	Guard                 string    `json:"guard"`
	Owner                 string    `json:"owner"`
	TargetID              string    `json:"targetId"`
	Trigger               string    `json:"trigger"` // auto | manual_keeper | owner
	Reason                string    `json:"reason"`
	ReasonHash            string    `json:"reasonHash"`
	Severity              string    `json:"severity"`
	Status                string    `json:"status"` // active | complete | stopped | timeout | failed
	DecidedAt             time.Time `json:"decidedAt"`
	DecisionToBroadcastMs *int      `json:"decisionToBroadcastMs"`
	TipCapUSD             float64   `json:"tipCapUsd"`
	TipNote               string    `json:"tipNote"`
	Txs                   []ExitTx  `json:"txs"`
	TotalOut              string    `json:"totalOut"`
	StartBlock            uint64    `json:"startBlock"`
	EndBlock              *uint64   `json:"endBlock"`
	Episode               string    `json:"-"`
}

const exitCols = `id, guard, owner, target_id, trigger, reason, reason_hash, severity, status, episode, decided_at,
	decision_to_broadcast_ms, tip_cap_usd, tip_note, total_out, start_block, end_block`

type scanner interface{ Scan(dest ...any) error }

func scanExit(r scanner) (*Exit, error) {
	e := &Exit{Txs: []ExitTx{}}
	var sb int64
	var eb *int64
	err := r.Scan(&e.ID, &e.Guard, &e.Owner, &e.TargetID, &e.Trigger, &e.Reason, &e.ReasonHash, &e.Severity, &e.Status, &e.Episode,
		&e.DecidedAt, &e.DecisionToBroadcastMs, &e.TipCapUSD, &e.TipNote, &e.TotalOut, &sb, &eb)
	if err != nil {
		return nil, err
	}
	e.StartBlock = uint64(sb)
	if eb != nil {
		v := uint64(*eb)
		e.EndBlock = &v
	}
	return e, nil
}

// InsertExit creates an exit and returns its id.
func (s *Store) InsertExit(ctx context.Context, e *Exit) error {
	return s.Pool.QueryRow(ctx, `INSERT INTO exits (guard, owner, target_id, trigger, reason, reason_hash, severity, status, episode, decided_at,
		decision_to_broadcast_ms, tip_cap_usd, tip_note, total_out, start_block, end_block)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING id`,
		Lower(e.Guard), Lower(e.Owner), e.TargetID, e.Trigger, e.Reason, e.ReasonHash, e.Severity, e.Status, e.Episode, e.DecidedAt,
		e.DecisionToBroadcastMs, e.TipCapUSD, e.TipNote, e.TotalOut, int64(e.StartBlock), endBlockArg(e.EndBlock)).Scan(&e.ID)
}

func endBlockArg(v *uint64) *int64 {
	if v == nil {
		return nil
	}
	x := int64(*v)
	return &x
}

// UpdateExit saves the mutable fields of an exit.
func (s *Store) UpdateExit(ctx context.Context, e *Exit) error {
	_, err := s.Pool.Exec(ctx, `UPDATE exits SET status=$2, decision_to_broadcast_ms=$3, tip_note=$4, total_out=$5, end_block=$6 WHERE id=$1`,
		e.ID, e.Status, e.DecisionToBroadcastMs, e.TipNote, e.TotalOut, endBlockArg(e.EndBlock))
	return err
}

// InsertExitTx records a broadcast transaction.
func (s *Store) InsertExitTx(ctx context.Context, exitID int64, t ExitTx) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO exit_txs (exit_id, hash, nonce, sent_block, max_priority_fee_wei, max_fee_wei, gas_limit, tip_usd, result)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		exitID, t.Hash, int64(t.Nonce), int64(t.SentBlock), t.MaxPriorityFeePerGasWei, t.MaxFeePerGasWei, int64(t.GasLimit), t.TipUSD, t.Result)
	return err
}

// UpdateExitTx records the mined outcome of a transaction.
func (s *Store) UpdateExitTx(ctx context.Context, t ExitTx) error {
	_, err := s.Pool.Exec(ctx, `UPDATE exit_txs SET block=$2, tip_usd=$3, amount_out=$4, burned=$5, remaining=$6, result=$7 WHERE hash=$1`,
		t.Hash, endBlockArg(t.Block), t.TipUSD, t.AmountOut, t.Burned, t.Remaining, t.Result)
	return err
}

func (s *Store) exitTxs(ctx context.Context, exitID int64) ([]ExitTx, error) {
	rows, err := s.Pool.Query(ctx, `SELECT hash, block, sent_block, nonce, max_priority_fee_wei, max_fee_wei, gas_limit, tip_usd, amount_out, burned, remaining, result
		FROM exit_txs WHERE exit_id=$1 ORDER BY id`, exitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExitTx{}
	for rows.Next() {
		var t ExitTx
		var b *int64
		var sb, n, gl int64
		if err := rows.Scan(&t.Hash, &b, &sb, &n, &t.MaxPriorityFeePerGasWei, &t.MaxFeePerGasWei, &gl, &t.TipUSD, &t.AmountOut, &t.Burned, &t.Remaining, &t.Result); err != nil {
			return nil, err
		}
		t.Block = nil
		if b != nil {
			v := uint64(*b)
			t.Block = &v
		}
		t.SentBlock, t.Nonce, t.GasLimit = uint64(sb), uint64(n), uint64(gl)
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetExit returns an exit with its transactions.
func (s *Store) GetExit(ctx context.Context, id int64) (*Exit, error) {
	e, err := scanExit(s.Pool.QueryRow(ctx, `SELECT `+exitCols+` FROM exits WHERE id=$1`, id))
	if err != nil {
		return nil, notFound(err)
	}
	e.Txs, err = s.exitTxs(ctx, id)
	return e, err
}

// LastExit returns the newest exit of a guard/target (ErrNotFound if none).
func (s *Store) LastExit(ctx context.Context, guard, target string) (*Exit, error) {
	e, err := scanExit(s.Pool.QueryRow(ctx, `SELECT `+exitCols+` FROM exits WHERE guard=$1 AND target_id=$2 ORDER BY id DESC LIMIT 1`, Lower(guard), target))
	if err != nil {
		return nil, notFound(err)
	}
	e.Txs, err = s.exitTxs(ctx, e.ID)
	return e, err
}

// ExitsFor returns all exits of a guard/target, oldest first.
func (s *Store) ExitsFor(ctx context.Context, guard, target string) ([]*Exit, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+exitCols+` FROM exits WHERE guard=$1 AND target_id=$2 ORDER BY id`, Lower(guard), target)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Exit
	for rows.Next() {
		e, err := scanExit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SumReturned adds up totalOut over every exit of a guard/target.
func (s *Store) SumReturned(ctx context.Context, guard, target string) (*big.Int, error) {
	exits, err := s.ExitsFor(ctx, guard, target)
	if err != nil {
		return nil, err
	}
	sum := new(big.Int)
	for _, e := range exits {
		if v, ok := new(big.Int).SetString(e.TotalOut, 10); ok {
			sum.Add(sum, v)
		}
	}
	return sum, nil
}

// HasExitInEpisode reports whether an exit already exists for the guard/target in an episode.
func (s *Store) HasExitInEpisode(ctx context.Context, guard, target, episode string) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM exits WHERE guard=$1 AND target_id=$2 AND episode=$3)`, Lower(guard), target, episode).Scan(&ok)
	return ok, err
}

// ExitIDByTx finds the exit a transaction hash belongs to (0 when unknown).
func (s *Store) ExitIDByTx(ctx context.Context, hash string) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx, `SELECT exit_id FROM exit_txs WHERE hash=$1`, hash).Scan(&id)
	if err != nil {
		if notFound(err) == ErrNotFound {
			return 0, nil
		}
		return 0, err
	}
	return id, nil
}

// MarkActiveExitsFailed closes exits left "active" by a previous process (the retry loop is in memory).
func (s *Store) MarkStaleActiveExits(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, `UPDATE exits SET status='stopped' WHERE status='active'`)
	return err
}

// ListExits returns exits newest first; guard and target are optional filters.
func (s *Store) ListExits(ctx context.Context, guard, target string, limit int) ([]*Exit, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+exitCols+` FROM exits
		WHERE ($1 = '' OR guard = $1) AND ($2 = '' OR target_id = $2) ORDER BY id DESC LIMIT $3`, Lower(guard), target, limit)
	if err != nil {
		return nil, err
	}
	var out []*Exit
	for rows.Next() {
		e, err := scanExit(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, e := range out {
		if e.Txs, err = s.exitTxs(ctx, e.ID); err != nil {
			return nil, err
		}
	}
	if out == nil {
		out = []*Exit{}
	}
	return out, nil
}
