package doldb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/sgsoluciones/dolibarr-mcp/internal/config"
)

func TestSalesCutoffIsCalendarMonthsBeforeToday(t *testing.T) {
	cases := []struct {
		today  string
		months int
		want   string
	}{
		{"2026-10-04", 12, "2025-10-04"},
		{"2026-03-31", 1, "2026-02-28"},
		{"2026-01-15", 120, "2016-01-15"},
	}
	for _, tc := range cases {
		today, _ := time.Parse("2006-01-02", tc.today)
		if got := salesCutoff(today, tc.months); got != tc.want {
			t.Errorf("salesCutoff(%s, %d) = %s, want %s", tc.today, tc.months, got, tc.want)
		}
	}
}

func TestPurchasesMissingTableIsDetected(t *testing.T) {
	missing := &mysql.MySQLError{Number: 1146, Message: "Table 'dolibarr.llx_sgpurchasehistory_price' doesn't exist"}
	if !isNoSuchTable(fmt.Errorf("query: %w", missing)) {
		t.Error("error 1146 must read as a missing table")
	}
	if isNoSuchTable(&mysql.MySQLError{Number: 1142}) || isNoSuchTable(errors.New("boom")) {
		t.Error("only 1146 is a missing table")
	}
}

func basicInputs() priceHistoryInputs {
	return priceHistoryInputs{
		months:     12,
		percentile: 90,
		order:      []int64{4242, 999},
		products: map[int64]productInfo{
			4242: {ref: "C209405", label: "CABLE CENTELFLEX"},
		},
		missing:            []string{"999", "NO-EXISTE"},
		purchasesAvailable: true,
		purchases: map[int64][]PurchaseRow{
			4242: {
				{RowID: 1, DatePrice: "2025-01-10 00:00:00", Qty: 10, NetUnitPrice: 100, SupplierID: 5, SupplierName: "Proveedor A", SourceType: "order", SourceRef: "OC1"},
				{RowID: 2, DatePrice: "2026-05-20 00:00:00", Qty: 30, NetUnitPrice: 200, SupplierID: 6, SupplierName: "Proveedor B", SourceType: "invoice", SourceRef: "FP7"},
				{RowID: 3, DatePrice: "2026-01-01 00:00:00", Qty: 0, NetUnitPrice: 5, Excluded: true},
			},
		},
		sales: map[int64]salesWindow{4242: {count: 2, avg: 350}},
	}
}

func TestAssembleMapsPurchaseStatsToTheContract(t *testing.T) {
	out := assemblePriceHistory(basicInputs())

	if out.Months != 12 || out.PercentileP != 90 || out.SocID != nil {
		t.Fatalf("header = %d/%d/%v, want 12/90/nil", out.Months, out.PercentileP, out.SocID)
	}
	if len(out.Productos) != 2 {
		t.Fatalf("productos = %d, want 2 (unknown ids stay in the list)", len(out.Productos))
	}

	p := out.Productos[0]
	if p.ProductID != 4242 || p.Ref == nil || *p.Ref != "C209405" || p.Label == nil {
		t.Errorf("product header = %+v", p)
	}
	c := p.Compras
	if c == nil {
		t.Fatal("compras is nil with the module installed")
	}
	// Window anchored on the last purchase (2026-05-20): the 2025-01 row is out.
	if c.CantidadTotal != 2 || c.CantidadVentana != 1 {
		t.Errorf("cantidad_total=%d cantidad_ventana=%d, want 2/1", c.CantidadTotal, c.CantidadVentana)
	}
	near(t, "promedio_ventana", c.PromedioVentana, 200)
	near(t, "percentil", c.Percentil, 200)
	if c.PercentilP != 90 {
		t.Errorf("percentil_p = %d", c.PercentilP)
	}
	want := LastPurchase{Precio: 200, Fecha: "2026-05-20", ProveedorID: 6, Proveedor: "Proveedor B", Documento: "FP7", Tipo: "invoice"}
	if c.Ultima == nil || *c.Ultima != want {
		t.Errorf("ultima = %+v, want %+v", c.Ultima, want)
	}

	v := p.Ventas
	if v.CantidadVentana != 2 {
		t.Errorf("ventas.cantidad_ventana = %d, want 2", v.CantidadVentana)
	}
	near(t, "ventas.promedio_ventana", v.PromedioVentana, 350)
	if v.UltimaCliente != nil {
		t.Errorf("ultima_cliente without customer = %+v, want nil", v.UltimaCliente)
	}

	if strings.Join(out.NoEncontrados, ",") != "999,NO-EXISTE" {
		t.Errorf("no_encontrados = %v", out.NoEncontrados)
	}
}

