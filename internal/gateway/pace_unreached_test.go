package gateway

import (
	"math"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/provider"
)

// Use before expiry spends an Agent Plan's week before another account's
// once the weeks left cannot carry its month, and tells the same pace and
// deadline in the route trace (#5641, the Volcengine reading from #5639).
// A reachable month still limits the pace; neither an almost-full rate
// cap nor a spent window is made usable by passing over the month.
func TestPaceSpendsAWeekTheMonthCantWait(t *testing.T) {
	now := time.Now()
	weekRenews := now.Add(23*time.Hour + 36*time.Minute)
	week := 7 * 24 * time.Hour
	old := allowances
	t.Cleanup(func() { allowances = old })
	allowances = func(string) map[string]provider.Allowance {
		return map[string]provider.Allowance{
			"pace-unreached@example.com": {{Used: 10, Resets: now.Add(3 * 24 * time.Hour), Span: week}},
		}
	}
	of := map[string]provider.Allowance{}
	keyWindows(t, of)
	codex := provider.Provider{ID: "codex", Account: &provider.Account{Agent: "codex", User: "pace-unreached@example.com"}}
	ark := provider.Provider{ID: "volcengine", Key: "pace-unreached-key"}
	heads := []candidate{
		{p: codex, model: "gpt-6-astra", rest: "pace-unreached-codex"},
		{p: ark, model: "doubao-seed-2.0-pro", rest: "pace-unreached-plan"},
	}
	g := provider.Provider{ID: provider.GroupPrefix + "pace-unreached", Routing: provider.Pace}
	for _, c := range []struct {
		name                           string
		shortUsed, weekUsed, monthUsed float64
		month                          time.Time
		wantOrder                      string
		wantPace                       float64
		wantDue                        time.Time
		wantUnreached                  int
	}{
		{
			name: "as reported, the month is reachable", weekUsed: 1628, monthUsed: 1628, month: now.Add(696 * time.Hour),
			wantOrder: "pace-unreached-codex pace-unreached-plan ", wantPace: 91.86 / 696, wantDue: now.Add(696 * time.Hour),
		},
		{
			name: "two weeks cannot carry the month", monthUsed: 1628, month: weekRenews.Add(2*week + 24*time.Minute),
			wantOrder: "pace-unreached-plan pace-unreached-codex ", wantPace: 100 / 23.6, wantDue: weekRenews, wantUnreached: 1,
		},
		{
			name: "two weeks can carry a smaller month", monthUsed: 11000, month: weekRenews.Add(2 * week),
			wantOrder: "pace-unreached-codex pace-unreached-plan ", wantPace: 45 / (23.6 + 336), wantDue: weekRenews.Add(2 * week),
		},
		{
			name: "a rate cap running low stays behind", shortUsed: 95, monthUsed: 1628, month: weekRenews.Add(2*week + 24*time.Minute),
			wantOrder: "pace-unreached-codex pace-unreached-plan ", wantPace: 100 / 23.6, wantDue: weekRenews, wantUnreached: 1,
		},
		{
			name: "a spent rate cap stays behind", shortUsed: 98, monthUsed: 1628, month: weekRenews.Add(2*week + 24*time.Minute),
			wantOrder: "pace-unreached-codex pace-unreached-plan ", wantPace: 100 / 23.6, wantDue: weekRenews, wantUnreached: 1,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			of[ark.Key] = provider.Allowance{
				{Used: c.shortUsed, Amount: c.shortUsed * 20, Of: 2000, Resets: now.Add(time.Hour), Span: 5 * time.Hour, Name: "5 hours"},
				{Used: c.weekUsed / 70, Amount: c.weekUsed, Of: 7000, Resets: weekRenews, Span: week, Name: "Weekly"},
				{Used: c.monthUsed / 200, Amount: c.monthUsed, Of: 20000, Resets: c.month, Span: 30 * 24 * time.Hour, Name: "Month"},
			}
			got, wg := weigh(g, heads, "", provider.Responses)
			if order := restsOf(got); order != c.wantOrder {
				t.Errorf("Use before expiry order = %q, want %q", order, c.wantOrder)
			}
			w := weighed(heads[1], g, wg, false, provider.Responses)
			// weigh reads the clock itself. The windows are hours away,
			// leaving room for a loaded runner without timing the test.
			if math.Abs(w.Pace-c.wantPace) > c.wantPace/100 || w.Due == nil || !w.Due.Equal(c.wantDue) {
				t.Errorf("trace pace %v due %v, want pace %v due %v", w.Pace, w.Due, c.wantPace, c.wantDue)
			}
			if !w.Known || w.Unreached != c.wantUnreached {
				t.Errorf("trace known %v unreached %d, want true and %d", w.Known, w.Unreached, c.wantUnreached)
			}
			if c.shortUsed > 0 && (w.Used != c.shortUsed || w.WeekLow) {
				t.Errorf("rate cap trace: used %v, week-low %v; want %v and false", w.Used, w.WeekLow, c.shortUsed)
			}
		})
	}
}
