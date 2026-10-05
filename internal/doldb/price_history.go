package doldb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// purchaseHistoryTable is created by the sgpurchasehistory module. Like
// sgproposalversion it only exists where the module is installed, so its
// absence degrades the answer instead of failing it.
const purchaseHistoryTable = "sgpurchasehistory_price"

// purchaseModuleMissing is what a caller reads when the purchase table does
// not exist. Spanish, like every other text of this response contract.
const purchaseModuleMissing = "El módulo sgpurchasehistory no está instalado en este Dolibarr (no existe la tabla %s): no hay estadísticas de compra. Las ventas sí se calcularon."

// PriceHistoryParams is a normalized request: the tool layer validates the
// limits before calling.
type PriceHistoryParams struct {
	ProductIDs  []int64
	ProductRefs []string
	CustomerID  int64 // 0 = no customer
	Months      int
	Percentile  int
}

// PriceHistory is the response contract shared with the sgpurchasehistory
// REST API (GET /sgpurchasehistory/stats, module 1.4.0). The Spanish keys are
// that contract; do not rename them on one side only.
type PriceHistory struct {
	Months      int                   `json:"months"`
	PercentileP int                   `json:"percentile_p"`
	SocID       *int64                `json:"socid"`
	Productos   []ProductPriceHistory `json:"productos"`
	// MCP-only extras, omitted when empty.
	NoEncontrados []string `json:"no_encontrados,omitempty"`
	Aviso         string   `json:"aviso,omitempty"`
}

type ProductPriceHistory struct {
	ProductID int64            `json:"product_id"`
	Ref       *string          `json:"ref"`
	Label     *string          `json:"label"`
	Compras   *PurchaseSummary `json:"compras"`
	Ventas    SalesSummary     `json:"ventas"`
}

type PurchaseSummary struct {
	CantidadTotal   int           `json:"cantidad_total"`
	CantidadVentana int           `json:"cantidad_ventana"`
	PromedioVentana *float64      `json:"promedio_ventana"`
	Percentil       *float64      `json:"percentil"`
	PercentilP      int           `json:"percentil_p"`
	Ultima          *LastPurchase `json:"ultima"`
}

type LastPurchase struct {
	Precio      float64 `json:"precio"`
	Fecha       string  `json:"fecha"`
	ProveedorID int64   `json:"proveedor_id"`
	Proveedor   string  `json:"proveedor"`
	Documento   string  `json:"documento"`
	Tipo        string  `json:"tipo"`
}

type SalesSummary struct {
	CantidadVentana int       `json:"cantidad_ventana"`
	PromedioVentana *float64  `json:"promedio_ventana"`
	UltimaCliente   *LastSale `json:"ultima_cliente"`
}

type LastSale struct {
	Precio    float64 `json:"precio"`
	Fecha     string  `json:"fecha"`
	Documento string  `json:"documento"`
	Tipo      string  `json:"tipo"` // "factura" | "propuesta"
}

// PriceHistory returns the purchase and sale summary of the given products.
// Like Fetch, the value is json.RawMessage so a cached entry is immutable.
func (d *DB) PriceHistory(ctx context.Context, p PriceHistoryParams) (any, error) {
	now := time.Now()
	return d.cachedRead(priceHistoryKey(p, now), func() (any, error) {
		ph, err := d.priceHistoryUncached(ctx, p, now)
		if err != nil {
			return nil, err
		}
		payload, err := json.Marshal(ph)
		if err != nil {
			return nil, fmt.Errorf("encode price history: %w", err)
		}
		return json.RawMessage(payload), nil
	})
}

