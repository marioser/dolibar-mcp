package doldb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Timeline item types.
const (
	ActivityReport      = "report"
	ActivityReportEvent = "report_event"
	ActivityAgenda      = "agenda"
	ActivityTime        = "time"
)

const (
	activityDateLayout   = "2006-01-02"
	activityDefaultLimit = 25
	activityMaxLimit     = 100

	sgtimecontrolEventTable = "sgtimecontrol_event"

	activityAbsentNotice = "sgtimecontrol is not installed in this Dolibarr: activity reports are not available, only the agenda and manual time are listed"
)

// sgtimecontrolEntryTable is a variable only so a test can point the reader at
// a table that does not exist and get a real ER_NO_SUCH_TABLE.
var sgtimecontrolEntryTable = "sgtimecontrol_entry"

// ActivityParams selects one project and a slice of its timeline.
type ActivityParams struct {
	ProjectID int64  `json:"project_id,omitempty"`
	Ref       string `json:"ref,omitempty"`
	DateFrom  string `json:"date_from,omitempty"`
	DateTo    string `json:"date_to,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}

// ProjectActivity is the project header, its tasks and a paged, time-ordered
// timeline of everything that happened in it.
type ProjectActivity struct {
	Project                *Project       `json:"project"`
	Tasks                  []ProjectTask  `json:"tasks"`
	SgtimecontrolInstalled bool           `json:"sgtimecontrol_installed"`
	Notice                 string         `json:"notice,omitempty"`
	Total                  int            `json:"total"`
	Limit                  int            `json:"limit"`
	Offset                 int            `json:"offset"`
	Timeline               []ActivityItem `json:"timeline"`
}

// ActivityItem is one timeline entry; exactly one detail pointer is set.
type ActivityItem struct {
	Type      string       `json:"type"`
	ID        int64        `json:"id"` // rowid of the source row
	Date      *time.Time   `json:"date"`
	UserID    *int64       `json:"user_id,omitempty"`
	User      string       `json:"user,omitempty"`
	UserLogin string       `json:"user_login,omitempty"`
	TaskID    *int64       `json:"task_id,omitempty"`
	TaskRef   string       `json:"task_ref,omitempty"`
	Report    *ReportEntry `json:"report,omitempty"`
	Event     *ReportEvent `json:"event,omitempty"`
	Agenda    *AgendaEvent `json:"agenda,omitempty"`
	Time      *TimeSpent   `json:"time,omitempty"`
}

// ReportEntry is a technician's field report (sgtimecontrol_entry). The row id,
// user and task live on the item that carries it.
type ReportEntry struct {
	Ref               *string        `json:"ref"`
	State             string         `json:"state"`
	TaskID            *int64         `json:"-"` // the item carries it; kept for evidence matching
	CustomerID        *int64         `json:"customer_id"`
	CustomerName      string         `json:"customer_name,omitempty"`
	Workday           *time.Time     `json:"workday"`
	StartAt           *time.Time     `json:"start_at"`
	EndAt             *time.Time     `json:"end_at"`
	DurationMinutes   int            `json:"duration_minutes"`
	BreakMinutes      int            `json:"break_minutes"`
	BreakDeclared     bool           `json:"break_declared"`
	PendingAssignment bool           `json:"pending_assignment"`
	PendingCostCenter *string        `json:"pending_cost_center"`
	OutsidePlanning   bool           `json:"outside_planning"`
	NativeTimeID      *int64         `json:"native_time_id"`
	RuleVersionCode   *string        `json:"rule_version_code"`
	LocationLabel     *string        `json:"location_label"`
	Latitude          *float64       `json:"latitude"`
	Longitude         *float64       `json:"longitude"`
	Comment           *string        `json:"comment"`
	EvidenceRef       *string        `json:"evidence_ref"`
	EvidencePayload   *string        `json:"evidence_payload"`
	ObservationReason *string        `json:"observation_reason"`
	AssignmentNote    *string        `json:"assignment_note"`
	DateApproved      *time.Time     `json:"date_approved"`
	DeclaredProgress  *int           `json:"declared_progress"`
	DateCreation      *time.Time     `json:"date_creation"`
	ClientUUID        *string        `json:"client_uuid"`
	ClassifiedAt      *time.Time     `json:"classified_at"`
	Evidence          []EvidenceFile `json:"evidence"`
}

// EvidenceFile names one file attached to a report. Size stays nil:
// llx_ecm_files does not store a size; the download tool (#47) gets it.
type EvidenceFile struct {
	EntryID  int64  `json:"entry_id"`
	Filename string `json:"filename"`
	Filepath string `json:"filepath"`
	Size     *int64 `json:"size"`
	Mimetype string `json:"mimetype"`
}

// ReportEvent is one state change in a report's history (sgtimecontrol_event).
type ReportEvent struct {
	EntryID     int64  `json:"entry_id"`
	EventType   string `json:"event_type"`
	FromState   string `json:"from_state,omitempty"`
	ToState     string `json:"to_state,omitempty"`
	Label       string `json:"label,omitempty"`
	Note        string `json:"note,omitempty"`
	PayloadJSON string `json:"payload_json,omitempty"`
}

// AgendaEvent is an actioncomm row linked to the project.
type AgendaEvent struct {
	Code        string     `json:"code,omitempty"`
	Label       string     `json:"label"`
	Note        string     `json:"note,omitempty"`
	DateEnd     *time.Time `json:"datep2,omitempty"`
	Percent     int        `json:"percent"`
	Location    string     `json:"location,omitempty"`
	ElementType string     `json:"elementtype,omitempty"`
	ElementID   *int64     `json:"fk_element,omitempty"`
}

// TimeSpent is time loaded by hand on a task (llx_element_time), not the time
// sgtimecontrol itself posts when a report is approved.
type TimeSpent struct {
	DurationSeconds int64    `json:"duration_seconds"`
	Note            string   `json:"note,omitempty"`
	THM             *float64 `json:"thm,omitempty"`
}

// normalizeActivityParams validates p and fills defaults. It never touches the
// database, so bad input fails before any query.
func normalizeActivityParams(p ActivityParams) (ActivityParams, error) {
	p.Ref = strings.TrimSpace(p.Ref)
	if p.ProjectID < 0 {
		return p, errors.New("project_id must be positive")
	}
	if (p.ProjectID > 0) == (p.Ref != "") {
		return p, errors.New("give exactly one of project_id or ref")
	}
	var from, to time.Time
	var err error
	if p.DateFrom != "" {
		if from, err = time.Parse(activityDateLayout, p.DateFrom); err != nil {
			return p, fmt.Errorf("date_from must be YYYY-MM-DD: %w", err)
		}
	}
	if p.DateTo != "" {
		if to, err = time.Parse(activityDateLayout, p.DateTo); err != nil {
			return p, fmt.Errorf("date_to must be YYYY-MM-DD: %w", err)
		}
	}
	if p.DateFrom != "" && p.DateTo != "" && from.After(to) {
		return p, errors.New("date_from is after date_to")
	}
	if p.Offset < 0 {
		return p, errors.New("offset cannot be negative")
	}
	if p.Limit <= 0 {
		p.Limit = activityDefaultLimit
	}
	if p.Limit > activityMaxLimit {
		p.Limit = activityMaxLimit
	}
	return p, nil
}

// ProjectActivity returns the project's logbook. Like Fetch, the value is a
// json.RawMessage so a cached entry cannot be mutated by a caller.
func (d *DB) ProjectActivity(ctx context.Context, p ActivityParams) (any, error) {
	p, err := normalizeActivityParams(p)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("project_activity\x00%d\x00%s\x00%s\x00%s\x00%d\x00%d", p.ProjectID, p.Ref, p.DateFrom, p.DateTo, p.Limit, p.Offset)
	return d.cachedRead(key, func() (any, error) {
		out, err := d.projectActivityUncached(ctx, p)
		if err != nil {
			return nil, err
		}
		payload, err := json.Marshal(out)
		if err != nil {
			return nil, fmt.Errorf("encode project activity: %w", err)
		}
		return json.RawMessage(payload), nil
	})
}

func (d *DB) projectActivityUncached(ctx context.Context, p ActivityParams) (*ProjectActivity, error) {
	// Safe to cancel on return: every query below materialises its rows first.
	ctx, cancel := d.queryContext(ctx)
	defer cancel()

	pj, err := d.fetchProject(ctx, p.ProjectID, p.Ref)
	if err != nil {
		return nil, err
	}

	items, err := d.activityReports(ctx, pj.ID, p)
	absent, err := sgtimecontrolAbsent(err)
	if err != nil {
		return nil, err
	}
	if !absent {
		events, err := d.activityEvents(ctx, pj.ID, p)
		if err != nil {
			return nil, err
		}
		items = append(items, events...)
	}
	agenda, err := d.activityAgenda(ctx, pj.ID, p)
	if err != nil {
		return nil, err
	}
	manual, err := d.activityTime(ctx, pj.ID, p, absent)
	if err != nil {
		return nil, err
	}
	items = append(append(items, agenda...), manual...)

	out := buildActivity(pj, items, absent, p)
	if err := d.attachEvidence(ctx, out.Timeline); err != nil {
		return nil, err
	}
	return out, nil
}

// buildActivity moves the tasks out of the project, orders the items by date
// (undated last, ties by type then id) and cuts the requested page.
//
// shortcut: merges every source in memory, fine for a project's volume; move to a UNION ALL with ORDER BY/LIMIT if one project reaches tens of thousands of rows.
func buildActivity(pj *Project, items []ActivityItem, absent bool, p ActivityParams) *ProjectActivity {
	tasks := pj.Tasks
	if tasks == nil {
		tasks = []ProjectTask{}
	}
	pj.Tasks = nil

	rank := map[string]int{ActivityReport: 0, ActivityReportEvent: 1, ActivityAgenda: 2, ActivityTime: 3}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		switch {
		case a.Date == nil && b.Date != nil:
			return false
		case a.Date != nil && b.Date == nil:
			return true
		case a.Date != nil && !a.Date.Equal(*b.Date):
			return a.Date.Before(*b.Date)
		}
		if rank[a.Type] != rank[b.Type] {
			return rank[a.Type] < rank[b.Type]
		}
		return a.ID < b.ID
	})

	start := min(p.Offset, len(items))
	end := min(start+p.Limit, len(items))
	out := &ProjectActivity{
		Project:                pj,
		Tasks:                  tasks,
		SgtimecontrolInstalled: !absent,
		Total:                  len(items),
		Limit:                  p.Limit,
		Offset:                 p.Offset,
		Timeline:               append([]ActivityItem{}, items[start:end]...),
	}
	if absent {
		out.Notice = activityAbsentNotice
	}
	return out
}

// sgtimecontrolAbsent reports whether err only means the module's table does
// not exist. Any other error is returned unchanged.
func sgtimecontrolAbsent(err error) (bool, error) {
	if err == nil {
		return false, nil
	}
	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) && myErr.Number == mysqlNoSuchTable {
		return true, nil
	}
	return false, err
}

// dateClause restricts a date expression to [DateFrom 00:00, DateTo+1d 00:00),
// as wall-clock strings exactly as stored.
func dateClause(expr string, p ActivityParams) (string, []any) {
	var sqlPart string
	var args []any
	if p.DateFrom != "" {
		sqlPart += " AND " + expr + " >= ?"
		args = append(args, p.DateFrom+" 00:00:00")
	}
	if p.DateTo != "" {
		to, _ := time.Parse(activityDateLayout, p.DateTo) // validated by normalizeActivityParams
		sqlPart += " AND " + expr + " < ?"
		args = append(args, to.AddDate(0, 0, 1).Format(activityDateLayout)+" 00:00:00")
	}
	return sqlPart, args
}

func (d *DB) collectItems(ctx context.Context, q string, args []any, scan func(*sql.Rows) (ActivityItem, error)) ([]ActivityItem, error) {
	rows, err := d.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ActivityItem
	for rows.Next() {
		it, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// userCols selects the display name and login of a user alias.
func (d *DB) userCols(alias string) string {
	return fmt.Sprintf("TRIM(CONCAT(COALESCE(%[1]s.firstname,''), ' ', COALESCE(%[1]s.lastname,''))), COALESCE(%[1]s.login,'')", alias)
}

func (d *DB) activityReports(ctx context.Context, projectID int64, p ActivityParams) ([]ActivityItem, error) {
	dateSQL, dateArgs := dateClause("COALESCE(e.start_at, e.workday)", p)
	q := fmt.Sprintf(`SELECT e.rowid, e.fk_user, %s, e.fk_task, COALESCE(t.ref,''),
		e.ref, e.state, e.fk_soc, COALESCE(s.nom,''), e.workday, e.start_at, e.end_at,
		e.duration_minutes, e.break_minutes, e.break_declared, e.pending_assignment,
		e.pending_cost_center, e.outside_planning, e.native_time_id, e.rule_version_code,
		e.location_label, e.latitude, e.longitude, e.comment, e.evidence_ref, e.evidence_payload,
		e.observation_reason, e.assignment_note, e.date_approved, e.declared_progress,
		e.date_creation, e.client_uuid, e.classified_at
	FROM %s e
	LEFT JOIN %s u ON u.rowid = e.fk_user
	LEFT JOIN %s t ON t.rowid = e.fk_task
	LEFT JOIN %s s ON s.rowid = e.fk_soc
	WHERE e.fk_project = ? AND e.entity = ?%s`,
		d.userCols("u"), d.T(sgtimecontrolEntryTable), d.T("user"), d.T("projet_task"), d.T("societe"), dateSQL)

	return d.collectItems(ctx, q, append([]any{projectID, d.Entity()}, dateArgs...), func(rows *sql.Rows) (ActivityItem, error) {
		it := ActivityItem{Type: ActivityReport}
		r := &ReportEntry{Evidence: []EvidenceFile{}}
		err := rows.Scan(&it.ID, &it.UserID, &it.User, &it.UserLogin, &r.TaskID, &it.TaskRef,
			&r.Ref, &r.State, &r.CustomerID, &r.CustomerName, &r.Workday, &r.StartAt, &r.EndAt,
			&r.DurationMinutes, &r.BreakMinutes, &r.BreakDeclared, &r.PendingAssignment,
			&r.PendingCostCenter, &r.OutsidePlanning, &r.NativeTimeID, &r.RuleVersionCode,
			&r.LocationLabel, &r.Latitude, &r.Longitude, &r.Comment, &r.EvidenceRef, &r.EvidencePayload,
			&r.ObservationReason, &r.AssignmentNote, &r.DateApproved, &r.DeclaredProgress,
			&r.DateCreation, &r.ClientUUID, &r.ClassifiedAt)
		it.TaskID = r.TaskID
		it.Date = r.StartAt
		if it.Date == nil {
			it.Date = r.Workday
		}
		it.Report = r
		return it, err
	})
}

func (d *DB) activityEvents(ctx context.Context, projectID int64, p ActivityParams) ([]ActivityItem, error) {
	dateSQL, dateArgs := dateClause("ev.date_event", p)
	q := fmt.Sprintf(`SELECT ev.rowid, ev.fk_user, %s, en.fk_task, COALESCE(t.ref,''),
		ev.fk_entry, ev.event_type, COALESCE(ev.from_state,''), COALESCE(ev.to_state,''),
		COALESCE(ev.label,''), COALESCE(ev.note,''), COALESCE(ev.payload_json,''), ev.date_event
	FROM %s ev
	JOIN %s en ON en.rowid = ev.fk_entry
	LEFT JOIN %s u ON u.rowid = ev.fk_user
	LEFT JOIN %s t ON t.rowid = en.fk_task
	WHERE en.fk_project = ? AND en.entity = ?%s`,
		d.userCols("u"), d.T(sgtimecontrolEventTable), d.T(sgtimecontrolEntryTable), d.T("user"), d.T("projet_task"), dateSQL)

	return d.collectItems(ctx, q, append([]any{projectID, d.Entity()}, dateArgs...), func(rows *sql.Rows) (ActivityItem, error) {
		it := ActivityItem{Type: ActivityReportEvent}
		e := &ReportEvent{}
		err := rows.Scan(&it.ID, &it.UserID, &it.User, &it.UserLogin, &it.TaskID, &it.TaskRef,
			&e.EntryID, &e.EventType, &e.FromState, &e.ToState, &e.Label, &e.Note, &e.PayloadJSON, &it.Date)
		it.Event = e
		return it, err
	})
}

func (d *DB) activityAgenda(ctx context.Context, projectID int64, p ActivityParams) ([]ActivityItem, error) {
	dateSQL, dateArgs := dateClause("a.datep", p)
	q := fmt.Sprintf(`SELECT a.id, COALESCE(a.fk_user_action, a.fk_user_author), %s, a.fk_task, COALESCE(t.ref,''),
		COALESCE(a.code,''), a.label, COALESCE(a.note,''), a.datep, a.datep2, a.percent,
		COALESCE(a.location,''), COALESCE(a.elementtype,''), a.fk_element
	FROM %s a
	LEFT JOIN %s u ON u.rowid = COALESCE(a.fk_user_action, a.fk_user_author)
	LEFT JOIN %s t ON t.rowid = a.fk_task
	WHERE a.fk_project = ? AND a.entity = ?%s`,
		d.userCols("u"), d.T("actioncomm"), d.T("user"), d.T("projet_task"), dateSQL)

	return d.collectItems(ctx, q, append([]any{projectID, d.Entity()}, dateArgs...), func(rows *sql.Rows) (ActivityItem, error) {
		it := ActivityItem{Type: ActivityAgenda}
		a := &AgendaEvent{}
		err := rows.Scan(&it.ID, &it.UserID, &it.User, &it.UserLogin, &it.TaskID, &it.TaskRef,
			&a.Code, &a.Label, &a.Note, &it.Date, &a.DateEnd, &a.Percent, &a.Location, &a.ElementType, &a.ElementID)
		it.Agenda = a
		return it, err
	})
}

// activityTime lists time loaded by hand. The rows sgtimecontrol posts itself
// are left out (they are already the reports); without the module there are
// none to exclude.
func (d *DB) activityTime(ctx context.Context, projectID int64, p ActivityParams, absent bool) ([]ActivityItem, error) {
	dateSQL, dateArgs := dateClause("COALESCE(et.element_datehour, et.element_date)", p)
	exclude := ""
	if !absent {
		exclude = fmt.Sprintf(" AND et.rowid NOT IN (SELECT native_time_id FROM %s WHERE native_time_id IS NOT NULL)", d.T(sgtimecontrolEntryTable))
	}
	q := fmt.Sprintf(`SELECT et.rowid, et.fk_user, %s, et.fk_element, t.ref,
		COALESCE(et.element_datehour, et.element_date), COALESCE(et.element_duration,0), COALESCE(et.note,''), et.thm
	FROM %s et
	JOIN %s t ON t.rowid = et.fk_element
	LEFT JOIN %s u ON u.rowid = et.fk_user
	WHERE et.elementtype = 'task' AND t.fk_projet = ?%s%s`,
		d.userCols("u"), d.T("element_time"), d.T("projet_task"), d.T("user"), exclude, dateSQL)

	return d.collectItems(ctx, q, append([]any{projectID}, dateArgs...), func(rows *sql.Rows) (ActivityItem, error) {
		it := ActivityItem{Type: ActivityTime}
		ts := &TimeSpent{}
		var secs float64
		err := rows.Scan(&it.ID, &it.UserID, &it.User, &it.UserLogin, &it.TaskID, &it.TaskRef,
			&it.Date, &secs, &ts.Note, &ts.THM)
		ts.DurationSeconds = int64(secs)
		it.Time = ts
		return it, err
	})
}

// ecmRow is one llx_ecm_files row, reduced to what evidence matching needs.
type ecmRow struct {
	filepath, filename, srcType string
	srcID                       int64
}

// attachEvidence lists the files of the reports on the returned page only.
// sgtimecontrol stores them in projet/<project ref>/<task ref>/rep<entryid>_*
// when the report has a task (indexed with src_object_type 'projet_task') and
// in sgtimecontrol/entry_<id>/ otherwise.
func (d *DB) attachEvidence(ctx context.Context, page []ActivityItem) error {
	reports := map[int64]*ReportEntry{}
	taskIDs := map[int64]bool{}
	for _, it := range page {
		if it.Type == ActivityReport && it.Report != nil {
			reports[it.ID] = it.Report
			if it.Report.TaskID != nil {
				taskIDs[*it.Report.TaskID] = true
			}
		}
	}
	if len(reports) == 0 {
		return nil
	}

	args := []any{d.Entity()}
	paths := make([]string, 0, len(reports))
	for id := range reports {
		paths = append(paths, "?")
		args = append(args, "sgtimecontrol/entry_"+strconv.FormatInt(id, 10))
	}
	cond := "filepath IN (" + strings.Join(paths, ",") + ")"
	if len(taskIDs) > 0 {
		marks := make([]string, 0, len(taskIDs))
		for id := range taskIDs {
			marks = append(marks, "?")
			args = append(args, id)
		}
		cond += " OR (src_object_type = 'projet_task' AND src_object_id IN (" + strings.Join(marks, ",") + ") AND filename LIKE 'rep%')"
	}
	q := fmt.Sprintf(`SELECT filepath, filename, COALESCE(src_object_type,''), COALESCE(src_object_id,0)
	FROM %s WHERE entity = ? AND (%s) ORDER BY rowid`, d.T("ecm_files"), cond)

	rows, err := d.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	var found []ecmRow
	for rows.Next() {
		var r ecmRow
		if err := rows.Scan(&r.filepath, &r.filename, &r.srcType, &r.srcID); err != nil {
			return err
		}
		found = append(found, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	assignEvidence(reports, found)
	return nil
}

var (
	entryDirRe = regexp.MustCompile(`^sgtimecontrol/entry_(\d+)$`)
	repFileRe  = regexp.MustCompile(`^rep(\d+)_`)
)

// assignEvidence hands each ECM row to the report it belongs to (keyed by entry
// id). A task folder is shared by every report of that task, so a rep<N>_ file
// counts only for entry N and only when the row is indexed under N's own task.
func assignEvidence(reports map[int64]*ReportEntry, rows []ecmRow) {
	seen := map[string]bool{}
	for _, r := range rows {
		var id int64
		var m []string
		if m = entryDirRe.FindStringSubmatch(r.filepath); m != nil {
			id, _ = strconv.ParseInt(m[1], 10, 64)
		} else if m = repFileRe.FindStringSubmatch(r.filename); m != nil && r.srcType == "projet_task" {
			id, _ = strconv.ParseInt(m[1], 10, 64)
			if rep := reports[id]; rep == nil || rep.TaskID == nil || *rep.TaskID != r.srcID {
				continue
			}
		} else {
			continue
		}
		rep := reports[id]
		if rep == nil {
			continue
		}
		key := fmt.Sprintf("%d\x00%s\x00%s", id, r.filepath, r.filename)
		if seen[key] {
			continue
		}
		seen[key] = true
		rep.Evidence = append(rep.Evidence, EvidenceFile{
			EntryID:  id,
			Filename: r.filename,
			Filepath: r.filepath,
			Mimetype: evidenceMimetype(r.filename),
		})
	}
}

func evidenceMimetype(name string) string {
	m := mime.TypeByExtension(strings.ToLower(path.Ext(name)))
	m, _, _ = strings.Cut(m, ";")
	if m == "" {
		return "application/octet-stream"
	}
	return m
}
