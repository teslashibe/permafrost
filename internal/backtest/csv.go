package backtest

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
)

// LoadCSV reads funding ticks from a CSV file with header columns:
//
//	time,symbol,rate,interval_seconds
//
// Time may be RFC3339 or epoch milliseconds. Rate is fractional
// (0.0001 = 1bp/interval). Returns the parsed ticks in original order.
func LoadCSV(path string) ([]FundingTick, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("backtest: open %s: %w", path, err)
	}
	defer f.Close()
	return ReadCSV(f)
}

// ReadCSV is the io.Reader-backed equivalent of LoadCSV.
func ReadCSV(r io.Reader) ([]FundingTick, error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("backtest: csv: %w", err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("backtest: csv has no data rows")
	}
	header := rows[0]
	idx := func(name string) int {
		for i, h := range header {
			if h == name {
				return i
			}
		}
		return -1
	}
	tIdx, sIdx, rIdx, iIdx := idx("time"), idx("symbol"), idx("rate"), idx("interval_seconds")
	if tIdx < 0 || sIdx < 0 || rIdx < 0 || iIdx < 0 {
		return nil, fmt.Errorf("backtest: csv missing required columns time,symbol,rate,interval_seconds")
	}

	out := make([]FundingTick, 0, len(rows)-1)
	for ln, row := range rows[1:] {
		if len(row) <= iIdx {
			return nil, fmt.Errorf("backtest: csv row %d truncated", ln+2)
		}
		t, err := parseTime(row[tIdx])
		if err != nil {
			return nil, fmt.Errorf("backtest: csv row %d time: %w", ln+2, err)
		}
		rate, err := decimal.NewFromString(row[rIdx])
		if err != nil {
			return nil, fmt.Errorf("backtest: csv row %d rate: %w", ln+2, err)
		}
		intervalSecs, err := strconv.ParseInt(row[iIdx], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("backtest: csv row %d interval: %w", ln+2, err)
		}
		out = append(out, FundingTick{
			Time:     t,
			Symbol:   row[sIdx],
			Rate:     rate,
			Interval: time.Duration(intervalSecs) * time.Second,
		})
	}
	return out, nil
}

func parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.UnixMilli(ms).UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unrecognised time format %q", s)
}