func (d *DB) priceHistoryUncached(ctx context.Context, p PriceHistoryParams, now time.Time) (*PriceHistory, error) {
	// Safe to cancel on return: every query below materialises its rows first.
	ctx, cancel := d.queryContext(ctx)
	defer cancel()

	in := priceHistoryInputs{months: p.Months, percentile: p.Percentile, customerID: p.CustomerID}

	var err error
	in.order, in.products, in.missing, err = d.resolveProducts(ctx, p.ProductIDs, p.ProductRefs)
	if err != nil {
		return nil, fmt.Errorf("resolve products: %w", err)
	}
	if len(in.order) == 0 {
		return assemblePriceHistory(in), nil
	}

	in.purchases, err = d.loadPurchaseRows(ctx, in.order)
	switch {
	case isNoSuchTable(err):
		in.purchasesAvailable = false
		in.purchasesTable = d.T(purchaseHistoryTable)
	case err != nil:
		return nil, fmt.Errorf("purchase history: %w", err)
	default:
		in.purchasesAvailable = true
	}

	if in.sales, err = d.loadSalesWindow(ctx, in.order, salesCutoff(now, p.Months)); err != nil {
		return nil, fmt.Errorf("sales: %w", err)
	}
	if p.CustomerID > 0 {
		if in.lastSales, err = d.loadLastSales(ctx, in.order, p.CustomerID); err != nil {
			return nil, fmt.Errorf("last sale to customer %d: %w", p.CustomerID, err)
		}
	}

	return assemblePriceHistory(in), nil
}

type productInfo struct {
	ref, label string
}

type salesWindow struct {
	count int
	avg   float64
}

// priceHistoryInputs is everything the database answered, so the shaping of
// the response stays a pure function a test can reach without MySQL.
type priceHistoryInputs struct {
	months, percentile int
	customerID         int64

	order    []int64 // output order: ids as given, then ids resolved from refs
	products map[int64]productInfo
	missing  []string

	purchasesAvailable bool
	purchasesTable     string
	purchases          map[int64][]PurchaseRow

	sales     map[int64]salesWindow
	lastSales map[int64]LastSale
}

func assemblePriceHistory(in priceHistoryInputs) *PriceHistory {
	out := &PriceHistory{
		Months:        in.months,
		PercentileP:   in.percentile,
		Productos:     make([]ProductPriceHistory, 0, len(in.order)),
		NoEncontrados: in.missing,
	}
	if in.customerID > 0 {
		soc := in.customerID
		out.SocID = &soc
	}
	if !in.purchasesAvailable && len(in.order) > 0 {
		table := in.purchasesTable
		if table == "" {
			table = "llx_" + purchaseHistoryTable
		}
		out.Aviso = fmt.Sprintf(purchaseModuleMissing, table)
	}

	opts := PurchaseStatsOptions{AvgMonths: in.months, Percentile: in.percentile}

	for _, id := range in.order {
		item := ProductPriceHistory{ProductID: id}
		if info, ok := in.products[id]; ok {
			ref, label := info.ref, info.label
			item.Ref, item.Label = &ref, &label
		}

		if in.purchasesAvailable {
			s := ComputePurchaseStats(in.purchases[id], opts)
			c := &PurchaseSummary{
				CantidadTotal:   s.Count,
				CantidadVentana: s.AvgSampleCount,
				// The tab's "Precio promedio" card is avg_weighted.
				PromedioVentana: s.AvgWeighted,
				Percentil:       s.Percentile,
				PercentilP:      s.PercentileP,
			}
			if s.Last != nil {
				c.Ultima = &LastPurchase{
					Precio:      s.Last.NetUnitPrice,
					Fecha:       dateOnly(s.Last.DatePrice),
					ProveedorID: s.Last.SupplierID,
					Proveedor:   s.Last.SupplierName,
					Documento:   s.Last.SourceRef,
					Tipo:        s.Last.SourceType,
				}
			}
			item.Compras = c
		}

		if w, ok := in.sales[id]; ok && w.count > 0 {
			avg := w.avg
			item.Ventas.CantidadVentana = w.count
			item.Ventas.PromedioVentana = &avg
		}
		if sale, ok := in.lastSales[id]; ok {
			sale := sale
			item.Ventas.UltimaCliente = &sale
		}

		out.Productos = append(out.Productos, item)
	}
	return out
}

