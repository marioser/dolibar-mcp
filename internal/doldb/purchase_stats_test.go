package doldb

import (
	"math"
	"testing"
)

// These cases are ported one-to-one from the sgpurchasehistory module's
// test/PurchasePriceStatsTest.php, with the same inputs and the same expected
// values. The product tab in Dolibarr and this server must print the same
// numbers for the same rows; if a case here drifts from its PHP twin, one of
// the two is wrong and nobody can tell which.

func purchaseRow(mod func(*PurchaseRow)) PurchaseRow {
	r := PurchaseRow{
		DatePrice:    "2026-01-15 00:00:00",
		Qty:          1.0,
		NetUnitPrice: 100.0,
		SupplierID:   1,
		SourceType:   "order",
		SourceRef:    "OC0001",
		SourceID:     1,
	}
	if mod != nil {
		mod(&r)
	}
	return r
}

func price(p float64) func(*PurchaseRow) {
	return func(r *PurchaseRow) { r.NetUnitPrice = p }
}

func at(date string, p, qty float64) func(*PurchaseRow) {
	return func(r *PurchaseRow) { r.DatePrice, r.NetUnitPrice, r.Qty = date, p, qty }
}

func defaultStats(rows ...PurchaseRow) PurchaseStats {
	return ComputePurchaseStats(rows, DefaultPurchaseStatsOptions())
}

func near(t *testing.T, label string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s = nil, want %v", label, want)
	}
	if math.Abs(*got-want) > 0.0001 {
		t.Errorf("%s = %v, want %v", label, *got, want)
	}
}

func isNil(t *testing.T, label string, got *float64) {
	t.Helper()
	if got != nil {
		t.Errorf("%s = %v, want nil", label, *got)
	}
}

func TestPercentileIsLinearInterpolation(t *testing.T) {
	s := defaultStats(purchaseRow(price(100)), purchaseRow(price(200)))
	near(t, "percentile", s.Percentile, 190)
	if s.PercentileP != 90 {
		t.Errorf("percentile_p = %d, want 90", s.PercentileP)
	}
}

func TestPercentileIsNeitherAverageNorMax(t *testing.T) {
	s := defaultStats(purchaseRow(price(37.81)), purchaseRow(price(160)), purchaseRow(price(100)))
	near(t, "percentile", s.Percentile, 148)
	if *s.Percentile == *s.AvgSimple || *s.Percentile == *s.MaxPrice {
		t.Errorf("percentile %v must differ from avg %v and max %v", *s.Percentile, *s.AvgSimple, *s.MaxPrice)
	}
}

func TestPercentileOfASinglePurchaseIsItsPrice(t *testing.T) {
	near(t, "percentile", defaultStats(purchaseRow(price(812.34))).Percentile, 812.34)
}

func TestPercentileRespectsTheWindow(t *testing.T) {
	s := ComputePurchaseStats([]PurchaseRow{
		purchaseRow(at("2020-01-01 00:00:00", 9000, 1)),
		purchaseRow(at("2026-01-10 00:00:00", 100, 1)),
		purchaseRow(at("2026-01-15 00:00:00", 200, 1)),
	}, PurchaseStatsOptions{AvgMonths: 12, Percentile: 90})
	if s.AvgSampleCount != 2 {
		t.Errorf("avg_sample_count = %d, want 2", s.AvgSampleCount)
	}
	near(t, "percentile", s.Percentile, 190)
}

func TestPercentileIgnoresExcludedRows(t *testing.T) {
	s := defaultStats(purchaseRow(price(100)), purchaseRow(price(200)),
		purchaseRow(func(r *PurchaseRow) { r.NetUnitPrice, r.Excluded = 0, true }))
	near(t, "percentile", s.Percentile, 190)
}

