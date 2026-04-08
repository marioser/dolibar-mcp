package doldb

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type SearchParams struct {
	Entity     string
	Query      string
	CustomerID int64
	Status     *int
	DateFrom   string
	DateTo     string
	AmountMin  *float64
	AmountMax  *float64
	Limit      int
	Offset     int
}

func (d *DB) Search(ctx context.Context, p SearchParams) ([]SearchResult, int, error) {
	if p.Limit <= 0 {
		p.Limit = 25
	}
	if p.Limit > 100 {
		p.Limit = 100
	}

	switch p.Entity {
	case "proposals":
		return d.searchProposals(ctx, p)
	case "orders":
		return d.searchOrders(ctx, p)
	case "purchases":
		return d.searchPurchases(ctx, p)
	case "customers":
		return d.searchCustomers(ctx, p)
	case "products":
		return d.searchProducts(ctx, p)
	case "projects":
		return d.searchProjects(ctx, p)
	case "warehouses":
		return d.searchWarehouses(ctx, p)
	case "shipments":
		return d.searchShipments(ctx, p)
	case "receptions":
		return d.searchReceptions(ctx, p)
	default:
		return nil, 0, fmt.Errorf("unknown entity: %s", p.Entity)
	}
}

// queryBuilder helps build WHERE clauses with parameters
type queryBuilder struct {
	conditions []string
	args       []any
}

func newQB() *queryBuilder {
	return &queryBuilder{}
}

func (qb *queryBuilder) addEntity(alias string, entity int) {
	qb.conditions = append(qb.conditions, fmt.Sprintf("%s.entity IN (?)", alias))
	qb.args = append(qb.args, entity)
}

func (qb *queryBuilder) addCustomerID(alias string, id int64) {
	if id > 0 {
		qb.conditions = append(qb.conditions, fmt.Sprintf("%s = ?", alias))
		qb.args = append(qb.args, id)
	}
}

func (qb *queryBuilder) addStatus(alias string, status *int) {
	if status != nil {
		qb.conditions = append(qb.conditions, fmt.Sprintf("%s = ?", alias))
		qb.args = append(qb.args, *status)
	}
}

func (qb *queryBuilder) addDateRange(alias, from, to string) {
	if from != "" {
		qb.conditions = append(qb.conditions, fmt.Sprintf("%s >= ?", alias))
		qb.args = append(qb.args, from)
	}
	if to != "" {
		qb.conditions = append(qb.conditions, fmt.Sprintf("%s <= ?", alias))
		qb.args = append(qb.args, to+" 23:59:59")
	}
}

func (qb *queryBuilder) addAmountRange(alias string, min, max *float64) {
	if min != nil {
		qb.conditions = append(qb.conditions, fmt.Sprintf("%s >= ?", alias))
		qb.args = append(qb.args, *min)
	}
	if max != nil {
		qb.conditions = append(qb.conditions, fmt.Sprintf("%s <= ?", alias))
		qb.args = append(qb.args, *max)
	}
}

func (qb *queryBuilder) addTextSearch(query string, aliases ...string) {
	if query == "" {
		return
	}
	like := "%" + query + "%"
	parts := make([]string, len(aliases))
	for i, a := range aliases {
		parts[i] = fmt.Sprintf("%s LIKE ?", a)
		qb.args = append(qb.args, like)
	}
	qb.conditions = append(qb.conditions, "("+strings.Join(parts, " OR ")+")")
}

func (qb *queryBuilder) addLimitOffset(limit, offset int) {
	qb.args = append(qb.args, limit, offset)
}

func (qb *queryBuilder) where() string {
	if len(qb.conditions) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(qb.conditions, " AND ")
}

// --- Entity-specific search implementations ---

