package tools

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
)

func documentDeps(t *testing.T) (*Deps, *capture, func()) {
	t.Helper()
	c := &capture{reply: `"informe.pdf"`}
	deps, closeSrv := newFakeDolibarr(t, c)
	return deps, c, closeSrv
}

// Dolibarr takes an upload as JSON with the bytes base64-encoded, so no multipart
// support is needed in the HTTP client. Verified against Dolibarr 23:
// POST /documents/upload with {filename, modulepart, ref, filecontent, fileencoding}
// answers 200 and the file then shows up in GET /documents.
func TestUploadSendsBase64ToTheDocumentsEndpoint(t *testing.T) {
	deps, c, closeSrv := documentDeps(t)
	defer closeSrv()

	_, _, err := deps.HandleDocument(context.Background(), nil, DocumentInput{
		Action:   "upload",
		Entity:   "projects",
		Ref:      "PJ2012-0080",
		Filename: "informe.pdf",
		Content:  base64.StdEncoding.EncodeToString([]byte("hola")),
	})
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	if c.path != "documents/upload" {
		t.Fatalf("path = %q, want %q", c.path, "documents/upload")
	}
	if got := c.body["modulepart"]; got != "project" {
		t.Fatalf("modulepart = %#v, want %q", got, "project")
	}
	if got := c.body["ref"]; got != "PJ2012-0080" {
		t.Fatalf("ref = %#v, want the document ref", got)
	}
	if got := c.body["fileencoding"]; got != "base64" {
		t.Fatalf("fileencoding = %#v, want %q", got, "base64")
	}
	if got := c.body["filecontent"]; got != base64.StdEncoding.EncodeToString([]byte("hola")) {
		t.Fatalf("filecontent = %#v, want the base64 payload untouched", got)
	}
}

// Uploads are keyed by ref, not id: the file lands in a directory named after the
// document reference. An id alone cannot be resolved by the endpoint.
func TestUploadRequiresARef(t *testing.T) {
	deps, c, closeSrv := documentDeps(t)
	defer closeSrv()

	_, _, err := deps.HandleDocument(context.Background(), nil, DocumentInput{
		Action:   "upload",
		Entity:   "projects",
		Filename: "informe.pdf",
		Content:  base64.StdEncoding.EncodeToString([]byte("hola")),
	})
	if err == nil {
		t.Fatal("an upload without a ref was accepted")
	}
	if c.hits != 0 {
		t.Fatalf("invalid upload reached the server (%d hits)", c.hits)
	}
}

// Raw bytes must never be sent as-is: the endpoint would store corrupted content.
func TestUploadRejectsContentThatIsNotBase64(t *testing.T) {
	deps, c, closeSrv := documentDeps(t)
	defer closeSrv()

	_, _, err := deps.HandleDocument(context.Background(), nil, DocumentInput{
		Action:   "upload",
		Entity:   "projects",
		Ref:      "PJ2012-0080",
		Filename: "informe.pdf",
		Content:  "esto no es base64!!",
	})
	if err == nil {
		t.Fatal("non-base64 content was accepted")
	}
	if !strings.Contains(err.Error(), "base64") {
		t.Fatalf("error %q should say the content must be base64", err)
	}
	if c.hits != 0 {
		t.Fatalf("invalid upload reached the server (%d hits)", c.hits)
	}
}

func TestListUsesTheDocumentsQuery(t *testing.T) {
	deps, c, closeSrv := documentDeps(t)
	defer closeSrv()

	_, _, err := deps.HandleDocument(context.Background(), nil, DocumentInput{
		Action: "list",
		Entity: "projects",
		ID:     1,
	})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}

	if !strings.HasPrefix(c.path, "documents") {
		t.Fatalf("path = %q, want the documents endpoint", c.path)
	}
}

// Every entity that can carry documents maps to the modulepart Dolibarr expects.
func TestEntityModuleparts(t *testing.T) {
	cases := map[string]string{
		"projects":  "project",
		"tasks":     "project_task",
		"proposals": "proposal",
		"orders":    "order",
		"purchases": "supplier_order",
		"customers": "thirdparty",
	}
	for entity, want := range cases {
		t.Run(entity, func(t *testing.T) {
			got, ok := documentModulepart(entity)
			if !ok {
				t.Fatalf("%s has no modulepart", entity)
			}
			if got != want {
				t.Fatalf("modulepart = %q, want %q", got, want)
			}
		})
	}
}

