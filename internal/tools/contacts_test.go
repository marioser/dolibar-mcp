package tools

import (
	"context"
	"testing"
)

// Every rejection happens before anything leaves the process: a call that
// cannot succeed should cost nothing.
func TestContactsRejectsBadInputBeforeTheNetwork(t *testing.T) {
	deps := &Deps{} // nil DB and API on purpose: a valid call would panic here

	cases := map[string]ContactsInput{
		"no action":           {Entity: "projects", ID: 571},
		"unknown action":      {Action: "delete", Entity: "projects", ID: 571},
		"list without entity": {Action: "list", ID: 571},
		"list without id":     {Action: "list", Entity: "projects"},
		"update without id":   {Action: "update", Data: map[string]any{"email": "x@y.z"}},
		"update without data": {Action: "update", ID: 42},
	}

	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := deps.HandleContacts(context.Background(), nil, in); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

// The id of an update is the CONTACT's, and it must reach /contacts/{id} —
// addressing the project instead would edit the wrong record silently.
func TestUpdateAddressesTheContactItself(t *testing.T) {
	c := &capture{reply: `{"id":"42"}`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	_, _, err := deps.HandleContacts(context.Background(), nil, ContactsInput{
		Action: "update",
		ID:     42,
		Data:   map[string]any{"email": "mario@acesco.com"},
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if c.path != "contacts/42" {
		t.Fatalf("path = %q, want %q", c.path, "contacts/42")
	}
	if got := c.body["email"]; got != "mario@acesco.com" {
		t.Fatalf("email = %#v, not forwarded", got)
	}
}

// Only the fields the caller named travel: sending the whole contact back would
// overwrite values nobody meant to touch.
func TestUpdateSendsOnlyTheNamedFields(t *testing.T) {
	c := &capture{reply: `{"id":"42"}`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	if _, _, err := deps.HandleContacts(context.Background(), nil, ContactsInput{
		Action: "update",
		ID:     42,
		Data:   map[string]any{"phone_mobile": "3001234567"},
	}); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	if len(c.body) != 1 {
		t.Fatalf("body carries %d fields, want only the one named: %#v", len(c.body), c.body)
	}
}

func TestContactEntitiesBacksTheEnum(t *testing.T) {
	got := ContactEntities()
	if len(got) != 2 || got[0] != "customers" || got[1] != "projects" {
		t.Fatalf("ContactEntities() = %v", got)
	}
}
