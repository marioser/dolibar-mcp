package tools

import (
	"context"
	"testing"
)

// Validation lives in doldb and runs before the database is touched, so a
// handler with no DB must still reject a bad project selector.
func TestHandleProjectActivityRequiresExactlyOneSelector(t *testing.T) {
	d := &Deps{}
	for name, in := range map[string]ProjectActivityInput{
		"neither": {},
		"both":    {ProjectID: 476, Ref: "GDI-00"},
	} {
		if _, _, err := d.HandleProjectActivity(context.Background(), nil, in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
