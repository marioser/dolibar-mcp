package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	ContactActionList   = "list"
	ContactActionUpdate = "update"
)

// ContactEntities are the elements whose contacts can be read. Backs both the
// schema enum and the error message, so the two cannot drift apart.
func ContactEntities() []string { return []string{"customers", "projects"} }

type ContactsInput struct {
	Action string         `json:"action" jsonschema:"list (read the contacts of an element) or update (change one contact's own fields)"`
	Entity string         `json:"entity,omitempty" jsonschema:"Element whose contacts to list: projects|customers. Required for list."`
	ID     int64          `json:"id,omitempty" jsonschema:"Element ID for list (the project id, or the third party id), or the contact id for update."`
	Data   map[string]any `json:"data,omitempty" jsonschema:"Fields to change on update: lastname, firstname, poste, email, phone, phone_mobile. Only the ones being changed."`
}

// The jsonschema tag on Result is load-bearing, not cosmetic: an untagged `any`
// infers the empty schema, which marshals as the boolean `true`, and strict MCP
// clients reject the whole tools/list on it — every tool would disappear.
type ContactsOutput struct {
	Result any `json:"result" jsonschema:"Contacts as decoded JSON: entity, element_id, count, contacts[] with id, fullname, position, email, phone, role_code and role_label"`
}

func (d *Deps) HandleContacts(ctx context.Context, req *mcp.CallToolRequest, input ContactsInput) (*mcp.CallToolResult, ContactsOutput, error) {
	switch input.Action {
	case ContactActionList:
		return d.listContacts(ctx, input)
	case ContactActionUpdate:
		return d.updateContact(ctx, input)
	case "":
		return nil, ContactsOutput{}, fmt.Errorf(
			"action is required: %s or %s", ContactActionList, ContactActionUpdate)
	default:
		return nil, ContactsOutput{}, fmt.Errorf(
			"invalid action %q. Valid: %s, %s", input.Action, ContactActionList, ContactActionUpdate)
	}
}

func (d *Deps) listContacts(ctx context.Context, input ContactsInput) (*mcp.CallToolResult, ContactsOutput, error) {
	if input.Entity == "" {
		return nil, ContactsOutput{}, fmt.Errorf(
			"entity is required to list contacts. Valid: %v", ContactEntities())
	}
	if input.ID <= 0 {
		return nil, ContactsOutput{}, fmt.Errorf("id is required to list contacts")
	}

	result, err := d.DB.ListContacts(ctx, input.Entity, input.ID)
	if err != nil {
		return nil, ContactsOutput{}, fmt.Errorf("contacts of %s %d: %w", input.Entity, input.ID, err)
	}
	return nil, ContactsOutput{Result: result}, nil
}

// updateContact goes through REST, which is the write channel. Reads come
// straight from MySQL; writes never do.
func (d *Deps) updateContact(ctx context.Context, input ContactsInput) (*mcp.CallToolResult, ContactsOutput, error) {
	if input.ID <= 0 {
		return nil, ContactsOutput{}, fmt.Errorf("id is required to update a contact: it is the contact's own id, not the project's")
	}
	if len(input.Data) == 0 {
		return nil, ContactsOutput{}, fmt.Errorf("data is required to update a contact: nothing to change")
	}

	result, err := d.API.Put(ctx, fmt.Sprintf("contacts/%d", input.ID), input.Data)
	if err != nil {
		res, _, _ := writeError(fmt.Sprintf("update contact %d", input.ID), err)
		return res, ContactsOutput{}, nil
	}
	return nil, ContactsOutput{Result: parseResult(result)}, nil
}
