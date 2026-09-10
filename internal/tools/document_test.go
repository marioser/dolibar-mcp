package tools

import (
	"context"
	"encoding/base64"
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