func (d *DB) searchProposals(ctx context.Context, p SearchParams) ([]SearchResult, int, error) {
	qb := newQB()
	qb.addEntity("p", d.Entity())
	qb.addCustomerID("p.fk_soc", p.CustomerID)
	qb.addStatus("p.fk_statut", p.Status)
	qb.addDateRange("p.datep", p.DateFrom, p.DateTo)
	qb.addAmountRange("p.total_ht", p.AmountMin, p.AmountMax)
	qb.addTextSearch(p.Query, "p.ref", "s.nom", "p.ref_client")

	base := fmt.Sprintf(`FROM %s p
		JOIN %s s ON s.rowid = p.fk_soc
		LEFT JOIN %s st ON st.id = p.fk_statut`,
		d.T("propal"), d.T("societe"), d.T("c_propalst"))

	where := qb.where()

	// Count
	var total int
	countSQL := "SELECT COUNT(*) " + base + where
	if err := d.QueryRowContext(ctx, countSQL, qb.args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count proposals: %w", err)
	}

	// Results
	selectSQL := fmt.Sprintf(`SELECT p.rowid, p.ref, p.fk_statut, COALESCE(st.label,''),
		p.total_ht, p.total_ttc, p.multicurrency_code,
		p.datep, p.fin_validite,
		s.nom, s.rowid %s%s
		ORDER BY p.datep DESC LIMIT ? OFFSET ?`, base, where)

	qb.addLimitOffset(p.Limit, p.Offset)

	rows, err := d.QueryContext(ctx, selectSQL, qb.args...)
	if err != nil {
		return nil, 0, fmt.Errorf("search proposals: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var date, dateEnd NullableTime
		var currency NullableString
		if err := rows.Scan(&r.ID, &r.Ref, &r.StatusCode, &r.Status,
			&r.TotalHT, &r.TotalTTC, &currency,
			&date, &dateEnd,
			&r.Customer, &r.CustomerID); err != nil {
			return nil, 0, err
		}
		r.Date = date.TimePtr()
		r.DateEnd = dateEnd.TimePtr()
		r.Currency = currency.String()
		results = append(results, r)
	}
	return results, total, nil
}

func (d *DB) searchOrders(ctx context.Context, p SearchParams) ([]SearchResult, int, error) {
	qb := newQB()
	qb.addEntity("c", d.Entity())
	qb.addCustomerID("c.fk_soc", p.CustomerID)
	qb.addStatus("c.fk_statut", p.Status)
	qb.addDateRange("c.date_commande", p.DateFrom, p.DateTo)
	qb.addAmountRange("c.total_ht", p.AmountMin, p.AmountMax)
	qb.addTextSearch(p.Query, "c.ref", "s.nom", "c.ref_client")

	base := fmt.Sprintf(`FROM %s c JOIN %s s ON s.rowid = c.fk_soc`, d.T("commande"), d.T("societe"))
	where := qb.where()

	var total int
	if err := d.QueryRowContext(ctx, "SELECT COUNT(*) "+base+where, qb.args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	selectSQL := fmt.Sprintf(`SELECT c.rowid, c.ref, c.fk_statut, c.total_ht, c.total_ttc,
		c.multicurrency_code, c.date_commande, s.nom, s.rowid %s%s
		ORDER BY c.date_commande DESC LIMIT ? OFFSET ?`, base, where)
	qb.addLimitOffset(p.Limit, p.Offset)

	rows, err := d.QueryContext(ctx, selectSQL, qb.args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var date NullableTime
		var currency NullableString
		if err := rows.Scan(&r.ID, &r.Ref, &r.StatusCode, &r.TotalHT, &r.TotalTTC,
			&currency, &date, &r.Customer, &r.CustomerID); err != nil {
			return nil, 0, err
		}
		r.Status = statusLabel("orders", r.StatusCode)
		r.Date = date.TimePtr()
		r.Currency = currency.String()
		results = append(results, r)
	}
	return results, total, nil
}

func (d *DB) searchPurchases(ctx context.Context, p SearchParams) ([]SearchResult, int, error) {
	qb := newQB()
	qb.addEntity("c", d.Entity())
	qb.addCustomerID("c.fk_soc", p.CustomerID)
	qb.addStatus("c.fk_statut", p.Status)
	qb.addDateRange("c.date_commande", p.DateFrom, p.DateTo)
	qb.addAmountRange("c.total_ht", p.AmountMin, p.AmountMax)
	qb.addTextSearch(p.Query, "c.ref", "s.nom", "c.ref_supplier")

	base := fmt.Sprintf(`FROM %s c JOIN %s s ON s.rowid = c.fk_soc`, d.T("commande_fournisseur"), d.T("societe"))
	where := qb.where()

	var total int
	if err := d.QueryRowContext(ctx, "SELECT COUNT(*) "+base+where, qb.args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	selectSQL := fmt.Sprintf(`SELECT c.rowid, c.ref, c.fk_statut, c.total_ht, c.total_ttc,
		c.multicurrency_code, c.date_commande, s.nom, s.rowid %s%s
		ORDER BY c.date_commande DESC LIMIT ? OFFSET ?`, base, where)
	qb.addLimitOffset(p.Limit, p.Offset)

	rows, err := d.QueryContext(ctx, selectSQL, qb.args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var date NullableTime
		var currency NullableString
		if err := rows.Scan(&r.ID, &r.Ref, &r.StatusCode, &r.TotalHT, &r.TotalTTC,
			&currency, &date, &r.Customer, &r.CustomerID); err != nil {
			return nil, 0, err
		}
		r.Status = statusLabel("purchases", r.StatusCode)
		r.Date = date.TimePtr()
		r.Currency = currency.String()
		results = append(results, r)
	}
	return results, total, nil
}

func (d *DB) searchCustomers(ctx context.Context, p SearchParams) ([]SearchResult, int, error) {
	qb := newQB()
	qb.addEntity("s", d.Entity())
	qb.addStatus("s.status", p.Status)
	qb.addTextSearch(p.Query, "s.nom", "s.name_alias", "s.code_client", "s.email", "s.phone")

	base := fmt.Sprintf(`FROM %s s`, d.T("societe"))
	where := qb.where()

	var total int
	if err := d.QueryRowContext(ctx, "SELECT COUNT(*) "+base+where, qb.args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	selectSQL := fmt.Sprintf(`SELECT s.rowid, COALESCE(s.code_client,''), s.nom, s.status,
		s.client, s.fournisseur, s.datec %s%s
		ORDER BY s.nom ASC LIMIT ? OFFSET ?`, base, where)
	qb.addLimitOffset(p.Limit, p.Offset)

	rows, err := d.QueryContext(ctx, selectSQL, qb.args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var isClient, isSupplier int
		var date NullableTime
		if err := rows.Scan(&r.ID, &r.Ref, &r.Label, &r.StatusCode,
			&isClient, &isSupplier, &date); err != nil {
			return nil, 0, err
		}
		r.Status = statusLabel("customers", r.StatusCode)
		r.Customer = r.Label
		r.Date = date.TimePtr()
		results = append(results, r)
	}
	return results, total, nil
}

func (d *DB) searchProducts(ctx context.Context, p SearchParams) ([]SearchResult, int, error) {
	qb := newQB()
	qb.addEntity("p", d.Entity())
	qb.addStatus("p.tosell", p.Status)
	qb.addAmountRange("p.price", p.AmountMin, p.AmountMax)
	qb.addTextSearch(p.Query, "p.ref", "p.label", "p.description", "p.barcode")

	base := fmt.Sprintf(`FROM %s p`, d.T("product"))
	where := qb.where()

	var total int
	if err := d.QueryRowContext(ctx, "SELECT COUNT(*) "+base+where, qb.args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	selectSQL := fmt.Sprintf(`SELECT p.rowid, p.ref, p.label, p.fk_product_type,
		p.tosell, p.price, p.price_ttc, p.stock, p.datec %s%s
		ORDER BY p.ref ASC LIMIT ? OFFSET ?`, base, where)
	qb.addLimitOffset(p.Limit, p.Offset)

	rows, err := d.QueryContext(ctx, selectSQL, qb.args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var ptype int
		var stock float64
		var date NullableTime
		if err := rows.Scan(&r.ID, &r.Ref, &r.Label, &ptype,
			&r.StatusCode, &r.TotalHT, &r.TotalTTC, &stock, &date); err != nil {
			return nil, 0, err
		}
		if ptype == 1 {
			r.Status = "Service"
		} else {
			r.Status = "Product"
		}
		r.Date = date.TimePtr()
		results = append(results, r)
	}
	return results, total, nil
}

func (d *DB) searchProjects(ctx context.Context, p SearchParams) ([]SearchResult, int, error) {
	qb := newQB()
	qb.addEntity("pj", d.Entity())
	qb.addCustomerID("pj.fk_soc", p.CustomerID)
	qb.addStatus("pj.fk_statut", p.Status)
	qb.addDateRange("pj.dateo", p.DateFrom, p.DateTo)
	qb.addTextSearch(p.Query, "pj.ref", "pj.title", "pj.description")

	base := fmt.Sprintf(`FROM %s pj LEFT JOIN %s s ON s.rowid = pj.fk_soc`,
		d.T("projet"), d.T("societe"))
	where := qb.where()

	var total int
	if err := d.QueryRowContext(ctx, "SELECT COUNT(*) "+base+where, qb.args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	selectSQL := fmt.Sprintf(`SELECT pj.rowid, pj.ref, pj.title, pj.fk_statut,
		pj.budget_amount, pj.dateo, pj.datee, COALESCE(s.nom,''), COALESCE(s.rowid,0) %s%s
		ORDER BY pj.dateo DESC LIMIT ? OFFSET ?`, base, where)
	qb.addLimitOffset(p.Limit, p.Offset)

	rows, err := d.QueryContext(ctx, selectSQL, qb.args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var dateStart, dateEnd NullableTime
		if err := rows.Scan(&r.ID, &r.Ref, &r.Label, &r.StatusCode,
			&r.TotalHT, &dateStart, &dateEnd, &r.Customer, &r.CustomerID); err != nil {
			return nil, 0, err
		}
		r.Status = statusLabel("projects", r.StatusCode)
		r.Date = dateStart.TimePtr()
		r.DateEnd = dateEnd.TimePtr()
		results = append(results, r)
	}
	return results, total, nil
}

func (d *DB) searchWarehouses(ctx context.Context, p SearchParams) ([]SearchResult, int, error) {
	qb := newQB()
	qb.addEntity("w", d.Entity())
	qb.addStatus("w.statut", p.Status)
	qb.addTextSearch(p.Query, "w.ref", "w.lieu", "w.description")

	base := fmt.Sprintf(`FROM %s w`, d.T("entrepot"))
	where := qb.where()

	var total int
	if err := d.QueryRowContext(ctx, "SELECT COUNT(*) "+base+where, qb.args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	selectSQL := fmt.Sprintf(`SELECT w.rowid, w.ref, w.lieu, w.statut %s%s
		ORDER BY w.ref ASC LIMIT ? OFFSET ?`, base, where)
	qb.addLimitOffset(p.Limit, p.Offset)

	rows, err := d.QueryContext(ctx, selectSQL, qb.args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var location NullableString
		if err := rows.Scan(&r.ID, &r.Ref, &location, &r.StatusCode); err != nil {
			return nil, 0, err
		}
		r.Label = location.String()
		r.Status = statusLabel("warehouses", r.StatusCode)
		results = append(results, r)
	}
	return results, total, nil
}

func (d *DB) searchShipments(ctx context.Context, p SearchParams) ([]SearchResult, int, error) {
	qb := newQB()
	qb.addEntity("e", d.Entity())
	qb.addCustomerID("e.fk_soc", p.CustomerID)
	qb.addStatus("e.fk_statut", p.Status)
	qb.addDateRange("e.date_expedition", p.DateFrom, p.DateTo)
	qb.addTextSearch(p.Query, "e.ref", "s.nom", "e.tracking_number")

	base := fmt.Sprintf(`FROM %s e JOIN %s s ON s.rowid = e.fk_soc`, d.T("expedition"), d.T("societe"))
	where := qb.where()

	var total int
	if err := d.QueryRowContext(ctx, "SELECT COUNT(*) "+base+where, qb.args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	selectSQL := fmt.Sprintf(`SELECT e.rowid, e.ref, e.fk_statut, e.date_expedition,
		s.nom, s.rowid %s%s
		ORDER BY e.date_expedition DESC LIMIT ? OFFSET ?`, base, where)
	qb.addLimitOffset(p.Limit, p.Offset)

	rows, err := d.QueryContext(ctx, selectSQL, qb.args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var date NullableTime
		if err := rows.Scan(&r.ID, &r.Ref, &r.StatusCode, &date,
			&r.Customer, &r.CustomerID); err != nil {
			return nil, 0, err
		}
		r.Status = statusLabel("shipments", r.StatusCode)
		r.Date = date.TimePtr()
		results = append(results, r)
	}
	return results, total, nil
}

func (d *DB) searchReceptions(ctx context.Context, p SearchParams) ([]SearchResult, int, error) {
	qb := newQB()
	qb.addEntity("r", d.Entity())
	qb.addCustomerID("r.fk_soc", p.CustomerID)
	qb.addStatus("r.fk_statut", p.Status)
	qb.addDateRange("r.date_reception", p.DateFrom, p.DateTo)
	qb.addTextSearch(p.Query, "r.ref", "s.nom")

	base := fmt.Sprintf(`FROM %s r JOIN %s s ON s.rowid = r.fk_soc`, d.T("reception"), d.T("societe"))
	where := qb.where()

	var total int
	if err := d.QueryRowContext(ctx, "SELECT COUNT(*) "+base+where, qb.args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	selectSQL := fmt.Sprintf(`SELECT r.rowid, r.ref, r.fk_statut, r.date_reception,
		s.nom, s.rowid %s%s
		ORDER BY r.date_reception DESC LIMIT ? OFFSET ?`, base, where)
	qb.addLimitOffset(p.Limit, p.Offset)

	rows, err := d.QueryContext(ctx, selectSQL, qb.args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var date NullableTime
		if err := rows.Scan(&r.ID, &r.Ref, &r.StatusCode, &date,
			&r.Customer, &r.CustomerID); err != nil {
			return nil, 0, err
		}
		r.Status = statusLabel("receptions", r.StatusCode)
		r.Date = date.TimePtr()
		results = append(results, r)
	}
	return results, total, nil
}

// statusLabel helper
func statusLabel(entity string, code int) string {
	// Inline map for performance - same data as response.StatusName but avoids import cycle
	m := map[string]map[int]string{
		"proposals":  {0: "Draft", 1: "Validated", 2: "Signed", 3: "Not signed", 4: "Billed"},
		"orders":     {-1: "Cancelled", 0: "Draft", 1: "Validated", 2: "Shipped partially", 3: "Shipped completely"},
		"purchases":  {0: "Draft", 1: "Validated", 2: "Approved", 3: "Ordered", 4: "Received partially", 5: "Received completely", 6: "Cancelled", 9: "Refused"},
		"projects":   {0: "Draft", 1: "Open", 2: "Closed"},
		"shipments":  {0: "Draft", 1: "Validated", 2: "Closed"},
		"receptions": {0: "Draft", 1: "Validated", 2: "Closed"},
		"customers":  {0: "Closed", 1: "Active"},
		"warehouses": {0: "Closed", 1: "Open"},
	}
	if em, ok := m[entity]; ok {
		if l, ok := em[code]; ok {
			return l
		}
	}
	return fmt.Sprintf("Unknown(%d)", code)
}

// Nullable types for scanning
type NullableTime struct {
	val   interface{}
	valid bool
}

func (n *NullableTime) Scan(src interface{}) error {
	if src == nil {
		n.valid = false
		return nil
	}
	n.valid = true
	n.val = src
	return nil
}

func (n *NullableTime) TimePtr() *time.Time {
	if !n.valid || n.val == nil {
		return nil
	}
	switch v := n.val.(type) {
	case time.Time:
		return &v
	}
	return nil
}

type NullableString struct {
	val   string
	valid bool
}

func (n *NullableString) Scan(src interface{}) error {
	if src == nil {
		n.valid = false
		return nil
	}
	n.valid = true
	switch v := src.(type) {
	case string:
		n.val = v
	case []byte:
		n.val = string(v)
	}
	return nil
}

func (n *NullableString) String() string {
	if !n.valid {
		return ""
	}
	return n.val
}
