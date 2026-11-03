// Package testdb provisions a private schema per PostgreSQL integration test.
package testdb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"backend/internal/database"
)

// ValidateURL deliberately accepts only a local, explicitly named disposable database.
// Remote test databases require a reviewed change to this guard, never an override flag.
func ValidateURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("TEST_DATABASE_URL must be a PostgreSQL URL")
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return nil, fmt.Errorf("PostgreSQL URL required")
	}
	if u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return nil, fmt.Errorf("test database must use a loopback host")
	}
	if u.Path != "/agroshield_test" {
		return nil, fmt.Errorf("test database must be named agroshield_test")
	}
	for key := range u.Query() {
		if key != "sslmode" {
			return nil, fmt.Errorf("test URL permits only the sslmode query option")
		}
	}
	return u, nil
}

func Open(t *testing.T) *sql.DB {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if raw == "" {
		if os.Getenv("REQUIRE_DB_TESTS") == "1" {
			t.Fatal("TEST_DATABASE_URL is required")
		}
		t.Skip("optional local database run: set TEST_DATABASE_URL; CI requires execution")
	}
	u, err := ValidateURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := database.ConnectDB(u.String())
	if err != nil {
		t.Fatal("test database connection failed (connection details suppressed)")
	}
	t.Cleanup(func() { _ = admin.Close() })
	var name string
	if err := admin.QueryRow("SELECT current_database()").Scan(&name); err != nil || name != "agroshield_test" {
		t.Fatal("test database identity check failed")
	}
	var token [12]byte
	if _, err := rand.Read(token[:]); err != nil {
		t.Fatal("generate test schema identifier")
	}
	schema := "agro_test_" + hex.EncodeToString(token[:])
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal("create isolated test schema failed")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error("drop isolated test schema failed")
		}
	})
	q := u.Query()
	q.Set("search_path", schema)
	q.Set("timezone", "UTC")
	u.RawQuery = q.Encode()
	db, err := database.ConnectDB(u.String())
	if err != nil {
		t.Fatal("connect isolated test schema failed")
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error("close isolated database failed")
		}
	})
	if err := database.RunMigration(db); err != nil {
		t.Fatal("initialize test schema failed (details suppressed); inspect migration with synthetic fixtures")
	}
	return db
}
