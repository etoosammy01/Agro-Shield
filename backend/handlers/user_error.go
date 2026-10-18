package handlers

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// userError returns a message that is safe to show to the person using the
// page. Services raise short, deliberate sentences for things the user can fix
// ("enter a delivery address", "insufficient wallet balance"), and those are
// passed through. A database, context, or network failure is replaced by the
// caller's fallback: SQL text names tables and constraints, and a provider
// transport error says nothing a user can act on.
func userError(err error, fallback string) string {
	if err == nil {
		return ""
	}

	var pgErr *pgconn.PgError
	var netErr net.Error
	if errors.As(err, &pgErr) ||
		errors.Is(err, sql.ErrNoRows) ||
		errors.Is(err, driver.ErrBadConn) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.As(err, &netErr) {
		return fallback
	}

	message := strings.TrimSpace(err.Error())
	if message == "" || strings.Contains(message, "SQLSTATE") {
		return fallback
	}
	return message
}
