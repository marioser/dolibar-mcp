package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DocumentsEndpoint is the Dolibarr REST resource for files. Uploads take the
// bytes base64-encoded inside a JSON body, so the JSON-only HTTP client needs no
// multipart support.
const DocumentsEndpoint = "documents"

const (
	DocumentActionUpload = "upload"
	DocumentActionList   = "list"
)

// entityModuleparts maps an MCP entity to the "modulepart" Dolibarr uses to place
// a file. The readable aliases are the ones the core accepts (see
// api_documents.class.php); the internal names differ and are not used here.
var entityModuleparts = map[string]string{
	"projects":   "project",
	"tasks":      "project_task",
	"proposals":  "proposal",
	"orders":     "order",
	"purchases":  "supplier_order",
	"customers":  "thirdparty",
	"shipments":  "shipment",
	"receptions": "reception",
	"products":   "product",
}

// documentModulepart returns the modulepart for an entity and whether it can
// carry documents at all.
func documentModulepart(entity string) (string, bool) {
	m, ok := entityModuleparts[entity]
	return m, ok
}

// documentEntities lists, sorted, the entities that can carry documents. It backs
// both the tool's enum and the error message, so the two cannot drift apart.
func documentEntities() []string {
	names := make([]string, 0, len(entityModuleparts))
	for e := range entityModuleparts {
		names = append(names, e)
	}
	sort.Strings(names)
	return names
}

func documentEntityList() string {
	return strings.Join(documentEntities(), ", ")
}

type DocumentInput struct {
	Action   string `json:"action" jsonschema:"upload (attach a file) or list (see what is attached)"`
	Entity   string `json:"entity" jsonschema:"Entity the file belongs to: projects|tasks|proposals|orders|purchases|customers|shipments|receptions|products"`
	Ref      string `json:"ref,omitempty" jsonschema:"Document reference (e.g. PJ2012-0080). REQUIRED to upload: the file is stored in a directory named after it."`
	ID       int64  `json:"id,omitempty" jsonschema:"Entity ID. Accepted when listing; uploads need the ref instead."`
	Filename string `json:"filename,omitempty" jsonschema:"File name including its extension, e.g. informe.pdf. Required to upload."`
	Content  string `json:"content,omitempty" jsonschema:"File contents encoded as base64. Required to upload. Raw text is rejected — encode it first or the file is stored corrupted."`
	Subdir   string `json:"subdir,omitempty" jsonschema:"Optional subdirectory inside the entity's document folder."`
}

func (d *Deps) HandleDocument(ctx context.Context, req *mcp.CallToolRequest, input DocumentInput) (*mcp.CallToolResult, WriteOutput, error) {
	modulepart, ok := documentModulepart(input.Entity)
	if !ok {
		return nil, WriteOutput{}, fmt.Errorf(
			"entity %q does not carry documents. Valid: %s", input.Entity, documentEntityList())
	}

	switch input.Action {
	case DocumentActionUpload:
		return d.uploadDocument(ctx, input, modulepart)
	case DocumentActionList:
		return d.listDocuments(ctx, input, modulepart)
	default:
		return nil, WriteOutput{}, fmt.Errorf(
			"invalid action %q. Valid: %s, %s", input.Action, DocumentActionUpload, DocumentActionList)
	}
}

func (d *Deps) uploadDocument(ctx context.Context, input DocumentInput, modulepart string) (*mcp.CallToolResult, WriteOutput, error) {
	// Validate before anything leaves the process: a rejected upload should cost
	// nothing, and corrupted content is worse than a refused call.
	if input.Ref == "" {
		return nil, WriteOutput{}, fmt.Errorf(
			"ref is required to upload: the file is stored in a directory named after the document reference, and an id cannot be resolved to one")
	}
	if input.Filename == "" {
		return nil, WriteOutput{}, fmt.Errorf("filename is required to upload")
	}
	if input.Content == "" {
		return nil, WriteOutput{}, fmt.Errorf("content is required to upload, encoded as base64")
	}
	if _, err := base64.StdEncoding.DecodeString(input.Content); err != nil {
		return nil, WriteOutput{}, fmt.Errorf(
			"content must be base64: %v. Encode the file first — sending raw bytes stores a corrupted file", err)
	}

	payload := map[string]any{
		"filename":     input.Filename,
		"modulepart":   modulepart,
		"ref":          input.Ref,
		"filecontent":  input.Content,
		"fileencoding": "base64",
	}
	if input.Subdir != "" {
		payload["subdir"] = input.Subdir
	}

	result, err := d.API.Post(ctx, DocumentsEndpoint+"/upload", payload)
	if err != nil {
		return writeError("upload "+input.Filename+" to "+input.Entity+" "+input.Ref, err)
	}

	return nil, WriteOutput{
		Success: true,
		Entity:  input.Entity,
		Action:  DocumentActionUpload,
		Result:  parseResult(result),
	}, nil
}

func (d *Deps) listDocuments(ctx context.Context, input DocumentInput, modulepart string) (*mcp.CallToolResult, WriteOutput, error) {
	if input.ID == 0 && input.Ref == "" {
		return nil, WriteOutput{}, fmt.Errorf("id or ref is required to list documents")
	}

	q := url.Values{}
	q.Set("modulepart", modulepart)
	if input.ID > 0 {
		q.Set("id", fmt.Sprintf("%d", input.ID))
	}
	if input.Ref != "" {
		q.Set("ref", input.Ref)
	}
	if input.Subdir != "" {
		q.Set("subdir", input.Subdir)
	}

	result, err := d.API.Get(ctx, DocumentsEndpoint+"?"+q.Encode())
	if err != nil {
		return writeError("list documents of "+input.Entity, err)
	}

	return nil, WriteOutput{
		Success: true,
		Entity:  input.Entity,
		ID:      input.ID,
		Action:  DocumentActionList,
		Result:  parseResult(result),
	}, nil
}
