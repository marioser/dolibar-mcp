package doldb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// --- unit: no database needed ---

func TestActivityRejectsAmbiguousProject(t *testing.T) {
	cases := map[string]ActivityParams{
		"neither":         {},
		"both":            {ProjectID: 476, Ref: "GDI-00"},
		"blank ref":       {Ref: "   "},
		"negative id":     {ProjectID: -1},
		"bad date_from":   {ProjectID: 476, DateFrom: "10/07/2026"},
		"bad date_to":     {ProjectID: 476, DateTo: "2026-13-01"},
		"from after to":   {ProjectID: 476, DateFrom: "2026-07-10", DateTo: "2026-07-09"},
		"negative offset": {ProjectID: 476, Offset: -5},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			var d *DB // validation must fail before the database is touched
			if _, err := d.ProjectActivity(context.Background(), p); err == nil {
				t.Fatalf("params %+v were accepted", p)
			}
		})
	}
}

func TestActivityLimitDefaultsAndCaps(t *testing.T) {
	cases := map[int]int{0: 25, -3: 25, 10: 10, 100: 100, 500: 100}
	for in, want := range cases {
		p, err := normalizeActivityParams(ActivityParams{ProjectID: 1, Limit: in})
		if err != nil {
			t.Fatalf("limit %d: %v", in, err)
		}
		if p.Limit != want {
			t.Errorf("limit %d -> %d, want %d", in, p.Limit, want)
		}
	}
}

// The module being absent is a normal state of an instance, not a failure:
// ER_NO_SUCH_TABLE is recognised, everything else still fails the read.
func TestSgtimecontrolAbsentOnMissingTable(t *testing.T) {
	missing := fmt.Errorf("query: %w", &mysql.MySQLError{Number: 1146, Message: "Table 'dolibarr.llx_sgtimecontrol_entry' doesn't exist"})
	absent, err := sgtimecontrolAbsent(missing)
	if !absent || err != nil {
		t.Fatalf("1146: absent=%v err=%v, want absent and no error", absent, err)
	}

	denied := &mysql.MySQLError{Number: 1142, Message: "SELECT command denied"}
	absent, err = sgtimecontrolAbsent(denied)
	if absent || !errors.Is(err, denied) {
		t.Fatalf("1142: absent=%v err=%v, want the error passed through", absent, err)
	}

	if absent, err = sgtimecontrolAbsent(nil); absent || err != nil {
		t.Fatalf("nil: absent=%v err=%v", absent, err)
	}
}

// With the module absent the answer still carries the project and its tasks,
// and says why there are no reports.
func TestActivityWithoutSgtimecontrolKeepsProjectAndTasks(t *testing.T) {
	pj := &Project{ID: 9, Ref: "PJ9", Tasks: []ProjectTask{{ID: 1, Ref: "T1"}}}
	out := buildActivity(pj, nil, true, ActivityParams{Limit: 25})

	if out.Project == nil || out.Project.ID != 9 {
		t.Fatalf("project lost: %+v", out.Project)
	}
	if len(out.Tasks) != 1 || out.Tasks[0].ID != 1 {
		t.Fatalf("tasks lost: %+v", out.Tasks)
	}
	if out.Project.Tasks != nil {
		t.Errorf("tasks are duplicated inside project: %+v", out.Project.Tasks)
	}
	if out.SgtimecontrolInstalled {
		t.Error("sgtimecontrol_installed = true with the module absent")
	}
	if !strings.Contains(out.Notice, "sgtimecontrol") {
		t.Errorf("notice %q does not say the module is absent", out.Notice)
	}
	raw, _ := json.Marshal(out)
	if !strings.Contains(string(raw), `"timeline":[]`) {
		t.Errorf("an empty timeline must serialise as []: %s", raw)
	}
}

func TestActivityTimelineIsSortedAndPaged(t *testing.T) {
	at := func(s string) *time.Time { v, _ := time.Parse("2006-01-02 15:04", s); return &v }
	items := []ActivityItem{
		{Type: ActivityAgenda, ID: 3, Date: at("2026-07-10 09:00")},
		{Type: ActivityReport, ID: 1, Date: at("2026-07-09 08:00")},
		{Type: ActivityReportEvent, ID: 7, Date: nil},
		{Type: ActivityTime, ID: 2, Date: at("2026-07-10 08:00")},
	}
	out := buildActivity(&Project{ID: 1}, items, false, ActivityParams{Limit: 2, Offset: 1})
	if out.Total != 4 {
		t.Fatalf("total = %d, want 4", out.Total)
	}
	if len(out.Timeline) != 2 || out.Timeline[0].ID != 2 || out.Timeline[1].ID != 3 {
		t.Fatalf("page = %+v, want items 2 then 3", out.Timeline)
	}
}

