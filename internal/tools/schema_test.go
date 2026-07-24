package tools

import (
	"context"
	"encoding/json"
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

// findBoolSubschema walks a marshalled JSON Schema and returns the path of the
// first sub-schema encoded as a bare boolean. A boolean is legal JSON Schema
// shorthand, but strict MCP clients (Claude Code among them) reject it and drop
// the whole tools/list response, so the wire schema must never contain one.
func findBoolSubschema(path string, node any) string {
	switch v := node.(type) {
	case map[string]any:
		for _, key := range []string{"properties", "$defs", "definitions", "patternProperties"} {
			sub, ok := v[key].(map[string]any)
			if !ok {
				continue
			}
			for name, child := range sub {
				childPath := path + "." + key + "." + name
				if _, isBool := child.(bool); isBool {
					return childPath
				}
				if found := findBoolSubschema(childPath, child); found != "" {
					return found
				}
			}
		}
		// additionalProperties is excluded on purpose: a boolean there is the
		// idiomatic open/closed-object switch and strict clients accept it.
		// Only a boolean standing in for a named sub-schema is the defect.
		for _, key := range []string{"items", "not"} {
			child, present := v[key]
			if !present {
				continue
			}
			childPath := path + "." + key
			if _, isBool := child.(bool); isBool {
				return childPath
			}
			if found := findBoolSubschema(childPath, child); found != "" {
				return found
			}
		}
		if ap, present := v["additionalProperties"]; present {
			if _, isBool := ap.(bool); !isBool {
				if found := findBoolSubschema(path+".additionalProperties", ap); found != "" {
					return found
				}
			}
		}
	}
	return ""
}

// TestOutputSchemasHaveNoBooleanSubschemas guards the regression that made the
// server appear "Connected" with zero usable tools: an `any`-typed field infers
// the empty schema, which jsonschema-go marshals as `true`, and Claude Code
// rejects the entire tools/list on it. This drives a real tools/list over an
// in-memory transport, so it asserts the bytes that reach the client.
func TestOutputSchemasHaveNoBooleanSubschemas(t *testing.T) {
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	Register(server, &Deps{})

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "probe", Version: "0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	res, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(res.Tools) == 0 {
		t.Fatal("tools/list returned no tools")
	}

	for _, tool := range res.Tools {
		for label, schema := range map[string]any{
			"inputSchema":  tool.InputSchema,
			"outputSchema": tool.OutputSchema,
		} {
			if schema == nil {
				continue
			}
			raw, err := json.Marshal(schema)
			if err != nil {
				t.Fatalf("%s: marshal %s: %v", tool.Name, label, err)
			}
			var decoded any
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("%s: unmarshal %s: %v", tool.Name, label, err)
			}
			if found := findBoolSubschema(label, decoded); found != "" {
				t.Errorf("%s: boolean sub-schema at %s — strict MCP clients drop the whole tools/list; schema: %s",
					tool.Name, found, raw)
			}
		}
	}
}
