# Market Data Import

The market-intelligence dashboard accepts reviewed external observations and verified LGA-neighbor mappings through a CSV command.

## External prices

CSV header:

```csv
produce,price,unit,provider,market,lga,state,source_url,collected_at
```

`collected_at` must be RFC 3339, for example `2026-08-24T10:00:00Z`.

```sh
cd backend
go run ./cmd/market-import -type prices -file /path/to/prices.csv
```

## LGA neighbors

CSV header:

```csv
state,lga,neighbor_lga
```

Relationships are directional. Include both directions when adjacency is mutual.

```sh
cd backend
go run ./cmd/market-import -type neighbors -file /path/to/neighbors.csv
```

The importer requires `DATABASE_URL`, runs the normal idempotent migrations, validates every row, and stops at the first invalid observation.
