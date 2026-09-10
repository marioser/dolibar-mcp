package tools

import (
	"context"
	"testing"
)

// Dolibarr numbers a project from its mask, but — unlike proposals and orders — it
// rejects a payload with no ref at all: POST /projects without one answers
// 400 "ref field missing". The literal "auto" is what asks for the mask.
//
// Stripping the ref outright, as auto-numbered entities did, made every project
// create fail. Verified against Dolibarr 23:
//
//	POST /projects {"title":"x"}                -> 400 ref field missing
//	POST /projects {"ref":"auto","title":"x"}   -> 200
func TestCreateProjectAsksDolibarrToNumberIt(t *testing.T) {
	c := &capture{reply: `484`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	_, _, err := deps.HandleCreate(context.Background(), nil, CreateInput{
		Entity: "projects",
		Data:   map[string]any{"title": "Obra civil"},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	if got := c.body["ref"]; got != "auto" {
		t.Fatalf("ref = %#v, want %q so Dolibarr applies its numbering mask", got, "auto")
	}
}

// A client-supplied number must never win over the mask.
func TestCreateProjectIgnoresAClientSuppliedRef(t *testing.T) {
	c := &capture{reply: `484`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	_, _, err := deps.HandleCreate(context.Background(), nil, CreateInput{
		Entity: "projects",
		Data:   map[string]any{"title": "Obra civil", "ref": "PJ-INVENTADO"},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	if got := c.body["ref"]; got != "auto" {
		t.Fatalf("ref = %#v, want the client value replaced by %q", got, "auto")
	}
}

// Proposals keep the existing contract: they are numbered with no ref key at all,
// and sending one would override the mask.
func TestCreateProposalSendsNoRefAtAll(t *testing.T) {
	c := &capture{reply: `3766`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	_, _, err := deps.HandleCreate(context.Background(), nil, CreateInput{
		Entity: "proposals",
		Data: map[string]any{
			"customer_id": 42,
			"ref":         "OF-INVENTADO",
			"extrafields": map[string]any{"tos_attached": "NoCgv"},
		},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	if _, present := c.body["ref"]; present {
		t.Fatalf("proposal payload carried a ref, which overrides the mask: %#v", c.body)
	}
}
