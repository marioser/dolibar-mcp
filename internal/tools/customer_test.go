package tools

import (
	"context"
	"testing"
)

// Dolibarr validates a new thirdparty on the raw request body: $FIELDS = array('name')
// in api_thirdparties.class.php, and a missing key answers 400 "name field missing"
// before anything is assigned to the object. Sending the deprecated "nom" alias
// instead made every customer create fail. Verified against Dolibarr 23:
//
//	POST /thirdparties {"nom":"x","client":1}  -> 400 name field missing
//	POST /thirdparties {"name":"x","client":1} -> 200
func TestCreateCustomerSendsNameToThirdparties(t *testing.T) {
	c := &capture{reply: `1058`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	_, out, err := deps.HandleCreate(context.Background(), nil, CreateInput{
		Entity: "customers",
		Data:   map[string]any{"name": "Bimbo", "client": 1},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	if c.path != "thirdparties" {
		t.Fatalf("path = %q, want thirdparties", c.path)
	}
	if got := c.body["name"]; got != "Bimbo" {
		t.Fatalf("name = %#v, want %q under the name key Dolibarr validates", got, "Bimbo")
	}
	if _, leaked := c.body["nom"]; leaked {
		t.Fatalf("payload carried nom, the deprecated alias the API does not validate: %#v", c.body)
	}
	if !out.Success {
		t.Fatalf("expected success, got %#v", out)
	}
}

// The same rename made PUT a silent no-op: Dolibarr answers 200 and keeps the old
// name, because Societe::update() only falls back to nom when name is empty, and
// name is already loaded from the database at that point.
func TestUpdateCustomerSendsNameToThirdparties(t *testing.T) {
	c := &capture{reply: `{"id":1058}`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	_, _, err := deps.HandleUpdate(context.Background(), nil, UpdateInput{
		Entity: "customers",
		ID:     1058,
		Data:   map[string]any{"name": "Bimbo S.A."},
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	if c.path != "thirdparties/1058" {
		t.Fatalf("path = %q, want thirdparties/1058", c.path)
	}
	if got := c.body["name"]; got != "Bimbo S.A." {
		t.Fatalf("name = %#v, want it sent under the name key so the rename is applied", got)
	}
	if _, leaked := c.body["nom"]; leaked {
		t.Fatalf("payload carried nom, which PUT /thirdparties silently ignores: %#v", c.body)
	}
}
