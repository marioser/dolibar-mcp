package tools

import (
	"context"
	"testing"
)

// What actually leaves the process is what matters here. Dolibarr accepts a project
// payload carrying "desc", answers 200, and drops the field — so a rename that looks
// harmless in the mapper silently loses the text. Verified against Dolibarr 23:
// PUT /projects/{id} with {"desc":...} leaves the description untouched.
func TestCreateProjectSendsDescriptionNotDesc(t *testing.T) {
	c := &capture{reply: `123`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	_, _, err := deps.HandleCreate(context.Background(), nil, CreateInput{
		Entity: "projects",
		Data:   map[string]any{"title": "Obra", "description": "alcance detallado"},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	if _, renamed := c.body["desc"]; renamed {
		t.Fatalf("payload carried desc; Dolibarr would drop it: %#v", c.body)
	}
	if got := c.body["description"]; got != "alcance detallado" {
		t.Fatalf("description = %#v, want it sent under the description key", got)
	}
}

func TestUpdateProjectSendsDescriptionNotDesc(t *testing.T) {
	c := &capture{reply: `{"id":123}`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	_, _, err := deps.HandleUpdate(context.Background(), nil, UpdateInput{
		Entity: "projects",
		ID:     123,
		Data:   map[string]any{"description": "alcance corregido"},
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	if _, renamed := c.body["desc"]; renamed {
		t.Fatalf("payload carried desc; Dolibarr would drop it: %#v", c.body)
	}
	if got := c.body["description"]; got != "alcance corregido" {
		t.Fatalf("description = %#v, want it sent under the description key", got)
	}
}

// Dolibarr marks title mandatory on the project resource, so a payload without one
// must be rejected here instead of producing an unusable document.
func TestCreateProjectRequiresTitle(t *testing.T) {
	c := &capture{reply: `123`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	_, _, err := deps.HandleCreate(context.Background(), nil, CreateInput{
		Entity: "projects",
		Data:   map[string]any{"description": "sin titulo"},
	})
	if err == nil {
		t.Fatal("creating a project without a title was accepted")
	}
	if c.hits != 0 {
		t.Fatalf("invalid payload reached the server (%d hits)", c.hits)
	}
}

func TestCreateProjectWithTitleIsAccepted(t *testing.T) {
	c := &capture{reply: `123`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	_, _, err := deps.HandleCreate(context.Background(), nil, CreateInput{
		Entity: "projects",
		Data:   map[string]any{"title": "Obra civil"},
	})
	if err != nil {
		t.Fatalf("a project with a title was rejected: %v", err)
	}
	if c.hits != 1 {
		t.Fatalf("hits = %d, want the create to reach the server once", c.hits)
	}
}

// Lines are the reason the rename exists: they must keep sending "desc".
func TestProposalLineStillSendsDesc(t *testing.T) {
	c := &capture{reply: `456`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	_, _, err := deps.HandleLine(context.Background(), nil, LineInput{
		Action:   "add",
		Entity:   "proposals",
		ParentID: 1,
		Data:     map[string]any{"description": "<p>detalle</p>", "qty": 1},
	})
	if err != nil {
		t.Fatalf("line add failed: %v", err)
	}

	if got := c.body["desc"]; got != "<p>detalle</p>" {
		t.Fatalf("desc = %#v, want the line description sent as desc", got)
	}
}
