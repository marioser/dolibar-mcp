package doldb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// A contact assigned to a project is a RELATION, not an entity: the link lives
// in llx_element_contact (element_id + fk_c_type_contact + fk_socpeople) and
// carries no ref and no status of its own. That is why this is a tool of its
// own instead of another name in mapper.ValidEntities() — that enum is shared
// by create, update and delete, and a relation has nothing to offer them.
//
// It is also why the read goes through SQL: the Dolibarr REST API exposes no
// endpoint for it. api_projects.class.php has get, getByRef, getByRefExt,
// getByMsgId, index, post, getLines, getRoles, put, delete and validate — and
// no getContacts. api_thirdparties.class.php has none either.

var ErrContactsUnsupportedEntity = errors.New("entity does not carry contacts")

// contactElements maps an MCP entity to the value llx_c_type_contact.element
// holds. That column stores the TABLE name without the llx_ prefix — "projet",
// "societe" — not the entity name this server uses. Passing "project" instead
// of "projet" returns an empty list for a project that does have contacts,
// which is the worst possible failure here: it looks like an answer.
var contactElements = map[string]string{
	"projects":  "projet",
	"customers": "societe",
}

func contactElement(entity string) string {
	return contactElements[entity]
}

// Contact is a person of a third party, with the role it plays on the element
// it was read from.
type Contact struct {
	ID        int64  `json:"id"`
	Lastname  string `json:"lastname,omitempty"`
	Firstname string `json:"firstname,omitempty"`
	Fullname  string `json:"fullname,omitempty"`
	Position  string `json:"position,omitempty"`
	Email     string `json:"email,omitempty"`
	Phone     string `json:"phone,omitempty"`
	Mobile    string `json:"mobile,omitempty"`
	CompanyID *int64 `json:"customer_id,omitempty"`
	Company   string `json:"customer_name,omitempty"`
	RoleCode  string `json:"role_code,omitempty"`
	RoleLabel string `json:"role_label,omitempty"`
	Active    int    `json:"active"`
}

type ContactList struct {
	Entity    string    `json:"entity"`
	ElementID int64     `json:"element_id"`
	Count     int       `json:"count"`
	Contacts  []Contact `json:"contacts"`
}

// ListContacts returns the contacts of a project (those assigned to it, with
// their role) or of a customer (every contact of the third party).
func (d *DB) ListContacts(ctx context.Context, entity string, elementID int64) (any, error) {
	if contactElement(entity) == "" {
		return nil, fmt.Errorf("%w: %q. Valid: customers, projects", ErrContactsUnsupportedEntity, entity)
	}
	return d.cachedRead(contactsKey(entity, elementID), func() (any, error) {
		list, err := d.listContactsUncached(ctx, entity, elementID)
		if err != nil {
			return nil, err
		}
		payload, err := json.Marshal(list)
		if err != nil {
			return nil, fmt.Errorf("encode contacts: %w", err)
		}
		return json.RawMessage(payload), nil
	})
}

func (d *DB) listContactsUncached(ctx context.Context, entity string, elementID int64) (*ContactList, error) {
	// Safe to cancel on return: the rows are materialised before we leave.
	ctx, cancel := d.queryContext(ctx)
	defer cancel()

	// A slice literal, not nil: the JSON has to be [] so "no contacts" reads as
	// an answer and not as a missing field.
	list := &ContactList{Entity: entity, ElementID: elementID, Contacts: []Contact{}}

	var q string
	var args []any

	// llx_element_contact has NO entity column, so the multi-entity scope is
	// applied on socpeople, which does.
	if entity == "projects" {
		q = fmt.Sprintf(`SELECT sp.rowid, COALESCE(sp.lastname,''), COALESCE(sp.firstname,''),
			COALESCE(sp.poste,''), COALESCE(sp.email,''), COALESCE(sp.phone,''),
			COALESCE(sp.phone_mobile,''), sp.fk_soc, COALESCE(s.nom,''),
			COALESCE(tc.code,''), COALESCE(tc.libelle,''), COALESCE(sp.statut,0)
		FROM %s ec
		JOIN %s tc ON tc.rowid = ec.fk_c_type_contact
		JOIN %s sp ON sp.rowid = ec.fk_socpeople
		LEFT JOIN %s s ON s.rowid = sp.fk_soc
		WHERE ec.element_id = ? AND tc.element = ? AND tc.source = 'external' AND sp.entity = ?
		ORDER BY tc.position, sp.lastname`,
			d.T("element_contact"), d.T("c_type_contact"), d.T("socpeople"), d.T("societe"))
		args = []any{elementID, contactElement(entity), d.Entity()}
	} else {
		q = fmt.Sprintf(`SELECT sp.rowid, COALESCE(sp.lastname,''), COALESCE(sp.firstname,''),
			COALESCE(sp.poste,''), COALESCE(sp.email,''), COALESCE(sp.phone,''),
			COALESCE(sp.phone_mobile,''), sp.fk_soc, COALESCE(s.nom,''),
			'', '', COALESCE(sp.statut,0)
		FROM %s sp
		LEFT JOIN %s s ON s.rowid = sp.fk_soc
		WHERE sp.fk_soc = ? AND sp.entity = ?
		ORDER BY sp.lastname`,
			d.T("socpeople"), d.T("societe"))
		args = []any{elementID, d.Entity()}
	}

	rows, err := d.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var c Contact
		var companyID sql.NullInt64
		if err := rows.Scan(&c.ID, &c.Lastname, &c.Firstname, &c.Position,
			&c.Email, &c.Phone, &c.Mobile, &companyID, &c.Company,
			&c.RoleCode, &c.RoleLabel, &c.Active); err != nil {
			return nil, err
		}
		c.CompanyID = ScanNullInt64(companyID)
		c.Fullname = fullname(c.Firstname, c.Lastname)
		list.Contacts = append(list.Contacts, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	list.Count = len(list.Contacts)
	return list, nil
}

func fullname(firstname, lastname string) string {
	switch {
	case firstname == "":
		return lastname
	case lastname == "":
		return firstname
	default:
		return firstname + " " + lastname
	}
}

// Its own namespace so it cannot collide with fetchKey or searchKey.
// InvalidateReads clears the whole cache, so writes need nothing extra here.
func contactsKey(entity string, elementID int64) string {
	return "contacts\x00" + entity + "\x00" + fmt.Sprint(elementID)
}
