package doldb

import (
	"context"
	"os"
	"testing"

	"github.com/sgsoluciones/dolibarr-mcp/internal/config"
)

// A project's extrafields must come back on Fetch. Writing them already works
// (MapToDolibarr turns "extrafields" into array_options), but without a read-back
// path there is no way to confirm what was stored — which makes the write
// unverifiable in practice.
//
// Skipped unless DOLIBARR_IT=1. Point it at a disposable instance:
//
//	DOLIBARR_IT=1 DB_PORT=3307 DB_NAME=dolibarr DB_USER=dolibarr DB_PASS=dolibarr \
//	  go test -run TestProjectExtrafields ./internal/doldb/
func TestProjectExtrafieldsAreReadBack(t *testing.T) {
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

	// Find a project that actually carries an extrafield value, so a nil result
	// means the read path is missing rather than the data being absent. jira_key is
	// the field configured on this instance.
	var projectID int64
	var stored string
	q := "SELECT fk_object, jira_key FROM " + db.T("projet_extrafields") +
		" WHERE jira_key IS NOT NULL AND jira_key <> ''" +
		" AND fk_object IN (SELECT rowid FROM " + db.T("projet") + ") LIMIT 1"
	if err := db.QueryRowContext(ctx, q).Scan(&projectID, &stored); err != nil {
		t.Skipf("no project carries extrafields in this instance: %v", err)
	}

	got, err := db.Fetch(ctx, "projects", projectID, "")
	if err != nil {
		t.Fatalf("fetch project %d: %v", projectID, err)
	}

	pj, ok := got.(*Project)
	if !ok {
		t.Fatalf("Fetch returned %T, want *Project", got)
	}
	if pj.Extrafields == nil {
		t.Fatalf("project %d has extrafields in the database but Fetch returned none", projectID)
	}
	if got := pj.Extrafields["jira_key"]; got != stored {
		t.Fatalf("jira_key = %#v, want %q as stored in the database", got, stored)
	}
}
