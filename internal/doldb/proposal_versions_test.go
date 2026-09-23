package doldb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"

	"github.com/sgsoluciones/dolibarr-mcp/internal/config"
)

func TestLiveVersionNumIsZeroWithoutVersions(t *testing.T) {
	if got := liveVersionNum(sql.NullInt64{}); got != 0 {
		t.Fatalf("live version with no rows = %d, want 0", got)
	}
}

// v0 is a real version: a proposal frozen once has MAX=0 and lives as v1.
// Treating MAX=0 like "no rows" would label the live copy v0 a second time.
func TestLiveVersionNumAdvancesPastVersionZero(t *testing.T) {
	cases := map[int64]int{0: 1, 1: 2, 7: 8}
	for maxNum, want := range cases {
		if got := liveVersionNum(sql.NullInt64{Int64: maxNum, Valid: true}); got != want {
			t.Errorf("MAX=%d: live version = %d, want %d", maxNum, got, want)
		}
	}
}

func TestMissingTableReadsAsModuleNotInstalled(t *testing.T) {
	raw := &mysql.MySQLError{Number: 1146, Message: "Table 'dolibarr.llx_sgproposalversion' doesn't exist"}

	err := proposalVersionsError(fmt.Errorf("query: %w", raw), "llx_sgproposalversion")

	if !errors.Is(err, ErrProposalVersionsNotInstalled) {
		t.Fatalf("error %q does not wrap ErrProposalVersionsNotInstalled", err)
	}
	if strings.Contains(err.Error(), "doesn't exist") {
		t.Errorf("raw SQL error leaked to the caller: %q", err)
	}
	if !strings.Contains(err.Error(), "llx_sgproposalversion") {
		t.Errorf("error should name the missing table: %q", err)
	}
}

func TestOtherErrorsPassThroughUntouched(t *testing.T) {
	denied := &mysql.MySQLError{Number: 1142, Message: "SELECT command denied"}
	if got := proposalVersionsError(denied, "llx_sgproposalversion"); got != error(denied) {
		t.Errorf("error 1142 was rewritten: %v", got)
	}
	if got := proposalVersionsError(sql.ErrNoRows, "llx_sgproposalversion"); !errors.Is(got, sql.ErrNoRows) {
		t.Errorf("sql.ErrNoRows was rewritten: %v", got)
	}
}

func TestProposalVersionOmitsTheSnapshot(t *testing.T) {
	payload, err := json.Marshal(ProposalVersion{ID: 1, ProposalID: 2, VersionNum: 0})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "snapshot") {
		t.Errorf("a list entry must not carry the snapshot: %s", payload)
	}
}

// Skipped unless DOLIBARR_IT=1. Point it at a disposable instance with the
// sgproposalversion module installed:
//
//	DOLIBARR_IT=1 DB_PORT=3307 DB_NAME=dolibarr DB_USER=dolibarr DB_PASS=dolibarr \
//	  go test -run TestProposalVersionsAgainstDatabase ./internal/doldb/
func TestProposalVersionsAgainstDatabase(t *testing.T) {
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

	var proposalID int64
	q := "SELECT fk_propal FROM " + db.T(proposalVersionTable) + " ORDER BY rowid DESC LIMIT 1"
	if err := db.QueryRowContext(ctx, q).Scan(&proposalID); err != nil {
		if errors.Is(proposalVersionsError(err, ""), ErrProposalVersionsNotInstalled) {
			t.Skip("sgproposalversion is not installed in this instance")
		}
		t.Skipf("no frozen proposal version to exercise: %v", err)
	}

	pv, err := db.listProposalVersionsUncached(ctx, proposalID)
	if err != nil {
		t.Fatalf("list versions of proposal %d: %v", proposalID, err)
	}
	if pv.Count == 0 {
		t.Skipf("proposal %d has no versions in entity %d", proposalID, db.Entity())
	}
	if pv.LiveVersionNum != pv.Versions[0].VersionNum+1 {
		t.Errorf("live version %d, want newest (%d) + 1", pv.LiveVersionNum, pv.Versions[0].VersionNum)
	}
	for i := 1; i < len(pv.Versions); i++ {
		if pv.Versions[i-1].VersionNum < pv.Versions[i].VersionNum {
			t.Fatalf("versions not ordered newest first: %+v", pv.Versions)
		}
	}

	if _, err := db.listProposalVersionsUncached(ctx, -1); !errors.Is(err, ErrProposalNotFound) {
		t.Errorf("a missing proposal returned %v, want ErrProposalNotFound", err)
	}
}