func TestNoRowsReturnsEmptyStats(t *testing.T) {
	s := defaultStats()
	if s.Count != 0 || s.SuppliersCount != 0 {
		t.Errorf("count=%d suppliers=%d, want 0/0", s.Count, s.SuppliersCount)
	}
	isNil(t, "last_price", s.LastPrice)
	isNil(t, "min_price", s.MinPrice)
	isNil(t, "max_price", s.MaxPrice)
	isNil(t, "avg_weighted", s.AvgWeighted)
	isNil(t, "variation_percent", s.VariationPercent)
	isNil(t, "percentile", s.Percentile)
}

func TestSinglePurchase(t *testing.T) {
	s := defaultStats(purchaseRow(func(r *PurchaseRow) { r.NetUnitPrice, r.Qty = 250, 4 }))
	if s.Count != 1 {
		t.Errorf("count = %d, want 1", s.Count)
	}
	near(t, "last_price", s.LastPrice, 250)
	near(t, "min_price", s.MinPrice, 250)
	near(t, "max_price", s.MaxPrice, 250)
	near(t, "avg_weighted", s.AvgWeighted, 250)
	isNil(t, "variation_percent", s.VariationPercent)
	if s.TotalQty != 4 {
		t.Errorf("total_qty = %v, want 4", s.TotalQty)
	}
}

func TestLastPriceIsLatestDateNotLastElement(t *testing.T) {
	s := defaultStats(
		purchaseRow(at("2026-03-01 00:00:00", 300, 1)),
		purchaseRow(at("2026-05-01 00:00:00", 500, 1)),
		purchaseRow(at("2026-04-01 00:00:00", 400, 1)),
	)
	near(t, "last_price", s.LastPrice, 500)
	if s.Last == nil || s.Last.DatePrice != "2026-05-01 00:00:00" {
		t.Errorf("last row = %+v, want 2026-05-01", s.Last)
	}
	near(t, "previous_price", s.PreviousPrice, 400)
}

func TestVariation(t *testing.T) {
	cases := []struct {
		name          string
		before, after float64
		want          *float64
	}{
		{"up", 100, 115, ptr(15)},
		{"down", 200, 150, ptr(-25)},
		{"previous zero", 0, 150, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := defaultStats(
				purchaseRow(at("2026-01-01 00:00:00", tc.before, 1)),
				purchaseRow(at("2026-02-01 00:00:00", tc.after, 1)),
			)
			if tc.want == nil {
				isNil(t, "variation_percent", s.VariationPercent)
				return
			}
			near(t, "variation_percent", s.VariationPercent, *tc.want)
		})
	}
}

func TestWeightedAverageByQuantity(t *testing.T) {
	s := defaultStats(
		purchaseRow(at("2026-01-01 00:00:00", 100, 10)),
		purchaseRow(at("2026-02-01 00:00:00", 200, 90)),
	)
	near(t, "avg_weighted", s.AvgWeighted, 190)
	near(t, "avg_simple", s.AvgSimple, 150)
}

func TestExcludedRowsStayOutOfStatistics(t *testing.T) {
	s := defaultStats(
		purchaseRow(at("2026-01-01 00:00:00", 100, 1)),
		purchaseRow(func(r *PurchaseRow) {
			r.DatePrice, r.NetUnitPrice, r.Excluded = "2026-06-01 00:00:00", 9999, true
		}),
	)
	if s.Count != 1 || s.ExcludedCount != 1 {
		t.Errorf("count=%d excluded=%d, want 1/1", s.Count, s.ExcludedCount)
	}
	near(t, "max_price", s.MaxPrice, 100)
	near(t, "last_price", s.LastPrice, 100)
}

func TestZeroQuantityCountsForMinMaxButNotWeight(t *testing.T) {
	s := defaultStats(
		purchaseRow(at("2026-01-01 00:00:00", 100, 10)),
		purchaseRow(at("2026-02-01 00:00:00", 500, 0)),
	)
	near(t, "max_price", s.MaxPrice, 500)
	near(t, "last_price", s.LastPrice, 500)
	near(t, "avg_weighted", s.AvgWeighted, 100)
}

