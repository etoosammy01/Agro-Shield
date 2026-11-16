package testdb

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"backend/internal/database"
)

func TestSchemasAreIsolatedAndCleaned(t *testing.T) {
	parent := Open(t)
	var parentSchema, childSchema string
	if err := parent.QueryRow(`SELECT current_schema()`).Scan(&parentSchema); err != nil {
		t.Fatal(err)
	}
	t.Run("private_schema", func(t *testing.T) {
		child := Open(t)
		if err := child.QueryRow(`SELECT current_schema()`).Scan(&childSchema); err != nil {
			t.Fatal(err)
		}
		if parentSchema == childSchema {
			t.Fatal("tests share a schema")
		}
		if _, err := child.Exec(`CREATE TABLE isolation_probe(id integer)`); err != nil {
			t.Fatal(err)
		}
		var exists bool
		if err := parent.QueryRow(`SELECT to_regclass('isolation_probe') IS NOT NULL`).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatal("child relation visible in parent search path")
		}
	})
	var remains bool
	if err := parent.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)`, childSchema).Scan(&remains); err != nil {
		t.Fatal(err)
	}
	if remains {
		t.Fatal("child schema survived cleanup")
	}
}

func TestSchemaSelectionAppliesToEveryPooledConnection(t *testing.T) {
	parent := Open(t)
	var expected string
	if err := parent.QueryRow(`SELECT current_schema()`).Scan(&expected); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", expected)
	// Model a server-side startup setting that selects public. The explicit
	// connection hook must still select the application schema before queries.
	q.Set("options", "-csearch_path=public")
	u.RawQuery = q.Encode()
	probe, err := database.ConnectDB(u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	probe.SetMaxOpenConns(4)
	probe.SetMaxIdleConns(0)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	seen := map[int]bool{}
	for range 3 {
		conn, err := probe.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		var actual string
		var pid int
		if err := conn.QueryRowContext(ctx, `SELECT current_schema(),pg_backend_pid()`).Scan(&actual, &pid); err != nil {
			t.Fatal(err)
		}
		if actual != expected || seen[pid] {
			t.Fatalf("physical connection did not select its private schema: %q", actual)
		}
		seen[pid] = true
	}
}
