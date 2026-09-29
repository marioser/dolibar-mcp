package doldb

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/sgsoluciones/dolibarr-mcp/internal/config"
)

// A contact list must serialise as [] and never as null: a caller that asks
// "who is the contact of this project" and gets null cannot tell an empty
// answer from a broken one.
func TestAnEmptyContactListSerialisesAsAnArray(t *testing.T) {
	payload, err := json.Marshal(ContactList{ElementID: 571, Contacts: []Contact{}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"contacts":[]`) {
		t.Errorf("empty list did not serialise as []: %s", payload)
	}
}

// The role is the whole point of reading contacts off a project: "Mario Serrano"
// alone does not say whether he is the client's responsible or a bystander.
func TestAContactCarriesItsRole(t *testing.T) {
	payload, err := json.Marshal(Contact{
		ID: 42, Lastname: "Serrano", Firstname: "Mario",
		RoleCode: "CUSTOMER", RoleLabel: "Responsable Cliente",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"role_code":"CUSTOMER"`, `"role_label":"Responsable Cliente"`} {
		if !strings.Contains(string(payload), want) {
			t.Errorf("missing %s in %s", want, payload)
		}
	}
}

// c_type_contact.element carries the TABLE name minus the llx_ prefix
// ("projet", "societe"), not the MCP entity name ("projects"). Getting this
// wrong returns an empty list against a project that does have contacts.
func TestTheContactTypeElementIsTheTableName(t *testing.T) {
	if got := contactElement("projects"); got != "projet" {
		t.Errorf("contactElement(projects) = %q, want %q", got, "projet")
	}
	if got := contactElement("customers"); got != "societe" {
		t.Errorf("contactElement(customers) = %q, want %q", got, "societe")
	}
}

func TestAnUnsupportedEntityIsRejectedBeforeAnyQuery(t *testing.T) {
	if _, err := (&DB{}).ListContacts(context.Background(), "invoices", 1); err == nil {
		t.Fatal("an unsupported entity was accepted")
	} else if !errors.Is(err, ErrContactsUnsupportedEntity) {
		t.Errorf("error %q does not wrap ErrContactsUnsupportedEntity", err)
	}
}

// Skipped unless DOLIBARR_IT=1. Point it at a disposable instance:
//
//	DOLIBARR_IT=1 DB_PORT=3307 DB_NAME=dolibarr DB_USER=dolibarr DB_PASS=dolibarr \
//	  go test -run TestContactsAgainstDatabase ./internal/doldb/
func TestContactsAgainstDatabase(t *testing.T) {
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

	var projectID int64
	q := "SELECT ec.element_id FROM " + db.T("element_contact") + " ec" +
		" JOIN " + db.T("c_type_contact") + " tc ON tc.rowid = ec.fk_c_type_contact" +
		" WHERE tc.element = 'projet' ORDER BY ec.rowid DESC LIMIT 1"
	if err := db.QueryRowContext(ctx, q).Scan(&projectID); err != nil {
		t.Skipf("no project with contacts to exercise: %v", err)
	}

	list, err := db.listContactsUncached(ctx, "projects", projectID)
	if err != nil {
		t.Fatalf("list contacts of project %d: %v", projectID, err)
	}
	if list.Count == 0 {
		t.Fatalf("project %d was selected because it has contacts, got none", projectID)
	}
	for _, c := range list.Contacts {
		if c.ID == 0 {
			t.Errorf("contact without id: %+v", c)
		}
		if c.RoleCode == "" {
			t.Errorf("contact %d has no role: %+v", c.ID, c)
		}
	}

	// A project with no contacts is an empty answer, not a failure.
	empty, err := db.listContactsUncached(ctx, "projects", -1)
	if err != nil {
		t.Fatalf("a project with no contacts returned an error: %v", err)
	}
	if empty.Count != 0 || empty.Contacts == nil {
		t.Errorf("want an empty, non-nil list, got %+v", empty)
	}
}