// The promedio of the contract is the WEIGHTED average: it is the figure the
// product tab prints as "Precio promedio".
func TestPromedioVentanaIsTheWeightedAverage(t *testing.T) {
	in := basicInputs()
	in.purchases[4242] = []PurchaseRow{
		{RowID: 1, DatePrice: "2026-01-01 00:00:00", Qty: 10, NetUnitPrice: 100},
		{RowID: 2, DatePrice: "2026-02-01 00:00:00", Qty: 90, NetUnitPrice: 200},
	}
	near(t, "promedio_ventana", assemblePriceHistory(in).Productos[0].Compras.PromedioVentana, 190)
}

func TestUnknownProductHasEmptyStatsNotAnError(t *testing.T) {
	p := assemblePriceHistory(basicInputs()).Productos[1]

	if p.ProductID != 999 || p.Ref != nil || p.Label != nil {
		t.Errorf("unknown product header = %+v, want ref/label nil", p)
	}
	if p.Compras == nil || p.Compras.CantidadTotal != 0 || p.Compras.Ultima != nil ||
		p.Compras.PromedioVentana != nil || p.Compras.Percentil != nil || p.Compras.PercentilP != 90 {
		t.Errorf("compras = %+v, want empty", p.Compras)
	}
	if p.Ventas.CantidadVentana != 0 || p.Ventas.PromedioVentana != nil {
		t.Errorf("ventas = %+v, want empty", p.Ventas)
	}

	raw, _ := json.Marshal(p)
	for _, key := range []string{`"ref":null`, `"label":null`, `"ultima":null`, `"promedio_ventana":null`, `"ultima_cliente":null`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("JSON must carry %s explicitly: %s", key, raw)
		}
	}
}

func TestMissingModuleGivesNullComprasAndAWarning(t *testing.T) {
	in := basicInputs()
	in.purchasesAvailable = false
	in.purchases = nil

	out := assemblePriceHistory(in)
	if out.Aviso == "" || !strings.Contains(out.Aviso, "sgpurchasehistory") {
		t.Errorf("aviso = %q, want it to name the module", out.Aviso)
	}
	for _, p := range out.Productos {
		if p.Compras != nil {
			t.Errorf("product %d: compras = %+v, want nil without the module", p.ProductID, p.Compras)
		}
	}
	if out.Productos[0].Ventas.CantidadVentana != 2 {
		t.Error("ventas must still be computed without the purchase module")
	}
	raw, _ := json.Marshal(out.Productos[0])
	if !strings.Contains(string(raw), `"compras":null`) {
		t.Errorf("compras must be an explicit null: %s", raw)
	}
}

func TestLastSaleToCustomer(t *testing.T) {
	in := basicInputs()
	in.customerID = 836
	in.lastSales = map[int64]LastSale{4242: {Precio: 410, Fecha: "2026-07-01", Documento: "FA2607-0001", Tipo: "factura"}}

	out := assemblePriceHistory(in)
	if out.SocID == nil || *out.SocID != 836 {
		t.Fatalf("socid = %v, want 836", out.SocID)
	}
	got := out.Productos[0].Ventas.UltimaCliente
	if got == nil || *got != in.lastSales[4242] {
		t.Errorf("ultima_cliente = %+v", got)
	}
	if out.Productos[1].Ventas.UltimaCliente != nil {
		t.Error("a product never sold to the customer has no ultima_cliente")
	}
}

