package tools

import (
	"github.com/google/jsonschema-go/jsonschema"
)

// anySlice converts a []string to []any for use as a JSON Schema enum.
func anySlice(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// inputSchema infers the JSON Schema for the tool input type T and injects enum
// constraints onto the named properties. This rejects invalid entity/action
// values at the MCP protocol layer (before the handler runs) and makes the
// valid set visible to the client in the tool schema.
//
// It panics on inference failure: the schema is derived from a static Go type,
// so a failure is a programming error that must surface at startup.
func inputSchema[T any](enums map[string][]any) *jsonschema.Schema {
	s, err := jsonschema.For[T](nil)
	if err != nil {
		panic("dolibarr-mcp: input schema inference failed: " + err.Error())
	}
	for prop, vals := range enums {
		if p, ok := s.Properties[prop]; ok {
			p.Enum = vals
		}
	}
	return s
}

func boolPtr(b bool) *bool { return &b }
