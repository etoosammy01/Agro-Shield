// Operator repair for explicitly confirmed test-only duplicate accounts.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"backend/internal/config"
	"backend/internal/database"
	"backend/internal/repository"
	"backend/internal/services"
)

func main() {
	keeper := flag.Int("keep", 0, "confirmed administrator account ID")
	remove := flag.String("delete", "", "comma-separated duplicate account IDs")
	testOnly := flag.Bool("test-records", false, "attest that duplicate funding records are test data")
	apply := flag.Bool("apply", false, "commit cleanup; default validates and rolls back")
	flag.Parse()
	var ids []int
	for _, value := range strings.Split(*remove, ",") {
		id, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || id <= 0 {
			fail("Specify valid duplicate account IDs")
		}
		ids = append(ids, id)
	}
	raw, err := config.LoadDatabaseURL()
	if err != nil {
		fail("Database configuration unavailable")
	}
	db, err := database.ConnectDB(raw)
	if err != nil {
		fail("Database connection unavailable; details suppressed")
	}
	defer db.Close()
	var schema string
	if err := db.QueryRow(`SELECT current_schema()`).Scan(&schema); err != nil || schema == "public" {
		fail("Select the application schema; archived public records must stay unchanged")
	}
	configured, err := services.ParseWalletAdminIDs(os.Getenv("WALLET_ADMIN_USER_IDS"))
	if err != nil {
		fail("Administrator configuration invalid")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report, err := repository.NewAdminRepository(db, configured).CleanupTestDuplicates(ctx, *keeper, ids, *testOnly, *apply)
	if err != nil {
		fail("Cleanup refused or rolled back: " + safeError(err))
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		fail("Unable to encode cleanup report")
	}
	if *apply {
		fmt.Println("Phone standard migration applied; administrator access preserved")
	}
}

func fail(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }
func safeError(err error) string {
	for _, known := range []error{repository.ErrAdminInvalid, repository.ErrAdminForbidden, repository.ErrAdminProtected, repository.ErrAdminConflict, repository.ErrAccountNotFound, repository.ErrAccountHasBalance, repository.ErrAccountHasPendingRequest, repository.ErrInvalidPhone} {
		if err == known {
			return known.Error()
		}
	}
	return "database operation failed; record details suppressed"
}
