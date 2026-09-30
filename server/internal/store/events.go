package store

import (
	"context"
	"encoding/json"
	"time"
)

// Event is one row of the activity feed (docs/api.md §5 "Event").
type Event struct {
	ID       int64     `json:"id"`
	Time     time.Time `json:"time"`
	Block    uint64    `json:"block"`
	Kind     string    `json:"kind"`
	TargetID *string   `json:"targetId"`
	Guard    *string   `json:"guard"`
	Severity *string   `json:"severity"`
	Message  string    `json:"message"`
	TxHash   *string   `json:"txHash"`
	ExitID   *int64    `json:"exitId"`
	Owner    string    `json:"-"`
}

func nz(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// InsertEvent stores an event and returns it with its id and time filled in.
func (s *Store) InsertEvent(ctx context.Context, e Event) (Event, error) {
	var tgt, guard, sev, tx string
	if e.TargetID != nil {
		tgt = *e.TargetID
	}
	if e.Guard != nil {
		guard = Lower(*e.Guard)
	}
	if e.Severity != nil {
		sev = *e.Severity
	}
	if e.TxHash != nil {
		tx = *e.TxHash
	}
	err := s.Pool.QueryRow(ctx, `INSERT INTO events (block,kind,target_id,guard,owner,severity,message,tx_hash,exit_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id, time`,
		int64(e.Block), e.Kind, tgt, guard, Lower(e.Owner), sev, e.Message, tx, e.ExitID).Scan(&e.ID, &e.Time)
	return e, err
}

// Events returns the newest events. With owner set, it returns that owner's events plus the
// target-wide ones (no owner); without owner it returns the global feed.
func (s *Store) Events(ctx context.Context, owner, target string, limit int) ([]Event, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, time, block, kind, target_id, guard, severity, message, tx_hash, exit_id, owner
		FROM events
		WHERE ($1 = '' OR owner = $1 OR owner = '') AND ($2 = '' OR target_id = $2)
		ORDER BY id DESC LIMIT $3`, Lower(owner), target, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var b int64
		var tgt, guard, sev, tx string
		if err := rows.Scan(&e.ID, &e.Time, &b, &e.Kind, &tgt, &guard, &sev, &e.Message, &tx, &e.ExitID, &e.Owner); err != nil {
			return nil, err
		}
		e.Block = uint64(b)
		e.TargetID, e.Guard, e.Severity, e.TxHash = nz(tgt), nz(guard), nz(sev), nz(tx)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Snapshot is one stored signal check.
type Snapshot struct {
	Block    uint64          `json:"block"`
	Time     time.Time       `json:"time"`
	Severity string          `json:"severity"`
	Signals  json.RawMessage `json:"signals"`
}

// InsertSnapshot stores a signal snapshot.
func (s *Store) InsertSnapshot(ctx context.Context, target string, sn Snapshot) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO signal_snapshots (target_id, block, time, severity, signals) VALUES ($1,$2,$3,$4,$5)`,
		target, int64(sn.Block), sn.Time, sn.Severity, []byte(sn.Signals))
	return err
}

// Snapshots returns the newest snapshots, oldest first.
func (s *Store) Snapshots(ctx context.Context, target string, limit int) ([]Snapshot, error) {
	rows, err := s.Pool.Query(ctx, `SELECT block, time, severity, signals FROM (
		SELECT id, block, time, severity, signals FROM signal_snapshots WHERE target_id=$1 ORDER BY id DESC LIMIT $2) t ORDER BY id`, target, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Snapshot{}
	for rows.Next() {
		var sn Snapshot
		var b int64
		var raw []byte
		if err := rows.Scan(&b, &sn.Time, &sn.Severity, &raw); err != nil {
			return nil, err
		}
		sn.Block, sn.Signals = uint64(b), raw
		out = append(out, sn)
	}
	return out, rows.Err()
}

// Incident is a stored severity change.
type Incident struct {
	ID           int64
	TargetID     string
	Block        uint64
	Time         time.Time
	FromSeverity string
	Severity     string
	Reason       string
	Inputs       json.RawMessage
}

// InsertIncident stores a severity change with its inputs (specs W3) and returns its id.
func (s *Store) InsertIncident(ctx context.Context, in Incident) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx, `INSERT INTO incidents (target_id, block, time, from_severity, severity, reason, inputs)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		in.TargetID, int64(in.Block), in.Time, in.FromSeverity, in.Severity, in.Reason, []byte(in.Inputs)).Scan(&id)
	return id, err
}

// Incidents returns incidents at or after a block, oldest first.
func (s *Store) IncidentsSince(ctx context.Context, target string, block uint64) ([]Incident, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, target_id, block, time, from_severity, severity, reason FROM incidents
		WHERE target_id=$1 AND block >= $2 ORDER BY id`, target, int64(block))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Incident
	for rows.Next() {
		var in Incident
		var b int64
		if err := rows.Scan(&in.ID, &in.TargetID, &b, &in.Time, &in.FromSeverity, &in.Severity, &in.Reason); err != nil {
			return nil, err
		}
		in.Block = uint64(b)
		out = append(out, in)
	}
	return out, rows.Err()
}

