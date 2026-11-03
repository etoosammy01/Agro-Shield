package testdb

import "testing"

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
