package main

import (
	"backend/internal/database"
	"backend/internal/repository"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	kind := flag.String("type", "", "CSV type: prices or neighbors")
	path := flag.String("file", "", "path to the CSV file")
	flag.Parse()
	if *kind == "" || *path == "" {
		log.Fatal("usage: go run ./cmd/market-import -type prices|neighbors -file data.csv")
	}

	db, err := database.ConnectDB()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := database.RunMigration(db); err != nil {
		log.Fatal(err)
	}

	file, err := os.Open(*path)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	repo := repository.NewMarketEventRepository(db)
	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true
	if _, err := reader.Read(); err != nil {
		log.Fatalf("reading CSV header: %v", err)
	}

	imported := 0
	for rowNumber := 2; ; rowNumber++ {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("reading row %d: %v", rowNumber, err)
		}
		switch strings.ToLower(strings.TrimSpace(*kind)) {
		case "prices":
			err = importPrice(repo, row)
		case "neighbors":
			err = importNeighbor(repo, row)
		default:
			log.Fatalf("unsupported import type %q", *kind)
		}
		if err != nil {
			log.Fatalf("row %d: %v", rowNumber, err)
		}
		imported++
	}
	log.Printf("imported %d %s rows", imported, *kind)
}

func importPrice(repo *repository.MarketEventRepository, row []string) error {
	if len(row) != 10 {
		return fmt.Errorf("price CSV requires 10 columns: produce,price,unit,provider,market,lga,state,country,source_url,collected_at")
	}
	price, err := strconv.ParseFloat(strings.TrimSpace(row[1]), 64)
	if err != nil {
		return fmt.Errorf("invalid price: %w", err)
	}
	collectedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(row[9]))
	if err != nil {
		return fmt.Errorf("invalid collected_at: %w", err)
	}
	return repo.RecordExternalPrice(row[0], price, row[2], row[3], row[4], row[5], row[6], row[7], row[8], collectedAt)
}

func importNeighbor(repo *repository.MarketEventRepository, row []string) error {
	if len(row) != 3 {
		return fmt.Errorf("neighbor CSV requires 3 columns: state,lga,neighbor_lga")
	}
	return repo.UpsertLGANeighbor(row[0], row[1], row[2])
}