// Evidence comes from both folders sgtimecontrol writes to; files of another
// report sharing the task folder, or indexed under another task, stay out.
func TestAssignEvidence(t *testing.T) {
	task30 := int64(30)
	reports := map[int64]*ReportEntry{
		36: {TaskID: &task30},
		25: {},
	}
	rows := []ecmRow{
		{filepath: "projet/GDI-00/30", filename: "rep36_20260710-1_a.jpg", srcType: "projet_task", srcID: 30},
		{filepath: "projet/GDI-00/30", filename: "rep37_20260710-1_b.jpg", srcType: "projet_task", srcID: 30},
		{filepath: "projet/OLD/30", filename: "rep36_20260710-1_c.pdf", srcType: "projet_task", srcID: 31},
		{filepath: "projet/GDI-00/30", filename: "plan.pdf", srcType: "projet_task", srcID: 30},
		{filepath: "sgtimecontrol/entry_25", filename: "20260706-1_d.png"},
		{filepath: "sgtimecontrol/entry_250", filename: "20260706-1_e.png"},
	}
	assignEvidence(reports, rows)

	if got := reports[36].Evidence; len(got) != 1 || got[0].Filename != "rep36_20260710-1_a.jpg" || got[0].EntryID != 36 {
		t.Fatalf("report 36 evidence = %+v", got)
	}
	if got := reports[36].Evidence[0].Mimetype; got != "image/jpeg" {
		t.Errorf("mimetype = %q, want image/jpeg", got)
	}
	if got := reports[25].Evidence; len(got) != 1 || got[0].Filename != "20260706-1_d.png" || got[0].Mimetype != "image/png" {
		t.Fatalf("report 25 evidence = %+v", got)
	}
}

// --- integration: DOLIBARR_IT=1 DB_PORT=3307 go test -count=1 ./internal/doldb/ ---

func activity(t *testing.T, db *DB, p ActivityParams) *ProjectActivity {
	t.Helper()
	v, err := db.ProjectActivity(context.Background(), p)
	if err != nil {
		t.Fatalf("project activity %+v: %v", p, err)
	}
	raw, ok := v.(json.RawMessage)
	if !ok {
		t.Fatalf("ProjectActivity returned %T, want json.RawMessage", v)
	}
	var out ProjectActivity
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return &out
}

func countInt(t *testing.T, db *DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", q, err)
	}
	return n
}

func TestActivityProject476ReportsOfTask30(t *testing.T) {
	db := itDB(t)
	want := countInt(t, db, "SELECT COUNT(*) FROM "+db.T("sgtimecontrol_entry")+" WHERE fk_project = 476 AND fk_task = 30 AND entity = ?", db.Entity())
	if want == 0 {
		t.Skip("project 476 / task 30 has no reports in this instance")
	}

	out := activity(t, db, ActivityParams{ProjectID: 476, Limit: 100})
	if out.Project == nil || out.Project.ID != 476 || !out.SgtimecontrolInstalled {
		t.Fatalf("project=%+v installed=%v", out.Project, out.SgtimecontrolInstalled)
	}
	if len(out.Tasks) == 0 {
		t.Fatal("tasks missing")
	}

	var reports []ActivityItem
	var prev *time.Time
	cctv := false
	for _, it := range out.Timeline {
		if it.Date != nil && prev != nil && it.Date.Before(*prev) {
			t.Fatalf("timeline not sorted: %v after %v", it.Date, prev)
		}
		if it.Date != nil {
			prev = it.Date
		}
		if it.Type != ActivityReport || it.Report == nil || it.TaskID == nil || *it.TaskID != 30 {
			continue
		}
		reports = append(reports, it)
		if it.Report.Comment != nil && *it.Report.Comment == "Mantenimiento de cctv" {
			cctv = true
			if it.User == "" || it.UserID == nil {
				t.Errorf("report %d has no technician: %+v", it.ID, it)
			}
		}
		if it.Report.Evidence == nil {
			t.Errorf("report %d: evidence must be [] when empty, got null", it.ID)
		}
	}
	if len(reports) != want {
		t.Errorf("task 30 reports = %d, want %d (rows in sgtimecontrol_entry)", len(reports), want)
	}
	if !cctv {
		t.Error(`no report of task 30 carries "Mantenimiento de cctv" in comment`)
	}
	withCoords := 0
	for _, r := range reports {
		if r.Report.Latitude != nil && r.Report.Longitude != nil {
			withCoords++
		}
	}
	if withCoords == 0 {
		t.Error("no report carries latitude/longitude")
	}
}

func TestActivityShowsNonApprovedReports(t *testing.T) {
	db := itDB(t)
	var entryID, projectID int64
	var state, day string
	q := "SELECT rowid, fk_project, state, DATE_FORMAT(COALESCE(start_at, workday), '%Y-%m-%d') FROM " + db.T("sgtimecontrol_entry") +
		" WHERE state NOT LIKE 'approved%' AND fk_project IS NOT NULL AND entity = ? ORDER BY rowid LIMIT 1"
	if err := db.QueryRowContext(context.Background(), q, db.Entity()).Scan(&entryID, &projectID, &state, &day); err != nil {
		t.Skipf("no non-approved report in this instance: %v", err)
	}

	out := activity(t, db, ActivityParams{ProjectID: projectID, DateFrom: day, DateTo: day, Limit: 100})
	for _, it := range out.Timeline {
		if it.Type == ActivityReport && it.ID == entryID {
			if it.Report.State != state {
				t.Fatalf("report %d state = %q, want %q", entryID, it.Report.State, state)
			}
			return
		}
	}
	t.Fatalf("non-approved report %d (%s) missing from project %d on %s", entryID, state, projectID, day)
}