func TestAllZeroQuantitiesFallBackToSimpleAverage(t *testing.T) {
	s := defaultStats(
		purchaseRow(at("2026-01-01 00:00:00", 100, 0)),
		purchaseRow(at("2026-02-01 00:00:00", 300, 0)),
	)
	near(t, "avg_weighted", s.AvgWeighted, 200)
}

func TestMinAndMaxWithTheirDates(t *testing.T) {
	s := defaultStats(
		purchaseRow(at("2026-01-01 00:00:00", 300, 1)),
		purchaseRow(at("2026-02-01 00:00:00", 120, 1)),
		purchaseRow(at("2026-03-01 00:00:00", 450, 1)),
	)
	near(t, "min_price", s.MinPrice, 120)
	near(t, "max_price", s.MaxPrice, 450)
	if s.MinDate != "2026-02-01 00:00:00" || s.MaxDate != "2026-03-01 00:00:00" {
		t.Errorf("min_date=%q max_date=%q", s.MinDate, s.MaxDate)
	}
}

func TestWindowDropsOldPurchases(t *testing.T) {
	s := ComputePurchaseStats([]PurchaseRow{
		purchaseRow(at("2023-01-01 00:00:00", 10, 1)),
		purchaseRow(at("2026-01-01 00:00:00", 100, 1)),
		purchaseRow(at("2026-06-01 00:00:00", 200, 1)),
	}, PurchaseStatsOptions{AvgMonths: 12, Percentile: 90})
	near(t, "avg_weighted", s.AvgWeighted, 150)
	near(t, "min_price", s.MinPrice, 10)
	if s.Count != 3 || s.AvgSampleCount != 2 {
		t.Errorf("count=%d avg_sample_count=%d, want 3/2", s.Count, s.AvgSampleCount)
	}
}

func TestWindowZeroMeansWholeHistory(t *testing.T) {
	s := ComputePurchaseStats([]PurchaseRow{
		purchaseRow(at("2020-01-01 00:00:00", 100, 1)),
		purchaseRow(at("2026-06-01 00:00:00", 300, 1)),
	}, PurchaseStatsOptions{AvgMonths: 0, Percentile: 90})
	near(t, "avg_weighted", s.AvgWeighted, 200)
	if s.AvgSampleCount != 2 {
		t.Errorf("avg_sample_count = %d, want 2", s.AvgSampleCount)
	}
}

func TestExplicitReferenceDateWins(t *testing.T) {
	s := ComputePurchaseStats([]PurchaseRow{
		purchaseRow(at("2025-01-01 00:00:00", 100, 1)),
		purchaseRow(at("2026-06-01 00:00:00", 300, 1)),
	}, PurchaseStatsOptions{AvgMonths: 12, Percentile: 90, ReferenceDate: "2025-06-01 00:00:00"})
	near(t, "avg_weighted", s.AvgWeighted, 100)
}

func TestCountsDistinctSuppliers(t *testing.T) {
	s := defaultStats(
		purchaseRow(func(r *PurchaseRow) { r.SupplierID = 1 }),
		purchaseRow(func(r *PurchaseRow) { r.SupplierID, r.DatePrice = 2, "2026-02-01 00:00:00" }),
		purchaseRow(func(r *PurchaseRow) { r.SupplierID, r.DatePrice = 1, "2026-03-01 00:00:00" }),
	)
	if s.SuppliersCount != 2 {
		t.Errorf("suppliers_count = %d, want 2", s.SuppliersCount)
	}
}