// Notification rows.
type Notification struct {
	ID      int64
	Owner   string
	Channel string
	Kind    string
	Subject string
	Body    string
}

// InsertNotification stores a queued notification.
func (s *Store) InsertNotification(ctx context.Context, n Notification) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx, `INSERT INTO notifications (owner,channel,kind,subject,body,status) VALUES ($1,$2,$3,$4,$5,'queued') RETURNING id`,
		Lower(n.Owner), n.Channel, n.Kind, n.Subject, n.Body).Scan(&id)
	return id, err
}

// UpdateNotification records the outcome of a send attempt.
func (s *Store) UpdateNotification(ctx context.Context, id int64, status string, attempts int, errMsg string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE notifications SET status=$2, attempts=$3, error=$4, sent_at = CASE WHEN $2='sent' THEN now() ELSE sent_at END WHERE id=$1`,
		id, status, attempts, errMsg)
	return err
}

// SimRun is one simulator run.
type SimRun struct {
	ID                 int64
	Scenario           string
	TargetID           string
	Status             string
	StartBlock         uint64
	DrainFinishedBlock *uint64
	EndBlock           *uint64
}

func (s *Store) InsertSimRun(ctx context.Context, scenario, target string, startBlock uint64) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx, `INSERT INTO sim_runs (scenario,target_id,status,start_block) VALUES ($1,$2,'running',$3) RETURNING id`,
		scenario, target, int64(startBlock)).Scan(&id)
	return id, err
}

func (s *Store) FinishSimRun(ctx context.Context, id int64, status string, drainFinished, end uint64) error {
	_, err := s.Pool.Exec(ctx, `UPDATE sim_runs SET status=$2, drain_finished_block=$3, end_block=$4 WHERE id=$1`, id, status, int64(drainFinished), int64(end))
	return err
}

// LatestSimRun returns the newest run of a scenario.
func (s *Store) LatestSimRun(ctx context.Context, scenario string) (*SimRun, error) {
	r := &SimRun{}
	var sb int64
	var df, eb *int64
	err := s.Pool.QueryRow(ctx, `SELECT id, scenario, target_id, status, start_block, drain_finished_block, end_block FROM sim_runs
		WHERE scenario=$1 ORDER BY id DESC LIMIT 1`, scenario).Scan(&r.ID, &r.Scenario, &r.TargetID, &r.Status, &sb, &df, &eb)
	if err != nil {
		return nil, notFound(err)
	}
	r.StartBlock = uint64(sb)
	if df != nil {
		v := uint64(*df)
		r.DrainFinishedBlock = &v
	}
	if eb != nil {
		v := uint64(*eb)
		r.EndBlock = &v
	}
	return r, nil
}

// CleanupAfterBlock removes rows created after a simulator snapshot block (used by /sim/reset).
func (s *Store) CleanupAfterBlock(ctx context.Context, block uint64) error {
	b := int64(block)
	stmts := []string{
		`DELETE FROM exit_txs WHERE exit_id IN (SELECT id FROM exits WHERE start_block > $1)`,
		`DELETE FROM exits WHERE start_block > $1`,
		`DELETE FROM events WHERE block > $1`,
		`DELETE FROM signal_snapshots WHERE block > $1`,
		`DELETE FROM incidents WHERE block > $1`,
		`DELETE FROM policies WHERE guard IN (SELECT address FROM guards WHERE created_block > $1)`,
		`DELETE FROM guards WHERE created_block > $1`,
		`DELETE FROM sim_runs WHERE start_block > $1`,
	}
	for _, q := range stmts {
		if _, err := s.Pool.Exec(ctx, q, b); err != nil {
			return err
		}
	}
	return nil
}