// resolveProducts looks up ids and refs in the configured entity. Ids keep
// their input order, then come the ids found by ref that were not already
// listed. An unknown id stays in the output (with empty stats) and is also
// reported in missing; an unknown ref can only be reported.
func (d *DB) resolveProducts(ctx context.Context, ids []int64, refs []string) ([]int64, map[int64]productInfo, []string, error) {
	products := map[int64]productInfo{}
	byRef := map[string]int64{}

	var conds []string
	var args []any
	args = append(args, d.Entity())
	if len(ids) > 0 {
		conds = append(conds, "rowid IN ("+placeholders(len(ids))+")")
		for _, id := range ids {
			args = append(args, id)
		}
	}
	if len(refs) > 0 {
		conds = append(conds, "ref IN ("+placeholders(len(refs))+")")
		for _, r := range refs {
			args = append(args, r)
		}
	}

	if len(conds) > 0 {
		q := fmt.Sprintf("SELECT rowid, ref, COALESCE(label,'') FROM %s WHERE entity = ? AND (%s)",
			d.T("product"), strings.Join(conds, " OR "))
		rows, err := d.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, nil, nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var info productInfo
			if err := rows.Scan(&id, &info.ref, &info.label); err != nil {
				return nil, nil, nil, err
			}
			products[id] = info
			byRef[info.ref] = id
		}
		if err := rows.Err(); err != nil {
			return nil, nil, nil, err
		}
	}

	var order []int64
	var missing []string
	seen := map[int64]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		order = append(order, id)
		if _, ok := products[id]; !ok {
			missing = append(missing, strconv.FormatInt(id, 10))
		}
	}
	for _, r := range refs {
		id, ok := byRef[r]
		if !ok {
			missing = append(missing, r)
			continue
		}
		if !seen[id] {
			seen[id] = true
			order = append(order, id)
		}
	}
	return order, products, missing, nil
}

// loadPurchaseRows reads the module's history of the products.
//
// Rows arrive oldest first and, within one date, by ascending rowid, so the
// stats' stable sort makes the HIGHEST rowid the most recent purchase of a
// day — the order in which they were recorded.
func (d *DB) loadPurchaseRows(ctx context.Context, ids []int64) (map[int64][]PurchaseRow, error) {
	q := fmt.Sprintf(`SELECT h.rowid, h.fk_product, h.date_price, h.qty, h.net_unit_price, h.excluded,
		h.fk_soc, COALESCE(s.nom,''), h.source_type, COALESCE(h.source_ref,''), h.fk_source
	FROM %s h
	LEFT JOIN %s s ON s.rowid = h.fk_soc
	WHERE h.entity = ? AND h.fk_product IN (%s)
	ORDER BY h.fk_product, h.date_price ASC, h.rowid ASC`,
		d.T(purchaseHistoryTable), d.T("societe"), placeholders(len(ids)))

	rows, err := d.QueryContext(ctx, q, append([]any{d.Entity()}, int64Args(ids)...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64][]PurchaseRow{}
	for rows.Next() {
		var r PurchaseRow
		var date time.Time
		var excluded int
		if err := rows.Scan(&r.RowID, &r.ProductID, &date, &r.Qty, &r.NetUnitPrice, &excluded,
			&r.SupplierID, &r.SupplierName, &r.SourceType, &r.SourceRef, &r.SourceID); err != nil {
			return nil, err
		}
		r.DatePrice = date.Format(sqlDateTime)
		r.Excluded = excluded != 0
		out[r.ProductID] = append(out[r.ProductID], r)
	}
	return out, rows.Err()
}

// netUnitSQL is the unit net price of a sale line in the company currency:
// subprice is already in base currency, the line discount is applied here.
const netUnitSQL = "d.subprice * (1 - COALESCE(d.remise_percent,0) / 100)"

// loadSalesWindow counts and averages the validated (1) or paid (2) standard
// customer-invoice lines of each product dated on or after cutoff. Credit
// notes, deposits and replacements (type != 0) stay out.
func (d *DB) loadSalesWindow(ctx context.Context, ids []int64, cutoff string) (map[int64]salesWindow, error) {
	q := fmt.Sprintf(`SELECT d.fk_product, COUNT(*), AVG(%s)
	FROM %s d
	INNER JOIN %s f ON f.rowid = d.fk_facture
	WHERE f.entity = ? AND f.fk_statut IN (1,2) AND f.type = 0
	  AND f.datef >= ? AND d.fk_product IN (%s)
	GROUP BY d.fk_product`,
		netUnitSQL, d.T("facturedet"), d.T("facture"), placeholders(len(ids)))

	args := append([]any{d.Entity(), cutoff}, int64Args(ids)...)
	rows, err := d.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64]salesWindow{}
	for rows.Next() {
		var id int64
		var w salesWindow
		var avg sql.NullFloat64
		if err := rows.Scan(&id, &w.count, &avg); err != nil {
			return nil, err
		}
		w.avg = avg.Float64
		out[id] = w
	}
	return out, rows.Err()
}

