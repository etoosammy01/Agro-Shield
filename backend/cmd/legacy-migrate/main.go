package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"time"

	"backend/internal/config"
	"backend/internal/database"
	"backend/internal/legacymigration"

	"github.com/jackc/pgx/v5"
)

func main() {
	source := flag.String("source", "", "existing normalized schema, for example public")
	target := flag.String("target", "", "separate current application schema")
	apply := flag.Bool("apply", false, "commit the import; default is read-only preflight")
	fingerprint := flag.String("expected-fingerprint", "", "require this reviewed source snapshot")
	reportFile := flag.String("report", "", "write a credential-free JSON report to this path")
	flag.Parse()
	if *source == "" || *target == "" || *source == *target {
		fmt.Fprintln(os.Stderr, "Specify distinct --source and --target schemas")
		os.Exit(1)
	}
	raw, err := config.LoadDatabaseURL()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Database configuration is unavailable")
		os.Exit(1)
	}
	u, err := url.Parse(raw)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Database URL is invalid")
		os.Exit(1)
	}
	q := u.Query()
	q.Set("search_path", pgx.Identifier{*target}.Sanitize())
	u.RawQuery = q.Encode()
	db, err := database.ConnectDB(u.String())
	if err != nil {
		fmt.Fprintln(os.Stderr, "Database connection failed; connection details suppressed")
		os.Exit(1)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	o := legacymigration.Options{Source: *source, Target: *target, ExpectedFingerprint: *fingerprint}
	report, err := legacymigration.Plan(ctx, db, o)
	if err == nil && *apply && len(report.Issues) == 0 && !report.AlreadyApplied {
		if *fingerprint == "" || report.Fingerprint != *fingerprint {
			err = fmt.Errorf("--apply requires the matching --expected-fingerprint from a read-only preflight")
		} else {
			_, err = db.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{*target}.Sanitize())
			if err == nil {
				err = database.RunMigration(db)
			}
			if err == nil {
				err = database.RunSessionMigration(db)
			}
			if err == nil {
				report, err = legacymigration.Apply(ctx, db, o)
			} else {
				err = fmt.Errorf("destination initialization failed; record and connection details suppressed")
			}
		}
	}
	encoded, encodeErr := json.MarshalIndent(report, "", "  ")
	if encodeErr != nil {
		fmt.Fprintln(os.Stderr, "Unable to encode migration report")
		os.Exit(1)
	}
	encoded = append(encoded, '\n')
	if *reportFile != "" {
		if writeErr := os.WriteFile(*reportFile, encoded, 0600); writeErr != nil {
			fmt.Fprintln(os.Stderr, "Unable to save migration report")
			os.Exit(1)
		}
	}
	fmt.Print(string(encoded))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(report.Issues) > 0 {
		fmt.Fprintln(os.Stderr, "Migration preflight did not pass; no new records were imported")
		os.Exit(1)
	}
}
