package tools

import (
	"context"
	"testing"
)

// These cases all fail validation before any API call, so a nil-API Deps is fine.
func TestHandleAction_Validation(t *testing.T) {
	d := &Deps{}
	ctx := context.Background()

	cases := []struct {
		name    string
		in      ActionInput
		wantErr bool
	}{
		{"close proposal without status", ActionInput{Entity: "proposals", ID: 1, Action: "close"}, true},
		{"close proposal invalid status", ActionInput{Entity: "proposals", ID: 1, Action: "close", Status: 5}, true},
		{"invalid action for entity", ActionInput{Entity: "proposals", ID: 1, Action: "makeorder"}, true},
		{"entity without state actions", ActionInput{Entity: "customers", ID: 1, Action: "validate"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := d.HandleAction(ctx, nil, c.in)
			if (err != nil) != c.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, c.wantErr)
			}
		})
	}
}