// An empty document folder is not a failure. Dolibarr answers 404 "does not
// return any document" when the directory is empty or not created yet, which is
// exactly the state of a project right after its ref changed — and that is the
// moment proposal-kit asks, to check the attachments followed the rename.
// Verified against the production ERP on project COM-989 (id 571).
func TestListingAProjectWithNoDocumentsIsNotAnError(t *testing.T) {
	c := &capture{
		status: http.StatusNotFound,
		reply:  `{"error":{"code":404,"message":"Not Found: Search for modulepart project with Id 571 or Ref COM-989 does not return any document."}}`,
	}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	_, out, err := deps.HandleDocument(context.Background(), nil, DocumentInput{
		Action: "list",
		Entity: "projects",
		Ref:    "COM-989",
	})
	if err != nil {
		t.Fatalf("listing an empty folder returned an error: %v", err)
	}
	if !out.Success {
		t.Fatalf("Success = false, want true for an empty folder")
	}
	items, ok := out.Result.([]any)
	if !ok {
		t.Fatalf("Result = %#v, want an empty list", out.Result)
	}
	if len(items) != 0 {
		t.Fatalf("Result has %d items, want 0", len(items))
	}
}

// The 404 exception must stay narrow. A server fault is still a fault: swallowing
// it would report "no documents" for a project whose files exist and could not be
// read, and the rename check would pass over lost attachments.
func TestAServerFaultWhileListingIsStillAnError(t *testing.T) {
	c := &capture{status: http.StatusInternalServerError, reply: `{"error":{"code":500,"message":"boom"}}`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	// An API fault travels as CallToolResult{IsError: true}, not as a Go error
	// (see writeError in output.go), so that is what has to survive the 404
	// exception.
	res, out, err := deps.HandleDocument(context.Background(), nil, DocumentInput{
		Action: "list",
		Entity: "projects",
		Ref:    "COM-989",
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatal("a 500 while listing was reported as success")
	}
	if out.Success {
		t.Fatal("a 500 while listing produced Success = true")
	}
}

// Uploading never replaces a file unless the caller says so. Without the flag
// Dolibarr refuses a repeated name, which is what happens when the attachments of
// a renamed project are re-uploaded.
func TestUploadDoesNotOverwriteUnlessAsked(t *testing.T) {
	deps, c, closeSrv := documentDeps(t)
	defer closeSrv()

	base := DocumentInput{
		Action:   "upload",
		Entity:   "projects",
		Ref:      "COM-989",
		Filename: "APU-INT-COM989-v0.xlsx",
		Content:  base64.StdEncoding.EncodeToString([]byte("xlsx bytes")),
	}

	if _, _, err := deps.HandleDocument(context.Background(), nil, base); err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if got := c.body["overwriteifexists"]; got != float64(0) {
		t.Fatalf("overwriteifexists = %#v, want 0 by default", got)
	}

	overwriting := base
	overwriting.Overwrite = true
	if _, _, err := deps.HandleDocument(context.Background(), nil, overwriting); err != nil {
		t.Fatalf("overwriting upload failed: %v", err)
	}
	if got := c.body["overwriteifexists"]; got != float64(1) {
		t.Fatalf("overwriteifexists = %#v, want 1 when overwrite is requested", got)
	}
}

// Dolibarr IGNORES subdir when ref is present. api_documents.class.php branches
// on ref: with it, the destination comes from the object's own directory and
// subdir plays no part; only a call without ref reads subdir at all. And ref is
// required to upload.
//
// Verified against the production ERP: an upload to project COM-989 with
// subdir="internal" landed in projet/COM-989, not projet/COM-989/internal.
//
// Accepting both silently puts the file somewhere other than where the caller
// asked — and the caller asking for "internal" is asking for the one with the
// direct cost in it. Refusing is the only honest answer.
func TestUploadRefusesSubdirBecauseTheCoreIgnoresIt(t *testing.T) {
	deps, c, closeSrv := documentDeps(t)
	defer closeSrv()

	_, _, err := deps.HandleDocument(context.Background(), nil, DocumentInput{
		Action:   "upload",
		Entity:   "projects",
		Ref:      "COM-989",
		Subdir:   "internal",
		Filename: "APU-INT-COM989-v0.xlsx",
		Content:  base64.StdEncoding.EncodeToString([]byte("xlsx")),
	})

	if err == nil {
		t.Fatal("an upload with subdir was accepted; the core would have ignored it")
	}
	if !strings.Contains(err.Error(), "subdir") {
		t.Errorf("the error does not name the offending parameter: %v", err)
	}
	if c.hits != 0 {
		t.Errorf("the request left the process anyway (%d hits)", c.hits)
	}
}
