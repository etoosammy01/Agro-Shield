package repository

import (
	"encoding/json"
	"testing"
	"time"
)

// These synthetic fixtures prove the conversion technique, not the interpretation
// of deployed data. No application column is converted by this test.
func TestExplicitTimestampInterpretationIgnoresSessionTimezone(t *testing.T) {
	db := testDB(t)
	for _, session := range []string{"UTC", "Africa/Lagos", "Asia/Tokyo"} {
		for _, source := range []string{"UTC", "Africa/Lagos"} {
			t.Run(session+"/"+source, func(t *testing.T) {
				tx, err := db.Begin()
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				if _, err := tx.Exec(`SELECT set_config('TimeZone',$1,true)`, session); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec(`CREATE TEMP TABLE timestamp_fixture(value TIMESTAMP NOT NULL) ON COMMIT DROP; INSERT INTO timestamp_fixture VALUES ('2026-01-02 12:34:56')`); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec(`ALTER TABLE timestamp_fixture ALTER COLUMN value TYPE TIMESTAMPTZ USING value AT TIME ZONE '` + source + `'`); err != nil {
					t.Fatal(err)
				}
				var actual time.Time
				if err := tx.QueryRow(`SELECT value FROM timestamp_fixture`).Scan(&actual); err != nil {
					t.Fatal(err)
				}
				hour := 12
				if source == "Africa/Lagos" {
					hour = 11
				}
				expected := time.Date(2026, 1, 2, hour, 34, 56, 0, time.UTC)
				if !actual.Equal(expected) {
					t.Fatalf("instant %s, want %s", actual, expected)
				}
				encoded, err := json.Marshal(actual.UTC())
				if err != nil {
					t.Fatal(err)
				}
				if string(encoded) != `"`+expected.Format(time.RFC3339)+`"` {
					t.Fatalf("JSON %s", encoded)
				}
				if _, err := tx.Exec(`ALTER TABLE timestamp_fixture ALTER COLUMN value TYPE TIMESTAMP USING value AT TIME ZONE '` + source + `'`); err != nil {
					t.Fatal(err)
				}
				var wall string
				if err := tx.QueryRow(`SELECT to_char(value, 'YYYY-MM-DD HH24:MI:SS') FROM timestamp_fixture`).Scan(&wall); err != nil {
					t.Fatal(err)
				}
				if wall != "2026-01-02 12:34:56" {
					t.Fatalf("rollback changed wall clock %s", wall)
				}
			})
		}
	}
}