// Same date: the row that comes later in the input wins, exactly like the PHP
// class. The caller decides the arrival order (see loadPurchaseRows).
func TestSameDayTieGoesToTheLaterInputRow(t *testing.T) {
	s := defaultStats(
		purchaseRow(func(r *PurchaseRow) { r.DatePrice, r.NetUnitPrice, r.SourceID = "2026-02-01 00:00:00", 100, 10 }),
		purchaseRow(func(r *PurchaseRow) { r.DatePrice, r.NetUnitPrice, r.SourceID = "2026-02-01 00:00:00", 180, 11 }),
	)
	near(t, "last_price", s.LastPrice, 180)
	near(t, "previous_price", s.PreviousPrice, 100)
}

func TestExcludedCreditNotesStayOut(t *testing.T) {
	s := defaultStats(
		purchaseRow(at("2026-01-01 00:00:00", 100, 5)),
		purchaseRow(func(r *PurchaseRow) {
			r.DatePrice, r.NetUnitPrice, r.Qty, r.Excluded = "2026-02-01 00:00:00", 100, -5, true
		}),
	)
	if s.Count != 1 || s.TotalQty != 5 {
		t.Errorf("count=%d total_qty=%v, want 1/5", s.Count, s.TotalQty)
	}
	near(t, "avg_weighted", s.AvgWeighted, 100)
}

func TestMinMaxCompareNumerically(t *testing.T) {
	// The PHP twin guards against string comparison ("9" > "10"); in Go the
	// values are already numbers, so this pins the same outcome.
	s := defaultStats(
		purchaseRow(at("2026-01-01 00:00:00", 9, 1)),
		purchaseRow(at("2026-02-01 00:00:00", 10, 1)),
	)
	near(t, "max_price", s.MaxPrice, 10)
	near(t, "min_price", s.MinPrice, 9)
	near(t, "avg_weighted", s.AvgWeighted, 9.5)
}

func TestSeriesIsSortedAndWithoutExcluded(t *testing.T) {
	s := defaultStats(
		purchaseRow(at("2026-03-01 00:00:00", 300, 1)),
		purchaseRow(at("2026-01-01 00:00:00", 100, 1)),
		purchaseRow(func(r *PurchaseRow) {
			r.DatePrice, r.NetUnitPrice, r.Excluded = "2026-02-01 00:00:00", 9999, true
		}),
	)
	if len(s.Series) != 2 || s.Series[0].Price != 100 || s.Series[1].Price != 300 {
		t.Errorf("series = %+v, want [100 300]", s.Series)
	}
}

// --- subtractMonths: the window anchor --------------------------------------

func TestSubtractMonthsClampsToLastDayOfTargetMonth(t *testing.T) {
	cases := map[string]struct {
		date   string
		months int
		want   string
		ok     bool
	}{
		"31 march minus 1": {"2026-03-31 10:20:30", 1, "2026-02-28 10:20:30", true},
		"leap year":        {"2024-03-31 00:00:00", 1, "2024-02-29 00:00:00", true},
		"crosses year":     {"2026-01-15 00:00:00", 12, "2025-01-15 00:00:00", true},
		"13 months":        {"2026-01-15 00:00:00", 13, "2024-12-15 00:00:00", true},
		"date only":        {"2026-05-31", 1, "2026-04-30 00:00:00", true},
		"zero months":      {"2026-05-31 00:00:00", 0, "", false},
		"unparseable":      {"not a date", 1, "", false},
		"empty":            {"", 1, "", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := subtractMonths(tc.date, tc.months)
			if ok != tc.ok || got != tc.want {
				t.Errorf("subtractMonths(%q, %d) = %q,%v want %q,%v", tc.date, tc.months, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestPercentileEdges(t *testing.T) {
	vals := []float64{30, 10, 20}
	if got := percentileOf(vals, 100); got != 30 {
		t.Errorf("P100 = %v, want 30", got)
	}
	if got := percentileOf(vals, 50); got != 20 {
		t.Errorf("P50 = %v, want 20", got)
	}
	if vals[0] != 30 {
		t.Error("percentileOf must not reorder the caller's slice")
	}
}

func ptr(f float64) *float64 { return &f }
