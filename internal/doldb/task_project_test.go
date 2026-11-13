package doldb

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	"github.com/sgsoluciones/dolibarr-mcp/internal/config"
)

// A task on its own says nothing about where it belongs. The JSON a caller
// reads must name the project, by id and by ref.
func TestTaskJSONNamesItsProject(t *testing.T) {
	raw, err := json.Marshal(ProjectTask{ID: 30, Ref: "30", Label: "x", ProjectID: 476, ProjectRef: "GDI-00"})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["project_id"] != float64(476) {
		t.Errorf("project_id = %#v, want 476 in %s", got["project_id"], raw)
	}
	if got["project_ref"] != "GDI-00" {
		t.Errorf("project_ref = %#v, want GDI-00 in %s", got["project_ref"], raw)
	}
}

func TestTaskSearchResultJSONNamesItsProject(t *testing.T) {
	raw, err := json.Marshal(SearchResult{ID: 30, Ref: "30", ProjectRef: "GDI-00"})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["project_ref"] != "GDI-00" {
		t.Errorf("project_ref = %#v, want GDI-00 in %s", got["project_ref"], raw)
	}
}

// Other entities never carry a project ref, and their JSON must not grow an
// empty key because tasks gained one.
func TestSearchResultOmitsEmptyProjectRef(t *testing.T) {
	raw, _ := json.Marshal(SearchResult{ID: 1, Ref: "PR1"})
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	if _, ok := got["project_ref"]; ok {
		t.Errorf("project_ref leaked into a non-task result: %s", raw)
	}
}

// decodeFetch reads a Fetch result back into its model. Fetch hands out
// json.RawMessage so a cached value cannot be mutated by a caller.
func decodeFetch[T any](t *testing.T, v any) *T {
	t.Helper()
	raw, ok := v.(json.RawMessage)
	if !ok {
		t.Fatalf("Fetch returned %T, want json.RawMessage", v)
	}
	out := new(T)
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode fetch result: %v", err)
	}
	return out
}

func itDB(t *testing.T) *DB {
	t.Helper()
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
	t.Cleanup(func() { db.Close() })
	return db
}

type taskRow struct {
	id                      int64
	ref, label, description string
	progress, status        int
	planned, spent          float64
	projectID               int64
	projectRef              string
}

// rawTask reads the task straight from the tables, as the reference the read
// layer must agree with.
func rawTask(t *testing.T, db *DB, id int64) taskRow {
	t.Helper()
	var r taskRow
	q := "SELECT t.rowid, t.ref, t.label, COALESCE(t.description,''), COALESCE(t.progress,0), COALESCE(t.fk_statut,0)," +
		" COALESCE(t.planned_workload,0), COALESCE(t.duration_effective,0), t.fk_projet, pj.ref" +
		" FROM " + db.T("projet_task") + " t JOIN " + db.T("projet") + " pj ON pj.rowid = t.fk_projet WHERE t.rowid = ?"
	if err := db.QueryRowContext(context.Background(), q, id).Scan(&r.id, &r.ref, &r.label, &r.description,
		&r.progress, &r.status, &r.planned, &r.spent, &r.projectID, &r.projectRef); err != nil {
		if err == sql.ErrNoRows {
			t.Skipf("task %d does not exist in this instance", id)
		}
		t.Fatalf("raw task %d: %v", id, err)
	}
	return r
}

func assertSameTask(t *testing.T, where string, got ProjectTask, want taskRow) {
	t.Helper()
	if got.ID != want.id || got.Ref != want.ref || got.Label != want.label || got.Description != want.description ||
		got.Progress != want.progress || got.Status != want.status || got.Planned != want.planned || got.Spent != want.spent {
		t.Errorf("%s: existing fields changed: got %+v, want %+v", where, got, want)
	}
	if got.ProjectID != want.projectID {
		t.Errorf("%s: project_id = %d, want fk_projet %d", where, got.ProjectID, want.projectID)
	}
	if got.ProjectRef != want.projectRef {
		t.Errorf("%s: project_ref = %q, want %q", where, got.ProjectRef, want.projectRef)
	}
}

// Task 30 of project 476 is the reference task of dolitest-db.
//
//	DOLIBARR_IT=1 DB_PORT=3307 go test -count=1 -run TestTaskProject ./internal/doldb/
func TestTaskProjectOnGetNestedAndSearch(t *testing.T) {
	db := itDB(t)
	ctx := context.Background()
	want := rawTask(t, db, 30)

	got, err := db.Fetch(ctx, "tasks", 30, "")
	if err != nil {
		t.Fatalf("fetch task: %v", err)
	}
	assertSameTask(t, "get tasks", *decodeFetch[ProjectTask](t, got), want)

	pjAny, err := db.Fetch(ctx, "projects", want.projectID, "")
	if err != nil {
		t.Fatalf("fetch project: %v", err)
	}
	found := false
	for _, nested := range decodeFetch[Project](t, pjAny).Tasks {
		if nested.ID == want.id {
			found = true
			assertSameTask(t, "nested task", nested, want)
		}
	}
	if !found {
		t.Fatalf("task %d missing from project %d", want.id, want.projectID)
	}

	results, _, err := db.Search(ctx, SearchParams{Entity: "tasks", Query: want.label, Limit: 100})
	if err != nil {
		t.Fatalf("search tasks: %v", err)
	}
	for _, r := range results {
		if r.ID != want.id {
			continue
		}
		if r.Ref != want.ref || r.Label != want.label || r.StatusCode != want.status || r.TotalHT != want.planned {
			t.Errorf("search: existing fields changed: %+v vs %+v", r, want)
		}
		if r.ProjectRef != want.projectRef {
			t.Errorf("search: project_ref = %q, want %q", r.ProjectRef, want.projectRef)
		}
		return
	}
	t.Fatalf("search for %q did not return task %d", want.label, want.id)
}

// Task extrafields go through the same generic reader as the project's, so the
// plumbing columns stay out and a task without values carries none.
func TestTaskExtrafieldsSkipPlumbing(t *testing.T) {
	db := itDB(t)
	got, err := db.Fetch(context.Background(), "tasks", 30, "")
	if err != nil {
		t.Fatalf("fetch task: %v", err)
	}
	task := decodeFetch[ProjectTask](t, got)
	for _, plumbing := range []string{"tms", "import_key", "rowid", "fk_object"} {
		if _, leaked := task.Extrafields[plumbing]; leaked {
			t.Errorf("task extrafields exposed %q: %#v", plumbing, task.Extrafields)
		}
	}
}
