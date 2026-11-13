package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sgsoluciones/dolibarr-mcp/internal/doldb"
)

type ProjectActivityInput struct {
	ProjectID int64  `json:"project_id,omitempty" jsonschema:"Dolibarr project ID. Give exactly one of project_id or ref"`
	Ref       string `json:"ref,omitempty" jsonschema:"Project ref, e.g. GDI-00. Give exactly one of project_id or ref"`
	DateFrom  string `json:"date_from,omitempty" jsonschema:"First day of the period, YYYY-MM-DD, inclusive (optional)"`
	DateTo    string `json:"date_to,omitempty" jsonschema:"Last day of the period, YYYY-MM-DD, inclusive (optional)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Timeline items per page (default 25, max 100)"`
	Offset    int    `json:"offset,omitempty" jsonschema:"Timeline items to skip (default 0)"`
}

// ProjectActivityOutput carries the logbook as decoded JSON. The jsonschema
// description on Result is load-bearing — see GetOutput.
type ProjectActivityOutput struct {
	Result any `json:"result" jsonschema:"Project activity as decoded JSON: project, tasks[], sgtimecontrol_installed, notice, total, limit, offset, timeline[] (each item: type report|report_event|agenda|time, id, date, user_id, user, user_login, task_id, task_ref and one detail object: report, event, agenda or time; a report carries state, comment, observation_reason, coordinates and evidence[] with entry_id, filename, filepath, mimetype)"`
}

func (d *Deps) HandleProjectActivity(ctx context.Context, req *mcp.CallToolRequest, input ProjectActivityInput) (*mcp.CallToolResult, ProjectActivityOutput, error) {
	result, err := d.DB.ProjectActivity(ctx, doldb.ActivityParams{
		ProjectID: input.ProjectID,
		Ref:       input.Ref,
		DateFrom:  input.DateFrom,
		DateTo:    input.DateTo,
		Limit:     input.Limit,
		Offset:    input.Offset,
	})
	if err != nil {
		return nil, ProjectActivityOutput{}, fmt.Errorf("project activity: %w", err)
	}
	return nil, ProjectActivityOutput{Result: result}, nil
}
