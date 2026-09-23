package doldb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
)

// proposalVersionTable is the table the sgproposalversion module creates. It
// only exists where the module is installed, so its absence is a normal state
// of a Dolibarr instance rather than a broken query.
const proposalVersionTable = "sgproposalversion"

// mysqlNoSuchTable is ER_NO_SUCH_TABLE.
const mysqlNoSuchTable = 1146

// ErrProposalVersionsNotInstalled reports that the sgproposalversion module is
// absent. Callers get this instead of the raw "Table ... doesn't exist".
var ErrProposalVersionsNotInstalled = errors.New("sgproposalversion is not installed in this Dolibarr")

// ErrProposalNotFound reports a proposal id that does not exist.
var ErrProposalNotFound = errors.New("proposal not found")

// ProposalVersion is one frozen version of a proposal. The snapshot column is
// deliberately left out: it is the whole serialized proposal and would flood a
// list meant to be read at a glance.
type ProposalVersion struct {
	ID           int64      `json:"id"`
	Entity       int        `json:"entity"`
	ProposalID   int64      `json:"fk_propal"`
	VersionNum   int        `json:"version_num"`
	DateCreation *time.Time `json:"date_creation,omitempty"`
	UserID       int64      `json:"fk_user_creat"`
	UserLogin    string     `json:"user_login,omitempty"`
	UserName     string     `json:"user_name,omitempty"`
	Note         string     `json:"note"`
	PDFFilename  string     `json:"pdf_filename,omitempty"`
}

// ProposalVersions is the version history of one proposal.
//
// LiveVersionNum is the number the live proposal carries now, which is also
// the number the next freeze will store: MAX(version_num)+1, or 0 when nothing
// has been frozen yet. It mirrors ProposalVersion::nextVersionNum in the
// module, so this server and Dolibarr always agree on the label.
type ProposalVersions struct {
	ProposalID     int64             `json:"proposal_id"`
	ProposalRef    string            `json:"proposal_ref"`
	LiveVersionNum int               `json:"live_version_num"`
	Count          int               `json:"count"`
	Versions       []ProposalVersion `json:"versions"`
}

// ListProposalVersions returns the frozen versions of a proposal, newest first,
// plus its live version number. Like Fetch, the value is json.RawMessage so a
// cached entry cannot be mutated by a caller.
func (d *DB) ListProposalVersions(ctx context.Context, proposalID int64) (any, error) {
	return d.cachedRead(proposalVersionsKey(proposalID), func() (any, error) {
		pv, err := d.listProposalVersionsUncached(ctx, proposalID)
		if err != nil {
			return nil, err
		}
		payload, err := json.Marshal(pv)
		if err != nil {
			return nil, fmt.Errorf("encode proposal versions: %w", err)
		}
		return json.RawMessage(payload), nil
	})
}

func (d *DB) listProposalVersionsUncached(ctx context.Context, proposalID int64) (*ProposalVersions, error) {
	// Safe to cancel on return: every query below materialises its rows first.
	ctx, cancel := d.queryContext(ctx)
	defer cancel()

	table := d.T(proposalVersionTable)

	// The versions come first so a missing module is reported as such even for
	// a proposal id that happens not to exist.
	//
	// Filtered by entity exactly like ProposalVersion::fetchAllForPropal.
	q := fmt.Sprintf(`SELECT v.rowid, v.entity, v.fk_propal, v.version_num, v.date_creation,
		v.fk_user_creat, COALESCE(u.login,''),
		TRIM(CONCAT(COALESCE(u.firstname,''), ' ', COALESCE(u.lastname,''))),
		COALESCE(v.note,''), COALESCE(v.pdf_filename,'')
	FROM %s v
	LEFT JOIN %s u ON u.rowid = v.fk_user_creat
	WHERE v.fk_propal = ? AND v.entity = ?
	ORDER BY v.version_num DESC`, table, d.T("user"))

	rows, err := d.QueryContext(ctx, q, proposalID, d.Entity())
	if err != nil {
		return nil, proposalVersionsError(err, table)
	}
	defer rows.Close()

	versions := []ProposalVersion{}
	for rows.Next() {
		var v ProposalVersion
		var created sql.NullTime
		if err := rows.Scan(&v.ID, &v.Entity, &v.ProposalID, &v.VersionNum, &created,
			&v.UserID, &v.UserLogin, &v.UserName, &v.Note, &v.PDFFilename); err != nil {
			return nil, err
		}
		v.DateCreation = ScanNullTime(created)
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var ref string
	err = d.QueryRowContext(ctx, fmt.Sprintf("SELECT ref FROM %s WHERE rowid = ?", d.T("propal")), proposalID).Scan(&ref)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: id %d", ErrProposalNotFound, proposalID)
	}
	if err != nil {
		return nil, err
	}

	// Not filtered by entity: ProposalVersion::nextVersionNum numbers across
	// the whole proposal, and the live number must match what the next freeze
	// will actually store.
	var maxNum sql.NullInt64
	if err := d.QueryRowContext(ctx,
		fmt.Sprintf("SELECT MAX(version_num) FROM %s WHERE fk_propal = ?", table),
		proposalID).Scan(&maxNum); err != nil {
		return nil, proposalVersionsError(err, table)
	}

	return &ProposalVersions{
		ProposalID:     proposalID,
		ProposalRef:    ref,
		LiveVersionNum: liveVersionNum(maxNum),
		Count:          len(versions),
		Versions:       versions,
	}, nil
}

// liveVersionNum is MAX(version_num)+1, or 0 when no version exists. MAX is
// NULL on an empty set, and 0 is a real version number (the first freeze
// stores v0), so the two must not be confused.
func liveVersionNum(maxNum sql.NullInt64) int {
	if !maxNum.Valid {
		return 0
	}
	return int(maxNum.Int64) + 1
}

// proposalVersionsError turns "table doesn't exist" into a sentence a person
// can act on. Everything else passes through untouched.
func proposalVersionsError(err error, table string) error {
	var myErr *mysql.MySQLError
	if errors.As(err, &myErr) && myErr.Number == mysqlNoSuchTable {
		return fmt.Errorf("%w (table %s does not exist): enable the module in Dolibarr", ErrProposalVersionsNotInstalled, table)
	}
	return err
}

func proposalVersionsKey(proposalID int64) string {
	return "proposal_versions\x00" + fmt.Sprint(proposalID)
}
