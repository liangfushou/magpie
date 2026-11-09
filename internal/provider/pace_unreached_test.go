package provider

import (
	"math"
	"slices"
	"testing"
	"time"
)

// Use before expiry spends the week once later weeks cannot carry the
// month left (#5641, the Volcengine Agent Plan reading from #5639).
func TestPaceSkipsUnreachedWindows(t *testing.T) {
	now := time.Now()
	day, week := 24*time.Hour, 7*24*time.Hour
	weekAt := now.Add(23*time.Hour + 36*time.Minute)
	monthAt := weekAt.Add(2 * week)
	a := Allowance{
		{Used: 0, Span: 5 * time.Hour, Of: 2000},
		{Used: 1628.0 / 70, Of: 7000, Span: week, Resets: weekAt},
		{Used: 1628.0 / 200, Of: 20000, Span: 30 * day, Resets: monthAt},
	}
	weekPace := (100 - a[1].Used) / weekAt.Sub(now).Hours()
	monthPace := func(at time.Time) float64 { return (100 - a[2].Used) / at.Sub(now).Hours() }
	for _, c := range []struct {
		name string
		edit func(Allowance)
		pace float64
		due  time.Time
	}{
		{"two weeks left", func(Allowance) {}, weekPace, weekAt},
		{"as reported, four weeks left", func(a Allowance) { a[2].Resets = now.Add(696 * time.Hour) }, (100 - a[2].Used) / 696, now.Add(696 * time.Hour)},
		{"month exactly reachable", func(a Allowance) { a[2].Used = 30 }, 70 / monthAt.Sub(now).Hours(), monthAt},
		{"month just out of reach", func(a Allowance) { a[2].Used = 29.99 }, weekPace, weekAt},
		{"counts unknown", func(a Allowance) { a[1].Of = 0 }, monthPace(monthAt), monthAt},
		{"different units", func(a Allowance) { a[1].Unit = "USD" }, monthPace(monthAt), monthAt},
		{"same spans", func(a Allowance) { a[1].Span = a[2].Span }, monthPace(monthAt), monthAt},
		{"short cap only", func(a Allowance) { a[1].Span = 5 * time.Hour }, monthPace(monthAt), monthAt},
		{"another model's week", func(a Allowance) { a[1].Model = "opus" }, monthPace(monthAt), monthAt},
		{"month expires first", func(a Allowance) {
			a[2].RunsOut, a[2].RunsOutLeft = now.Add(10*time.Hour), 2000
		}, 1, now.Add(10 * time.Hour)},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, order := range [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
				b := slices.Clone(a)
				c.edit(b)
				b = Allowance{b[order[0]], b[order[1]], b[order[2]]}
				if got, due := b.Pace("doubao-seed-2.0-pro", now); math.Abs(got-c.pace) > 1e-9 || !due.Equal(c.due) {
					t.Errorf("order %v: pace %v due %v, want %v due %v", order, got, due, c.pace, c.due)
				}
			}
		})
	}

	// An auto-used reset starts both budgets again together: neither
	// window can be passed over, and the tighter week still decides.
	b := a.restartedBy(now.Add(2*time.Hour + resetLead()))
	if got, due := b.Pace("m", now); math.Abs(got-(100-a[1].Used)/2) > 1e-9 || !due.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("automatic reset: pace %v due %v", got, due)
	}
}

// Selecting the windows must not replace their values with Renewal's
// inferred resets or turn a partly expiring balance into a whole one.
func TestPaceKeptWindowsKeepTheirReading(t *testing.T) {
	now := time.Now()
	day := 24 * time.Hour
	month := Limit{Used: 20, Of: 20000, Span: 30 * day}
	year := Limit{Used: 0, Of: 1000000, Span: 365 * day, Resets: now.Add(60 * day)}
	for _, c := range []struct {
		name string
		l    Limit
		pace float64
		due  time.Time
	}{
		{"reset unknown", month, 80.0 / (30 * 24), time.Time{}},
		{"reset passed", Limit{Used: 20, Of: 20000, Span: 30 * day, Resets: now.Add(-time.Hour)}, 100.0 / (30 * 24), time.Time{}},
		{"only some credits expire", Limit{Used: 20, Of: 20000, Span: 30 * day, Resets: now.Add(30 * day), RunsOut: now.Add(10 * time.Hour), RunsOutLeft: 2000}, 1, now.Add(10 * time.Hour)},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got, due := (Allowance{year, c.l}).Pace("m", now); math.Abs(got-c.pace) > 1e-9 || !due.Equal(c.due) {
				t.Fatalf("pace %v due %v, want %v due %v", got, due, c.pace, c.due)
			}
		})
	}
	// Several unreachable windows can precede the one being paced.
	weekAt := now.Add(24 * time.Hour)
	month.Resets = now.Add(10 * day)
	week := Limit{Used: 30, Of: 7000, Span: 7 * day, Resets: weekAt}
	if got, due := (Allowance{week, year, month}).Pace("m", now); math.Abs(got-70.0/24) > 1e-9 || !due.Equal(weekAt) {
		t.Fatalf("year and month out of reach: pace %v due %v", got, due)
	}
}
