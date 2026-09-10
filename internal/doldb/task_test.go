package doldb

import (
	"context"
	"os"
	"testing"

	"github.com/sgsoluciones/dolibarr-mcp/internal/config"
)

// Writing tasks goes through the REST API, but reading them comes from MySQL.
// Both halves have to exist or the entity is only half-supported: the tool would
// accept a create and then be unable to list or show what it created.
//
// Skipped unless DOLIBARR_IT=1:
//
//	DOLIBARR_IT=1 DB_PORT=3307 DB_NAME=dolibarr DB_USER=dolibarr DB_PASS=dolibarr \
//	  go test -run TestTask ./internal/doldb/
func TestTasksAreSearchableAndFetchable(t *testing.T) {
	if os.Getenv("DOLIBARR_IT") == "" {
		t.Skip("set DOLIBARR_IT=1 to run against a real Dolibarr database")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	db, err := New(cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	results, total, err := db.Search(ctx, SearchParams{Entity: "tasks", Limit: 5})
	if err != nil {
		t.Fatalf("search tasks: %v", err)
	}
	if total == 0 || len(results) == 0 {
		t.Skip("this instance has no project tasks to exercise")
	}

	first := results[0]
	if first.ID == 0 {
		t.Fatalf("search returned a task with no id: %#v", first)
	}

	got, err := db.Fetch(ctx, "tasks", first.ID, "")
	if err != nil {
		t.Fatalf("fetch task %d: %v", first.ID, err)
	}
	task, ok := got.(*ProjectTask)
	if !ok {
		t.Fatalf("Fetch returned %T, want *ProjectTask", got)
	}
	if task.ID != first.ID {
		t.Fatalf("fetched task id = %d, want %d", task.ID, first.ID)
	}
	if task.Label == "" {
		t.Fatalf("task %d came back with no label: %#v", task.ID, task)
	}
}