func TestEmptyListsSerializeAsArrays(t *testing.T) {
	out := assemblePriceHistory(priceHistoryInputs{months: 12, percentile: 90})
	raw, _ := json.Marshal(out)
	if !strings.Contains(string(raw), `"productos":[]`) || !strings.Contains(string(raw), `"socid":null`) {
		t.Errorf("unexpected envelope: %s", raw)
	}
	if strings.Contains(string(raw), "no_encontrados") || strings.Contains(string(raw), "aviso") {
		t.Errorf("optional keys must be omitted when empty: %s", raw)
	}
}

// Skipped unless DOLIBARR_IT=1. Read-only. Point it at the LOCAL disposable
// instance, never production:
//
//	DOLIBARR_IT=1 DB_PORT=3307 DB_NAME=... DB_USER=... DB_PASS=... \
//	  go test -run TestPriceHistoryAgainstDatabase -v ./internal/doldb/
//
// DOLIBARR_IT_PRODUCTS (comma-separated ids) and DOLIBARR_IT_CUSTOMER choose
// what to query; the defaults are the local snapshot's Centelflex 4x10 family
// and Ultracem. The full result is logged so it can be compared by hand with
// the product's purchase-history tab.
func TestPriceHistoryAgainstDatabase(t *testing.T) {
	if os.Getenv("DOLIBARR_IT") == "" {
		t.Skip("set DOLIBARR_IT=1 to run against a real Dolibarr database")
	}

	cfg := &config.Config{
		DBHost:       envOr("DB_HOST", "127.0.0.1"),
		DBPort:       atoiOr("DB_PORT", 3306),
		DBName:       envOr("DB_NAME", "dolibarr"),
		DBUser:       envOr("DB_USER", "dolibarr"),
		DBPass:       envOr("DB_PASS", "dolibarr"),
		DBPrefix:     envOr("DB_PREFIX", "llx_"),
		Entity:       atoiOr("DOLIBARR_ENTITY", 1),
		QueryTimeout: 30 * time.Second,
	}
	db, err := New(cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer db.Close()

	var ids []int64
	for _, s := range strings.Split(envOr("DOLIBARR_IT_PRODUCTS", "4242,7604,6204,1302,13836"), ",") {
		var id int64
		if _, err := fmt.Sscan(strings.TrimSpace(s), &id); err == nil {
			ids = append(ids, id)
		}
	}
	params := PriceHistoryParams{
		ProductIDs:  ids,
		ProductRefs: []string{"C209405", "REF-QUE-NO-EXISTE"},
		CustomerID:  int64(atoiOr("DOLIBARR_IT_CUSTOMER", 836)),
		Months:      12,
		Percentile:  90,
	}

	ph, err := db.priceHistoryUncached(context.Background(), params, time.Now())
	if err != nil {
		t.Fatalf("price history: %v", err)
	}
	pretty, _ := json.MarshalIndent(ph, "", "  ")
	t.Logf("dolibarr_price_history %v customer=%d:\n%s", ids, params.CustomerID, pretty)

	// The ref C209405 is id 4242 in the snapshot: it adds a product only when
	// 4242 is not already among the ids.
	want := len(ids) + 1
	for _, id := range ids {
		if id == 4242 {
			want = len(ids)
		}
	}
	if len(ph.Productos) != want {
		t.Errorf("productos = %d, want %d", len(ph.Productos), want)
	}
	if !strings.Contains(strings.Join(ph.NoEncontrados, ","), "REF-QUE-NO-EXISTE") {
		t.Errorf("an unknown ref must be reported in no_encontrados: %v", ph.NoEncontrados)
	}

	// Cross-check the purchase figures against the module's own table with an
	// independent query: the count must match the tab's "Compras" card.
	for _, p := range ph.Productos {
		if p.Compras == nil {
			continue
		}
		var n int
		q := "SELECT COUNT(*) FROM " + db.T(purchaseHistoryTable) + " WHERE entity = ? AND fk_product = ? AND excluded = 0"
		if err := db.QueryRowContext(context.Background(), q, db.Entity(), p.ProductID).Scan(&n); err != nil {
			t.Fatalf("count purchases of %d: %v", p.ProductID, err)
		}
		if p.Compras.CantidadTotal != n {
			t.Errorf("product %d: cantidad_total = %d, table says %d", p.ProductID, p.Compras.CantidadTotal, n)
		}
	}
}