func TestActivityDateFilter(t *testing.T) {
	db := itDB(t)
	out := activity(t, db, ActivityParams{ProjectID: 476, DateFrom: "2026-07-09", DateTo: "2026-07-09", Limit: 100})
	if out.Total == 0 {
		t.Skip("project 476 has no activity on 2026-07-09")
	}
	for _, it := range out.Timeline {
		if it.Date == nil || it.Date.Format("2006-01-02") != "2026-07-09" {
			t.Errorf("%s %d dated %v is outside 2026-07-09", it.Type, it.ID, it.Date)
		}
		if it.Type == ActivityReport && it.ID == 36 {
			t.Error("report 36 (2026-07-10) leaked into 2026-07-09")
		}
	}
}

func TestActivityPagination(t *testing.T) {
	db := itDB(t)
	e := db.Entity()
	want := countInt(t, db, "SELECT COUNT(*) FROM "+db.T("sgtimecontrol_entry")+" WHERE fk_project = 476 AND entity = ?", e) +
		countInt(t, db, "SELECT COUNT(*) FROM "+db.T("sgtimecontrol_event")+" ev JOIN "+db.T("sgtimecontrol_entry")+
			" en ON en.rowid = ev.fk_entry WHERE en.fk_project = 476 AND en.entity = ?", e) +
		countInt(t, db, "SELECT COUNT(*) FROM "+db.T("actioncomm")+" WHERE fk_project = 476 AND entity = ?", e) +
		countInt(t, db, "SELECT COUNT(*) FROM "+db.T("element_time")+" et JOIN "+db.T("projet_task")+
			" t ON t.rowid = et.fk_element WHERE et.elementtype = 'task' AND t.fk_projet = 476 AND et.rowid NOT IN"+
			" (SELECT native_time_id FROM "+db.T("sgtimecontrol_entry")+" WHERE native_time_id IS NOT NULL)")

	full := activity(t, db, ActivityParams{ProjectID: 476, Limit: 500})
	if full.Total != want {
		t.Fatalf("total = %d, want %d", full.Total, want)
	}
	if full.Limit != 100 {
		t.Errorf("limit 500 came back as %d, want 100", full.Limit)
	}
	if exp := min(want, 100); len(full.Timeline) != exp {
		t.Errorf("timeline has %d items, want %d", len(full.Timeline), exp)
	}

	page := activity(t, db, ActivityParams{ProjectID: 476, Limit: 10, Offset: 5})
	if page.Total != want || page.Offset != 5 {
		t.Fatalf("page total=%d offset=%d", page.Total, page.Offset)
	}
	for i, it := range page.Timeline {
		ref := full.Timeline[5+i]
		if it.Type != ref.Type || it.ID != ref.ID {
			t.Fatalf("page item %d = %s %d, want %s %d", i, it.Type, it.ID, ref.Type, ref.ID)
		}
	}

	byRef := activity(t, db, ActivityParams{Ref: full.Project.Ref, Limit: 1})
	if byRef.Project.ID != 476 || byRef.Total != want {
		t.Errorf("by ref: project %d total %d", byRef.Project.ID, byRef.Total)
	}
}

func TestActivityValidationAgainstDatabase(t *testing.T) {
	db := itDB(t)
	for name, p := range map[string]ActivityParams{
		"neither": {},
		"both":    {ProjectID: 476, Ref: "GDI-00"},
	} {
		if _, err := db.ProjectActivity(context.Background(), p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := db.ProjectActivity(context.Background(), ActivityParams{ProjectID: 999999999}); err == nil {
		t.Error("an unknown project was accepted")
	}
}

// A real 1146 from the server: point the reader at a table that does not exist.
func TestActivityModuleAbsentAgainstDatabase(t *testing.T) {
	db := itDB(t)
	saved := sgtimecontrolEntryTable
	sgtimecontrolEntryTable = "sgtimecontrol_entry_absent"
	t.Cleanup(func() { sgtimecontrolEntryTable = saved })

	out := activity(t, db, ActivityParams{ProjectID: 476, Limit: 100})
	if out.SgtimecontrolInstalled || out.Notice == "" {
		t.Fatalf("installed=%v notice=%q, want the module reported absent", out.SgtimecontrolInstalled, out.Notice)
	}
	if out.Project == nil || out.Project.ID != 476 || len(out.Tasks) == 0 {
		t.Fatalf("project/tasks missing: %+v %+v", out.Project, out.Tasks)
	}
	for _, it := range out.Timeline {
		if it.Type == ActivityReport || it.Type == ActivityReportEvent {
			t.Fatalf("%s item with the module absent: %+v", it.Type, it)
		}
	}
}