// loadLastSales finds, per product, the most recent sale to the customer at
// any date: a validated/paid standard invoice line first, and for products
// never invoiced to them, a signed (2) or billed (4) proposal line.
func (d *DB) loadLastSales(ctx context.Context, ids []int64, customerID int64) (map[int64]LastSale, error) {
	out := map[int64]LastSale{}

	invoices := fmt.Sprintf(`SELECT d.fk_product, %s, f.datef, f.ref
	FROM %s d
	INNER JOIN %s f ON f.rowid = d.fk_facture
	WHERE f.entity = ? AND f.fk_statut IN (1,2) AND f.type = 0
	  AND f.fk_soc = ? AND d.fk_product IN (%s)
	ORDER BY f.datef DESC, f.rowid DESC, d.rowid DESC`,
		netUnitSQL, d.T("facturedet"), d.T("facture"), placeholders(len(ids)))
	if err := d.collectLastSales(ctx, out, "factura", invoices, customerID, ids); err != nil {
		return nil, err
	}

	var pending []int64
	for _, id := range ids {
		if _, ok := out[id]; !ok {
			pending = append(pending, id)
		}
	}
	if len(pending) == 0 {
		return out, nil
	}

	proposals := fmt.Sprintf(`SELECT d.fk_product, %s, p.datep, p.ref
	FROM %s d
	INNER JOIN %s p ON p.rowid = d.fk_propal
	WHERE p.entity = ? AND p.fk_statut IN (2,4)
	  AND p.fk_soc = ? AND d.fk_product IN (%s)
	ORDER BY p.datep DESC, p.rowid DESC, d.rowid DESC`,
		netUnitSQL, d.T("propaldet"), d.T("propal"), placeholders(len(pending)))
	if err := d.collectLastSales(ctx, out, "propuesta", proposals, customerID, pending); err != nil {
		return nil, err
	}
	return out, nil
}

// collectLastSales keeps the first row per product of a query ordered newest
// first.
func (d *DB) collectLastSales(ctx context.Context, out map[int64]LastSale, kind, q string, customerID int64, ids []int64) error {
	args := append([]any{d.Entity(), customerID}, int64Args(ids)...)
	rows, err := d.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var s LastSale
		var date sql.NullTime
		if err := rows.Scan(&id, &s.Precio, &date, &s.Documento); err != nil {
			return err
		}
		if _, done := out[id]; done {
			continue
		}
		if date.Valid {
			s.Fecha = date.Time.Format("2006-01-02")
		}
		s.Tipo = kind
		out[id] = s
	}
	return rows.Err()
}

// salesCutoff is today minus months calendar months, as a DATE. It clamps the
// day like the purchase window does (31 March - 1 month = 28 February).
func salesCutoff(today time.Time, months int) string {
	cutoff, ok := subtractMonths(today.Format(sqlDateTime), months)
	if !ok {
		return today.Format("2006-01-02")
	}
	return dateOnly(cutoff)
}

func dateOnly(datetime string) string {
	if len(datetime) >= 10 {
		return datetime[:10]
	}
	return datetime
}

func isNoSuchTable(err error) bool {
	var myErr *mysql.MySQLError
	return errors.As(err, &myErr) && myErr.Number == mysqlNoSuchTable
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func int64Args(ids []int64) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

// priceHistoryKey includes today's date: the sales window moves with it.
func priceHistoryKey(p PriceHistoryParams, now time.Time) string {
	var b strings.Builder
	b.WriteString("price_history\x00")
	b.WriteString(now.Format("2006-01-02"))
	fmt.Fprintf(&b, "\x00%d\x00%d\x00%d\x00", p.CustomerID, p.Months, p.Percentile)
	for _, id := range p.ProductIDs {
		fmt.Fprintf(&b, "%d,", id)
	}
	b.WriteString("\x00")
	for _, r := range p.ProductRefs {
		b.WriteString(r)
		b.WriteString("\x01")
	}
	return b.String()
}
