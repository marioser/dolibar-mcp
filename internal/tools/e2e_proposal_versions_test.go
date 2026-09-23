package tools

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/sgsoluciones/dolibarr-mcp/internal/config"
	"github.com/sgsoluciones/dolibarr-mcp/internal/dolapi"
	"github.com/sgsoluciones/dolibarr-mcp/internal/doldb"
)

// End-to-end check of the proposal version tools: freeze through the REST API,
// then read the history straight from the database, wired exactly as main.go
// wires them (the write invalidates the read cache).
//
// Skipped unless DOLIBARR_E2E=1. Requires the database and API env vars
// (DB_*, DOLIBARR_API_URL, DOLIBARR_API_KEY), the sgproposalversion module
// >= 1.3.0, and TEST_PROPOSAL_ID. It FREEZES a real version of that proposal
// (versions cannot be deleted), so point it at a disposable instance — never
// production.
func TestProposalVersionsE2E(t *testing.T) {
	if os.Getenv("DOLIBARR_E2E") == "" {
		t.Skip("set DOLIBARR_E2E=1 to run the end-to-end proposal version test")
	}
	proposalID, _ := strconv.ParseInt(os.Getenv("TEST_PROPOSAL_ID"), 10, 64)
	if proposalID == 0 {
		t.Fatal("TEST_PROPOSAL_ID required")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	db, err := doldb.New(cfg)
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	defer db.Close()
	client := dolapi.New(cfg)
	client.OnWrite(db.InvalidateReads)
	deps := &Deps{DB: db, API: client}
	ctx := context.Background()

	before := listVersions(t, deps, ctx, proposalID)

	res, out, err := deps.HandleProposalFreezeVersion(ctx, nil, ProposalFreezeVersionInput{
		ProposalID: proposalID,
		Note:       "MCP e2e freeze",
	})
	if e := toolErr(res, err); e != nil {
		t.Fatalf("freeze failed: %v", e)
	}
	frozen, _ := out.Result.(map[string]any)
	t.Logf("froze proposal %d: %v", proposalID, frozen)
	if got := asInt64(t, frozen["version_num"]); got != int64(before.LiveVersionNum) {
		t.Errorf("froze version %d, want the previous live number %d", got, before.LiveVersionNum)
	}

	after := listVersions(t, deps, ctx, proposalID)
	if after.LiveVersionNum != before.LiveVersionNum+1 {
		t.Errorf("live version after freeze = %d, want %d", after.LiveVersionNum, before.LiveVersionNum+1)
	}
	if after.Count != before.Count+1 {
		t.Errorf("version count after freeze = %d, want %d", after.Count, before.Count+1)
	}
	if after.Count > 0 && after.Versions[0].Note != "MCP e2e freeze" {
		t.Errorf("newest version note = %q, want the note just sent", after.Versions[0].Note)
	}

	// A missing proposal must read as such, on both tools.
	res, _, err = deps.HandleProposalFreezeVersion(ctx, nil, ProposalFreezeVersionInput{ProposalID: 999999999, Note: "x"})
	if e := toolErr(res, err); e == nil {
		t.Error("freezing a missing proposal should fail")
	} else {
		t.Logf("missing proposal (freeze): %v", e)
	}
	if _, _, err := deps.HandleProposalVersions(ctx, nil, ProposalVersionsInput{ProposalID: 999999999}); err == nil {
		t.Error("listing a missing proposal should fail")
	} else {
		t.Logf("missing proposal (list): %v", err)
	}
}

func listVersions(t *testing.T, deps *Deps, ctx context.Context, proposalID int64) doldb.ProposalVersions {
	t.Helper()
	_, out, err := deps.HandleProposalVersions(ctx, nil, ProposalVersionsInput{ProposalID: proposalID})
	if err != nil {
		t.Fatalf("list versions of %d: %v", proposalID, err)
	}
	raw, err := json.Marshal(out.Result)
	if err != nil {
		t.Fatal(err)
	}
	var pv doldb.ProposalVersions
	if err := json.Unmarshal(raw, &pv); err != nil {
		t.Fatalf("decode versions: %v (%s)", err, raw)
	}
	return pv
}
