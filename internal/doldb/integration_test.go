package doldb

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/sgsoluciones/dolibarr-mcp/internal/config"
)

// Integration snapshot test for the read layer. Skipped unless DOLIBARR_IT=1.
// It runs Search and Fetch across every entity against a real Dolibarr database
// and, when DOLIBARR_IT_SNAPSHOT is set, writes the full result set as JSON.
//
// Usage (golden equivalence check for refactors):
//
//	DOLIBARR_IT=1 DOLIBARR_IT_SNAPSHOT=/tmp/before.json go test -run TestReadSnapshot ./internal/doldb/
//	# ...refactor...
//	DOLIBARR_IT=1 DOLIBARR_IT_SNAPSHOT=/tmp/after.json  go test -run TestReadSnapshot ./internal/doldb/
//	diff /tmp/before.json /tmp/after.json   # must be identical
//
// DB connection is read from env (DB_HOST/DB_PORT/DB_NAME/DB_USER/DB_PASS/
// DB_PREFIX/DOLIBARR_ENTITY) with localhost defaults.
func TestReadSnapshot(t *testing.T) {
	if os.Getenv("DOLIBARR_IT") == "" {
		t.Skip("set DOLIBARR_IT=1 to run the integration snapshot")
	}

	cfg := &config.Config{
		DBHost:   envOr("DB_HOST", "127.0.0.1"),
		DBPort:   atoiOr("DB_PORT", 3306),
		DBName:   envOr("DB_NAME", "dolibarr"),
		DBUser:   envOr("DB_USER", "dolibarr"),
		DBPass:   envOr("DB_PASS", "dolibarr"),
		DBPrefix: envOr("DB_PREFIX", "llx_"),
		Entity:   atoiOr("DOLIBARR_ENTITY", 1),
	}

	db, err := New(cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	snapshot := map[string]any{}

	entities := ValidEntitiesForTest()
	// document entities support Fetch-with-lines and are worth fetching in detail
	docTables := map[string]string{
		"proposals":  "propal",
		"orders":     "commande",
		"purchases":  "commande_fournisseur",
		"projects":   "projet",
		"shipments":  "expedition",
		"receptions": "reception",
		"customers":  "societe",
		"products":   "product",
		"warehouses": "entrepot",
	}

	for _, ent := range entities {
		// 1. plain search (default sort, first page)
		res, total, err := db.Search(ctx, SearchParams{Entity: ent, Limit: 50})
		snapshot["search:"+ent] = errOr(map[string]any{"total": total, "results": res}, err)

		// 2. text search + pagination (exercises LIKE + offset)
		res2, total2, err2 := db.Search(ctx, SearchParams{Entity: ent, Query: "a", Limit: 10, Offset: 5})
		snapshot["search_q:"+ent] = errOr(map[string]any{"total": total2, "results": res2}, err2)

		// 3. fetch a few real rows by id (exercises joins, lines, null handling)
		for _, id := range firstIDs(t, db, docTables[ent], 5) {
			one, ferr := db.Fetch(ctx, ent, id, "")
			snapshot["fetch:"+ent+":"+strconv.FormatInt(id, 10)] = errOr(one, ferr)
		}
	}

	if out := os.Getenv("DOLIBARR_IT_SNAPSHOT"); out != "" {
		data, err := json.MarshalIndent(snapshot, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, data, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("snapshot written to %s (%d keys)", out, len(snapshot))
	}
}

// firstIDs returns up to n rowids from the given table, ordered, for Fetch tests.
func firstIDs(t *testing.T, db *DB, table string, n int) []int64 {
	if table == "" {
		return nil
	}
	rows, err := db.QueryContext(context.Background(),
		"SELECT rowid FROM "+db.T(table)+" ORDER BY rowid LIMIT ?", n)
	if err != nil {
		t.Fatalf("firstIDs %s: %v", table, err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func errOr(v any, err error) any {
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return v
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func atoiOr(k string, d int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return d
}

// ValidEntitiesForTest lists the entities exercised by the snapshot, in a stable order.
func ValidEntitiesForTest() []string {
	return []string{
		"customers", "products", "proposals", "projects",
		"orders", "purchases", "warehouses", "shipments", "receptions",
	}
}
