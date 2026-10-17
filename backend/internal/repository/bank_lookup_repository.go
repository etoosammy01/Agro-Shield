package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrNoBankLookup means there is no cached answer for this account number at
// this bank — the pair has never been swept, or the sweep found nothing.
var ErrNoBankLookup = errors.New("no cached bank lookup")

// BankLookup is one cached answer to "which bank does this account number
// belong to, and whose name is on it".
type BankLookup struct {
	AccountNumber string
	BankCode      string
	BankName      string
	AccountName   string
	ProbedAt      time.Time
}

type BankLookupRepository struct {
	db *sql.DB
}

func NewBankLookupRepository(db *sql.DB) *BankLookupRepository {
	return &BankLookupRepository{db: db}
}

// HasAny reports whether this account number has ever been swept, whether or
// not anything resolved. It is what separates "nothing cached, go probe" from
// "swept before and nothing answered, do not probe again" — two states that
// both come back from Find as an empty slice.
func (r *BankLookupRepository) HasAny(ctx context.Context, accountNumber string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM bank_account_lookups WHERE account_number = $1
		)`, accountNumber).Scan(&exists)
	return exists, err
}

// Find returns every bank that has answered for this account number. An empty
// result means either the number was never swept (check HasAny) or it was and
// no bank resolved.
//
// A miss is stored as a row with an empty bank_code, which is excluded here so
// callers never receive it as a match.
func (r *BankLookupRepository) Find(ctx context.Context, accountNumber string) ([]BankLookup, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT account_number, bank_code, bank_name, account_name, probed_at
		FROM bank_account_lookups
		WHERE account_number = $1 AND bank_code <> ''
		ORDER BY bank_name`, accountNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lookups []BankLookup
	for rows.Next() {
		var lookup BankLookup
		if err := rows.Scan(
			&lookup.AccountNumber, &lookup.BankCode, &lookup.BankName,
			&lookup.AccountName, &lookup.ProbedAt,
		); err != nil {
			return nil, err
		}
		lookups = append(lookups, lookup)
	}
	return lookups, rows.Err()
}

// FindOne returns the cached answer for one account number at one bank, or
// ErrNoBankLookup when that pair has never been recorded. The save path uses
// it to check a submitted account name against a resolved one.
func (r *BankLookupRepository) FindOne(ctx context.Context, accountNumber, bankCode string) (*BankLookup, error) {
	var lookup BankLookup
	err := r.db.QueryRowContext(ctx, `
		SELECT account_number, bank_code, bank_name, account_name, probed_at
		FROM bank_account_lookups
		WHERE account_number = $1 AND bank_code = $2`, accountNumber, bankCode,
	).Scan(
		&lookup.AccountNumber, &lookup.BankCode, &lookup.BankName,
		&lookup.AccountName, &lookup.ProbedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoBankLookup
	}
	if err != nil {
		return nil, err
	}
	return &lookup, nil
}

// SaveMatches stores the banks that answered for an account number. Rows that
// already exist are left alone: a number's bank and holder do not change, and
// letting concurrent sweeps of the same number overwrite each other would make
// two identical lookups fight over identical data.
//
// All rows share one transaction so a sweep is recorded all at once — a
// partial write would leave a number looking like it resolved at fewer banks
// than it really does.
func (r *BankLookupRepository) SaveMatches(ctx context.Context, accountNumber string, matches []BankLookup) error {
	if len(matches) == 0 {
		return r.SaveMiss(ctx, accountNumber)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, match := range matches {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO bank_account_lookups (account_number, bank_code, bank_name, account_name)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (account_number, bank_code) DO NOTHING`,
			accountNumber, match.BankCode, match.BankName, match.AccountName,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SaveMiss records that an account number was swept against the bank shortlist
// and no bank answered. Without this row a mistyped number would re-probe the
// whole shortlist on every attempt, which is the traffic the cache exists to
// avoid.
//
// It is the only row that is updated rather than ignored on conflict: the fact
// is the same, but the timestamp is what tells an operator how stale the miss
// is, and a re-probe is exactly the event worth dating.
func (r *BankLookupRepository) SaveMiss(ctx context.Context, accountNumber string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO bank_account_lookups (account_number, bank_code)
		VALUES ($1, '')
		ON CONFLICT (account_number, bank_code) DO UPDATE
			SET probed_at = CURRENT_TIMESTAMP`, accountNumber)
	return err
}
