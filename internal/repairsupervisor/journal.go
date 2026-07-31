package repairsupervisor

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/yingliang-zhang/ananke/internal/repaircontract"
	"github.com/yingliang-zhang/ananke/internal/transportprimitives"
)

// ErrJournalConflict is returned when a claim with the same (attempt_hash,
// phase) tuple already exists.
var ErrJournalConflict = errors.New("supervisor journal conflict")

// ErrJournalNotFound is returned when no entry exists for a query.
var ErrJournalNotFound = errors.New("supervisor journal entry not found")

// ErrJournalPredecessorMissing is returned when a claim's predecessor
// is not found in the journal.
var ErrJournalPredecessorMissing = errors.New("supervisor journal predecessor missing")

// BootEpoch records a supervisor boot epoch. Each supervisor start creates a
// new boot epoch. Prior-epoch nonterminal claims become waiting_for_human.
type BootEpoch struct {
	BootEpochID   string
	BootEpochHash string
	StartedAt     time.Time
}

// ClaimEntry is a stored phase claim in the supervisor journal.
type ClaimEntry struct {
	ClaimHash              string
	ClaimID                string
	Phase                  repaircontract.SupervisorIntentPhase
	Sequence               int
	AttemptHash            string
	AttemptNumber          int
	AttemptCap             int
	BootEpochID            string
	BootEpochHash          string
	JournalHeadHash        string
	JournalPredecessorHash string
	ClaimJSON              string
	Launched               bool
	TerminalStatus         string // "" if not terminal, "waiting_for_human" if prior-epoch nonterminal
	CreatedAt              time.Time
}

// Journal is the supervisor FULL-sync journal. It uses SQLite with FULL
// journal mode and fullfsync for crash-safe durability.
type Journal struct {
	db *sql.DB
}

// Open creates or opens a supervisor journal at the given path. The journal
// uses FULL journal mode and fullfsync for maximum durability.
func Open(path string) (*Journal, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(FULL)&_pragma=synchronous(FULL)")
	if err != nil {
		return nil, fmt.Errorf("open supervisor journal: %w", err)
	}
	j := &Journal{db: db}
	if err := j.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return j, nil
}

// Close closes the journal database.
func (j *Journal) Close() error {
	return j.db.Close()
}

