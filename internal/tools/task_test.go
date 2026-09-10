package tools

import (
	"context"
	"testing"
)

// Tasks are a first-class REST resource in Dolibarr (POST /tasks), so they go
// through the generic CRUD pipe rather than needing a tool of their own.
func TestCreateTaskGoesToTheTasksResource(t *testing.T) {
	c := &capture{reply: `35`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	_, _, err := deps.HandleCreate(context.Background(), nil, CreateInput{
		Entity: "tasks",
		Data: map[string]any{
			"label":         "Excavacion",
			"project_id":    483,
			"planned_hours": 28800,
		},
	})
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	if c.path != "tasks" {
		t.Fatalf("path = %q, want %q", c.path, "tasks")
	}
	// The API keys the parent project as fk_project, even though the column is
	// fk_projet. Confirmed on Dolibarr 23 by reading the row back.
	if got := c.body["fk_project"]; got != float64(483) {
		t.Fatalf("fk_project = %#v, want the project id", got)
	}
	if got := c.body["planned_workload"]; got != float64(28800) {
		t.Fatalf("planned_workload = %#v, want the planned hours in seconds", got)
	}
}

// Both ref and label are mandatory: POST /tasks without either answers 400.
// The ref must be the literal "auto" so Dolibarr applies its numbering mask.
func TestCreateTaskAsksDolibarrToNumberIt(t *testing.T) {
	c := &capture{reply: `35`}
	deps, closeSrv := newFakeDolibarr(t, c)
	defer closeSrv()

	_, _, err := deps.HandleCreate(context.Background(), nil, CreateInput{
		Entity: "tasks",
		Data:   map[string]any{"label": "Excavacion", "project_id": 483},
	})
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	if got := c.body["ref"]; got != "auto" {
		t.Fatalf("ref = %#v, want %q so Dolibarr applies its numbering mask", got, "auto")
	}
}

func TestCreateTaskRequiresLabelAndProject(t *testing.T) {
	cases := map[string]map[string]any{
		"no label":   {"project_id": 483},
		"no project": {"label": "Excavacion"},
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			c := &capture{reply: `35`}
			deps, closeSrv := newFakeDolibarr(t, c)
			defer closeSrv()

			_, _, err := deps.HandleCreate(context.Background(), nil, CreateInput{
				Entity: "tasks",
				Data:   data,
			})
			if err == nil {
				t.Fatal("an incomplete task was accepted")
			}
			if c.hits != 0 {
				t.Fatalf("invalid payload reached the server (%d hits)", c.hits)
			}
		})
	}
}
