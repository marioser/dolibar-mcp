package doldb

import (
	"sort"
	"strings"
	"time"
)

// This file is a line-by-line port of PurchasePriceStats::compute from the
// sgpurchasehistory Dolibarr module (class/purchasepricestats.class.php). The
// product's purchase-history tab, the module's REST API and this server must
// print the same numbers for the same rows, so every rule here — the window
// anchor, the percentile interpolation, the tie-break — follows the PHP, not
// what would look tidier in Go. The test file ports the PHP tests with their
// exact expected values; change both sides together or neither.

// PercentileDefault is the percentile used to quote. It mirrors
// PurchasePriceStats::PERCENTILE_DEFAULT and the PERCENTIL constant in
// proposal-kit's apu_vs_compras_reales.py.
const PercentileDefault = 90

// AvgMonthsDefault is the default averaging window, in months.
const AvgMonthsDefault = 12

// sqlDateTime is the layout the PHP class compares lexicographically.
const sqlDateTime = "2006-01-02 15:04:05"

// PurchaseRow is one row of llx_sgpurchasehistory_price. DatePrice is kept as
// the 'Y-m-d H:i:s' string the PHP class compares with strcmp.
type PurchaseRow struct {
	RowID        int64
	ProductID    int64
	DatePrice    string
	Qty          float64
	NetUnitPrice float64
	Excluded     bool
	SupplierID   int64
	SupplierName string
	SourceType   string
	SourceRef    string
	SourceID     int64
}

// PurchaseStatsOptions mirrors the $options array of the PHP class.
type PurchaseStatsOptions struct {
	// AvgMonths is the averaging window; 0 means the whole history.
	AvgMonths int
	// ReferenceDate ends the window. Empty means the date of the last purchase.
	ReferenceDate string
	Percentile    int
}

// DefaultPurchaseStatsOptions is what the PHP class uses when no option is set.
func DefaultPurchaseStatsOptions() PurchaseStatsOptions {
	return PurchaseStatsOptions{AvgMonths: AvgMonthsDefault, Percentile: PercentileDefault}
}

// SeriesPoint is one non-excluded purchase in date order.
type SeriesPoint struct {
	Date  string
	Price float64
}

// PurchaseStats mirrors the array PurchasePriceStats::compute returns. Nil
// pointers are the PHP nulls.
type PurchaseStats struct {
	Count            int
	ExcludedCount    int
	Last             *PurchaseRow
	LastPrice        *float64
	PreviousPrice    *float64
	PreviousDate     string
	VariationPercent *float64
	MinPrice         *float64
	MinDate          string
	MaxPrice         *float64
	MaxDate          string
	AvgWeighted      *float64
	AvgSimple        *float64
	AvgSampleCount   int
	Percentile       *float64
	PercentileP      int
	TotalQty         float64
	SuppliersCount   int
	Series           []SeriesPoint
}