func (j *Journal) migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS supervisor_boot_epochs (
			boot_epoch_id TEXT PRIMARY KEY,
			boot_epoch_hash TEXT NOT NULL UNIQUE,
			started_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS supervisor_claims (
			claim_hash TEXT PRIMARY KEY,
			claim_id TEXT NOT NULL,
			phase TEXT NOT NULL CHECK (phase IN ('materialization_claim', 'adapter_claim', 'test_claim')),
			sequence INTEGER NOT NULL CHECK (sequence >= 1 AND sequence <= 3),
			attempt_hash TEXT NOT NULL,
			attempt_number INTEGER NOT NULL CHECK (attempt_number >= 1),
			attempt_cap INTEGER NOT NULL,
			boot_epoch_id TEXT NOT NULL,
			boot_epoch_hash TEXT NOT NULL,
			journal_head_hash TEXT NOT NULL,
			journal_predecessor_hash TEXT NOT NULL,
			claim_json TEXT NOT NULL,
			launched INTEGER NOT NULL DEFAULT 0,
			terminal_status TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			UNIQUE (attempt_hash, phase),
			UNIQUE (attempt_hash, sequence),
			UNIQUE (claim_id),
			FOREIGN KEY (boot_epoch_id) REFERENCES supervisor_boot_epochs(boot_epoch_id)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE INDEX IF NOT EXISTS idx_supervisor_claims_attempt
			ON supervisor_claims (attempt_hash, sequence)`,
		`CREATE INDEX IF NOT EXISTS idx_supervisor_claims_boot_epoch
			ON supervisor_claims (boot_epoch_id, terminal_status)`,
		`CREATE TRIGGER IF NOT EXISTS supervisor_claims_insert_only_update
			BEFORE UPDATE ON supervisor_claims
			BEGIN SELECT RAISE(ABORT, 'supervisor claims are immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS supervisor_claims_insert_only_delete
			BEFORE DELETE ON supervisor_claims
			BEGIN SELECT RAISE(ABORT, 'supervisor claims are immutable'); END`,
		`CREATE TABLE IF NOT EXISTS supervisor_terminal_events (
			terminal_event_hash TEXT PRIMARY KEY,
			claim_hash TEXT NOT NULL UNIQUE,
			phase TEXT NOT NULL,
			sequence INTEGER NOT NULL,
			attempt_hash TEXT NOT NULL,
			boot_epoch_id TEXT NOT NULL,
			boot_epoch_hash TEXT NOT NULL,
			terminal_status TEXT NOT NULL,
			terminal_event_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY (claim_hash) REFERENCES supervisor_claims(claim_hash)
				DEFERRABLE INITIALLY DEFERRED
		)`,
		`CREATE TRIGGER IF NOT EXISTS supervisor_terminal_events_insert_only_update
			BEFORE UPDATE ON supervisor_terminal_events
			BEGIN SELECT RAISE(ABORT, 'supervisor terminal events are immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS supervisor_terminal_events_insert_only_delete
			BEFORE DELETE ON supervisor_terminal_events
			BEGIN SELECT RAISE(ABORT, 'supervisor terminal events are immutable'); END`,
	}
	for _, stmt := range statements {
		if _, err := j.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate supervisor journal: %w", err)
		}
	}
	return nil
}

// RecordBootEpoch records a new supervisor boot epoch.
func (j *Journal) RecordBootEpoch(ctx context.Context, epoch BootEpoch) error {
	_, err := j.db.ExecContext(ctx,
		`INSERT INTO supervisor_boot_epochs (boot_epoch_id, boot_epoch_hash, started_at)
		VALUES (?, ?, ?)`,
		epoch.BootEpochID, epoch.BootEpochHash, epoch.StartedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("record boot epoch: %w", err)
	}
	return nil
}

// ClaimPhase records a phase claim in the journal with at-most-once semantics.
// If a claim with the same (attempt_hash, phase) already exists, it returns
// the existing claim (idempotent) or ErrJournalConflict if the content differs.
// The predecessor claim (sequence-1) must exist unless this is sequence 1.
func (j *Journal) ClaimPhase(ctx context.Context, claim repaircontract.SupervisorIntentClaim, bootEpochID, bootEpochHash string) (ClaimEntry, error) {
	// Validate the claim structurally.
	if claim.ClaimHash == "" || claim.ClaimID == "" || claim.AttemptHash == "" {
		return ClaimEntry{}, fmt.Errorf("%w: empty required field", ErrJournalConflict)
	}
	if claim.Sequence < 1 || claim.Sequence > 3 {
		return ClaimEntry{}, fmt.Errorf("%w: invalid sequence %d", ErrJournalConflict, claim.Sequence)
	}

	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return ClaimEntry{}, err
	}
	defer func() { _ = tx.Rollback() }()

	// Check for existing claim with same (attempt_hash, phase).
	existing, found, err := loadClaimByAttemptPhase(ctx, tx, claim.AttemptHash, string(claim.Phase))
	if err != nil {
		return ClaimEntry{}, err
	}
	if found {
		// Idempotent replay.
		claimJSON, _ := transportprimitives.MarshalCanonical(claim)
		if existing.ClaimJSON != string(claimJSON) {
			return ClaimEntry{}, fmt.Errorf("%w: (attempt_hash=%s, phase=%s)", ErrJournalConflict, claim.AttemptHash, claim.Phase)
		}
		return existing, nil
	}

	// Check predecessor (sequence-1 must exist unless sequence 1).
	if claim.Sequence > 1 {
		predPhase := phaseBySequence(claim.Sequence - 1)
		_, predFound, err := loadClaimByAttemptPhase(ctx, tx, claim.AttemptHash, string(predPhase))
		if err != nil {
			return ClaimEntry{}, err
		}
		if !predFound {
			return ClaimEntry{}, fmt.Errorf("%w: sequence %d requires predecessor %s", ErrJournalPredecessorMissing, claim.Sequence, predPhase)
		}
	}

	// Compute journal head hash (hash of the claim itself).
	claimJSON, err := transportprimitives.MarshalCanonical(claim)
	if err != nil {
		return ClaimEntry{}, fmt.Errorf("marshal claim: %w", err)
	}

	// Get the predecessor's journal head hash for chaining.
	journalPredecessorHash := ""
	if claim.Sequence > 1 {
		predPhase := phaseBySequence(claim.Sequence - 1)
		pred, _, err := loadClaimByAttemptPhase(ctx, tx, claim.AttemptHash, string(predPhase))
		if err != nil {
			return ClaimEntry{}, err
		}
		journalPredecessorHash = pred.JournalHeadHash
	}

	// Journal head hash: hash of (predecessor_hash + claim_hash).
	journalHeadHash := computeJournalHeadHash(journalPredecessorHash, claim.ClaimHash)

	createdAt := time.Now().UTC().Format(time.RFC3339Nano)

	_, err = tx.ExecContext(ctx,
		`INSERT INTO supervisor_claims
		(claim_hash, claim_id, phase, sequence, attempt_hash, attempt_number, attempt_cap,
		 boot_epoch_id, boot_epoch_hash, journal_head_hash, journal_predecessor_hash,
		 claim_json, launched, terminal_status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, '', ?)`,
		claim.ClaimHash, claim.ClaimID, string(claim.Phase), claim.Sequence,
		claim.AttemptHash, claim.AttemptNumber, claim.AttemptCap,
		bootEpochID, bootEpochHash, journalHeadHash, journalPredecessorHash,
		string(claimJSON), createdAt)
	if err != nil {
		return ClaimEntry{}, fmt.Errorf("insert claim: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return ClaimEntry{}, err
	}

	return ClaimEntry{
		ClaimHash:              claim.ClaimHash,
		ClaimID:                claim.ClaimID,
		Phase:                  claim.Phase,
		Sequence:               claim.Sequence,
		AttemptHash:            claim.AttemptHash,
		AttemptNumber:          claim.AttemptNumber,
		AttemptCap:             claim.AttemptCap,
		BootEpochID:            bootEpochID,
		BootEpochHash:          bootEpochHash,
		JournalHeadHash:        journalHeadHash,
		JournalPredecessorHash: journalPredecessorHash,
		ClaimJSON:              string(claimJSON),
		Launched:               false,
		TerminalStatus:         "",
		CreatedAt:              parseTime(createdAt),
	}, nil
}

// MarkLaunched records that a phase launch has occurred. This is a separate
// table entry (terminal event) rather than an update to the claim, since
// claims are immutable.
func (j *Journal) MarkLaunched(ctx context.Context, claimHash, terminalEventHash, terminalStatus string, claim repaircontract.SupervisorIntentClaim, bootEpochID, bootEpochHash string) error {
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Check if terminal event already exists (idempotent).
	var existing string
	err = tx.QueryRowContext(ctx,
		`SELECT terminal_event_hash FROM supervisor_terminal_events WHERE claim_hash = ?`,
		claimHash).Scan(&existing)
	if err == nil {
		// Already recorded.
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	terminalJSON, err := transportprimitives.MarshalCanonical(map[string]any{
		"terminal_event_hash": terminalEventHash,
		"claim_hash":          claimHash,
		"phase":               string(claim.Phase),
		"sequence":            claim.Sequence,
		"attempt_hash":        claim.AttemptHash,
		"boot_epoch_id":       bootEpochID,
		"terminal_status":     terminalStatus,
	})
	if err != nil {
		return fmt.Errorf("marshal terminal event: %w", err)
	}

	createdAt := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx,
		`INSERT INTO supervisor_terminal_events
		(terminal_event_hash, claim_hash, phase, sequence, attempt_hash,
		 boot_epoch_id, boot_epoch_hash, terminal_status, terminal_event_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		terminalEventHash, claimHash, string(claim.Phase), claim.Sequence,
		claim.AttemptHash, bootEpochID, bootEpochHash, terminalStatus,
		string(terminalJSON), createdAt)
	if err != nil {
		return fmt.Errorf("insert terminal event: %w", err)
	}

	return tx.Commit()
}

// GetClaim retrieves a claim by its hash.
func (j *Journal) GetClaim(ctx context.Context, claimHash string) (ClaimEntry, error) {
	entry, found, err := loadClaimByHash(ctx, j.db, claimHash)
	if err != nil {
		return ClaimEntry{}, err
	}
	if !found {
		return ClaimEntry{}, ErrJournalNotFound
	}
	return entry, nil
}

// GetClaimByAttemptPhase retrieves a claim by (attempt_hash, phase).
func (j *Journal) GetClaimByAttemptPhase(ctx context.Context, attemptHash string, phase repaircontract.SupervisorIntentPhase) (ClaimEntry, error) {
	entry, found, err := loadClaimByAttemptPhase(ctx, j.db, attemptHash, string(phase))
	if err != nil {
		return ClaimEntry{}, err
	}
	if !found {
		return ClaimEntry{}, ErrJournalNotFound
	}
	return entry, nil
}

// GetClaimsForAttempt returns all claims for an attempt, ordered by sequence.
func (j *Journal) GetClaimsForAttempt(ctx context.Context, attemptHash string) ([]ClaimEntry, error) {
	rows, err := j.db.QueryContext(ctx,
		`SELECT claim_hash, claim_id, phase, sequence, attempt_hash, attempt_number, attempt_cap,
			boot_epoch_id, boot_epoch_hash, journal_head_hash, journal_predecessor_hash,
			claim_json, launched, terminal_status, created_at
		FROM supervisor_claims WHERE attempt_hash = ? ORDER BY sequence`,
		attemptHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ClaimEntry
	for rows.Next() {
		entry, err := scanClaim(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	return result, rows.Err()
}

// MarkPriorEpochNonterminal marks all nonterminal claims from prior boot
// epochs as waiting_for_human. This is called when a new boot epoch starts.
// Since claims are immutable (insert-only triggers), this creates terminal
// events rather than updating the claims.
func (j *Journal) MarkPriorEpochNonterminal(ctx context.Context, currentBootEpochID string) error {
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Find all nonterminal claims from prior epochs (no terminal event).
	rows, err := tx.QueryContext(ctx,
		`SELECT c.claim_hash, c.claim_id, c.phase, c.sequence, c.attempt_hash,
			c.attempt_number, c.attempt_cap, c.boot_epoch_id, c.boot_epoch_hash,
			c.journal_head_hash, c.claim_json, c.created_at
		FROM supervisor_claims c
		LEFT JOIN supervisor_terminal_events t ON c.claim_hash = t.claim_hash
		WHERE c.boot_epoch_id != ? AND t.claim_hash IS NULL`,
		currentBootEpochID)
	if err != nil {
		return fmt.Errorf("query prior epoch nonterminal: %w", err)
	}

	type pendingClaim struct {
		claimHash, claimID, phase, attemptHash string
		sequence, attemptNumber, attemptCap    int
		bootEpochID, bootEpochHash             string
		journalHeadHash, claimJSON, createdAt  string
	}
	var pending []pendingClaim
	for rows.Next() {
		var pc pendingClaim
		if err := rows.Scan(&pc.claimHash, &pc.claimID, &pc.phase, &pc.sequence,
			&pc.attemptHash, &pc.attemptNumber, &pc.attemptCap,
			&pc.bootEpochID, &pc.bootEpochHash, &pc.journalHeadHash,
			&pc.claimJSON, &pc.createdAt); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, pc)
	}
	rows.Close()

	for _, pc := range pending {
		terminalHash := computeJournalHeadHash(pc.journalHeadHash, "waiting_for_human")
		terminalJSON, _ := transportprimitives.MarshalCanonical(map[string]any{
			"terminal_event_hash": terminalHash,
			"claim_hash":          pc.claimHash,
			"phase":               pc.phase,
			"sequence":            pc.sequence,
			"attempt_hash":        pc.attemptHash,
			"boot_epoch_id":       pc.bootEpochID,
			"terminal_status":     "waiting_for_human",
		})
		_, err := tx.ExecContext(ctx,
			`INSERT INTO supervisor_terminal_events
			(terminal_event_hash, claim_hash, phase, sequence, attempt_hash,
			 boot_epoch_id, boot_epoch_hash, terminal_status, terminal_event_json, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			terminalHash, pc.claimHash, pc.phase, pc.sequence, pc.attemptHash,
			pc.bootEpochID, pc.bootEpochHash, "waiting_for_human",
			string(terminalJSON), time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			return fmt.Errorf("insert waiting_for_human terminal: %w", err)
		}
	}

	return tx.Commit()
}

// --- helpers ---

func phaseBySequence(seq int) repaircontract.SupervisorIntentPhase {
	switch seq {
	case 1:
		return repaircontract.MaterializationClaimPhase
	case 2:
		return repaircontract.AdapterClaimPhase
	case 3:
		return repaircontract.TestClaimPhase
	default:
		return ""
	}
}

func computeJournalHeadHash(predecessorHash, claimHash string) string {
	combined := predecessorHash + ":" + claimHash
	h := sha256.Sum256([]byte(combined))
	return "sha256:" + hex.EncodeToString(h[:])
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func loadClaimByHash(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, claimHash string) (ClaimEntry, bool, error) {
	row := queryer.QueryRowContext(ctx,
		`SELECT claim_hash, claim_id, phase, sequence, attempt_hash, attempt_number, attempt_cap,
			boot_epoch_id, boot_epoch_hash, journal_head_hash, journal_predecessor_hash,
			claim_json, launched, terminal_status, created_at
		FROM supervisor_claims WHERE claim_hash = ?`, claimHash)
	return scanClaimRow(row)
}

func loadClaimByAttemptPhase(ctx context.Context, queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, attemptHash, phase string) (ClaimEntry, bool, error) {
	row := queryer.QueryRowContext(ctx,
		`SELECT claim_hash, claim_id, phase, sequence, attempt_hash, attempt_number, attempt_cap,
			boot_epoch_id, boot_epoch_hash, journal_head_hash, journal_predecessor_hash,
			claim_json, launched, terminal_status, created_at
		FROM supervisor_claims WHERE attempt_hash = ? AND phase = ?`, attemptHash, phase)
	return scanClaimRow(row)
}

func scanClaim(rows *sql.Rows) (ClaimEntry, error) {
	var e ClaimEntry
	var phase, createdAtStr string
	var launched int
	if err := rows.Scan(
		&e.ClaimHash, &e.ClaimID, &phase, &e.Sequence,
		&e.AttemptHash, &e.AttemptNumber, &e.AttemptCap,
		&e.BootEpochID, &e.BootEpochHash, &e.JournalHeadHash, &e.JournalPredecessorHash,
		&e.ClaimJSON, &launched, &e.TerminalStatus, &createdAtStr); err != nil {
		return ClaimEntry{}, err
	}
	e.Phase = repaircontract.SupervisorIntentPhase(phase)
	e.Launched = launched != 0
	e.CreatedAt = parseTime(createdAtStr)
	return e, nil
}

func scanClaimRow(row *sql.Row) (ClaimEntry, bool, error) {
	var e ClaimEntry
	var phase, createdAtStr string
	var launched int
	err := row.Scan(
		&e.ClaimHash, &e.ClaimID, &phase, &e.Sequence,
		&e.AttemptHash, &e.AttemptNumber, &e.AttemptCap,
		&e.BootEpochID, &e.BootEpochHash, &e.JournalHeadHash, &e.JournalPredecessorHash,
		&e.ClaimJSON, &launched, &e.TerminalStatus, &createdAtStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ClaimEntry{}, false, nil
		}
		return ClaimEntry{}, false, err
	}
	e.Phase = repaircontract.SupervisorIntentPhase(phase)
	e.Launched = launched != 0
	e.CreatedAt = parseTime(createdAtStr)
	return e, true, nil
}
