package tools

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/mapper"
)

func TestInputSchemaInjectsEnum(t *testing.T) {
	s := inputSchema[SearchInput](map[string][]any{
		"entity": anySlice(mapper.ValidEntities()),
	})
	prop, ok := s.Properties["entity"]
	if !ok {
		t.Fatal("entity property missing from inferred schema")
	}
	if len(prop.Enum) != len(mapper.ValidEntities()) {
		t.Errorf("entity enum has %d values, want %d", len(prop.Enum), len(mapper.ValidEntities()))
	}
	if prop.Enum[0] != "customers" {
		t.Errorf("first enum value = %v, want customers", prop.Enum[0])
	}
}

func TestActionEnums(t *testing.T) {
	ents, verbs := actionEnums()
	if len(ents) == 0 || len(verbs) == 0 {
		t.Fatal("action enums empty")
	}
	// entities must be sorted and drawn from ValidActions keys
	if ents[0] != "orders" {
		t.Errorf("first action entity = %v, want orders (sorted)", ents[0])
	}
	// verbs must be de-duplicated (validate appears under many entities)
	seen := map[any]bool{}
	for _, v := range verbs {
		if seen[v] {
			t.Errorf("duplicate verb in enum: %v", v)
		}
		seen[v] = true
	}
}

// TestRegisterDoesNotPanic proves the SDK accepts every pre-built input schema
// (enum injection included) — AddTool resolves and validates the schema at
// registration, so a malformed schema would panic here.
func TestRegisterDoesNotPanic(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	Register(server, &Deps{}) // handlers are wired, not called — nil deps are fine
}