// ComputePurchaseStats is the Go twin of PurchasePriceStats::compute.
//
// Rows may come in any order. Rows with the same date keep their input order,
// and the later one counts as more recent — so the caller decides the
// tie-break by the order it passes them in.
func ComputePurchaseStats(rows []PurchaseRow, opts PurchaseStatsOptions) PurchaseStats {
	stats := PurchaseStats{PercentileP: PercentileDefault}

	valid := make([]PurchaseRow, 0, len(rows))
	for _, r := range rows {
		if r.Excluded {
			stats.ExcludedCount++
			continue
		}
		valid = append(valid, r)
	}
	if len(valid) == 0 {
		return stats
	}

	// Stable sort by date string == PHP's usort on (strcmp(date), position).
	sorted := valid
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].DatePrice < sorted[j].DatePrice
	})

	count := len(sorted)
	stats.Count = count

	last := sorted[count-1]
	stats.Last = &last
	stats.LastPrice = fptr(last.NetUnitPrice)

	if count > 1 {
		previous := sorted[count-2]
		stats.PreviousPrice = fptr(previous.NetUnitPrice)
		stats.PreviousDate = previous.DatePrice
		if previous.NetUnitPrice != 0 {
			stats.VariationPercent = fptr((last.NetUnitPrice - previous.NetUnitPrice) / previous.NetUnitPrice * 100)
		}
	}

	suppliers := map[int64]bool{}
	for _, r := range sorted {
		p := r.NetUnitPrice
		if stats.MinPrice == nil || p < *stats.MinPrice {
			stats.MinPrice, stats.MinDate = fptr(p), r.DatePrice
		}
		if stats.MaxPrice == nil || p > *stats.MaxPrice {
			stats.MaxPrice, stats.MaxDate = fptr(p), r.DatePrice
		}
		if r.SupplierID > 0 {
			suppliers[r.SupplierID] = true
		}
		stats.TotalQty += r.Qty
		stats.Series = append(stats.Series, SeriesPoint{Date: r.DatePrice, Price: p})
	}
	stats.SuppliersCount = len(suppliers)

	// The window ends at the LAST PURCHASE by default, not today: a product
	// last bought three years ago still shows the average of its last twelve
	// months of activity.
	window := sorted
	if opts.AvgMonths > 0 {
		reference := opts.ReferenceDate
		if reference == "" {
			reference = last.DatePrice
		}
		if cutoff, ok := subtractMonths(reference, opts.AvgMonths); ok {
			window = nil
			for _, r := range sorted {
				if r.DatePrice >= cutoff && r.DatePrice <= reference {
					window = append(window, r)
				}
			}
		}
	}

	stats.AvgSampleCount = len(window)
	if len(window) > 0 {
		var sumSimple, sumValue, sumQty float64
		prices := make([]float64, 0, len(window))
		for _, r := range window {
			sumSimple += r.NetUnitPrice
			if r.Qty > 0 {
				sumValue += r.NetUnitPrice * r.Qty
				sumQty += r.Qty
			}
			prices = append(prices, r.NetUnitPrice)
		}
		avgSimple := sumSimple / float64(len(window))
		stats.AvgSimple = fptr(avgSimple)
		if sumQty > 0 {
			stats.AvgWeighted = fptr(sumValue / sumQty)
		} else {
			stats.AvgWeighted = fptr(avgSimple)
		}
		// Over the SAME window as the average, on purpose.
		stats.Percentile = fptr(percentileOf(prices, opts.Percentile))
	}
	stats.PercentileP = opts.Percentile

	return stats
}

// percentileOf is the p-th percentile by linear interpolation — the same
// method as SQL PERCENTILE_CONT and the PHP class. It does not modify values.
func percentileOf(values []float64, p int) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	// PHP: (count-1)*p is an int product, then a float division by 100.
	pos := float64((len(sorted)-1)*p) / 100
	low := int(pos)
	rest := pos - float64(low)
	if rest == 0 {
		return sorted[low]
	}
	return sorted[low] + (sorted[low+1]-sorted[low])*rest
}

// subtractMonths subtracts calendar months from a 'Y-m-d[ H:i:s]' date without
// reading the clock. Like the PHP, it pivots on the month and clamps the day
// to the last day of the target month (31 March - 1 = 28/29 February, not
// 3 March). It reports false when the input does not parse or months <= 0,
// which the caller treats as "no window".
func subtractMonths(date string, months int) (string, bool) {
	if date == "" || months <= 0 {
		return "", false
	}
	t, err := time.Parse(sqlDateTime, date)
	if err != nil {
		t, err = time.Parse("2006-01-02", strings.TrimSpace(date))
		if err != nil {
			return "", false
		}
	}

	total := t.Year()*12 + int(t.Month()) - 1 - months
	year := total / 12
	if total < 0 && total%12 != 0 {
		year-- // floor, like PHP's floor($total / 12)
	}
	month := total - year*12 + 1

	day := t.Day()
	if last := daysIn(year, time.Month(month)); day > last {
		day = last
	}
	out := time.Date(year, time.Month(month), day, t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
	return out.Format(sqlDateTime), true
}

func daysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func fptr(f float64) *float64 { return &f }
