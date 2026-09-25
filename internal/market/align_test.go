package market

import (
	"testing"
	"time"
)

func TestAlignDropsWeekendsAndJoinsOnDate(t *testing.T) {
	d := func(y int, m time.Month, day int, px float64) Bar {
		return Bar{Date: time.Date(y, m, day, 16, 0, 0, 0, time.UTC), Close: px, Open: px - 1}
	}
	eq := []Bar{
		d(2024, 1, 5, 100), // Friday
		d(2024, 1, 6, 50),  // Saturday, must drop
		d(2024, 1, 8, 110), // Monday
	}
	btc := []Bar{
		d(2024, 1, 5, 10),
		d(2024, 1, 6, 11),
		d(2024, 1, 8, 12),
	}
	gold := []Bar{
		d(2024, 1, 5, 20),
		d(2024, 1, 8, 21),
	}
	days := align(eq, btc, gold, nil)
	if len(days) != 2 {
		t.Fatalf("days %d", len(days))
	}
	if days[0].Equity != 100 || days[0].BTC != 10 || days[0].Gold != 20 {
		t.Fatalf("friday %+v", days[0])
	}
	if days[1].Equity != 110 || days[1].BTC != 12 || days[1].Gold != 21 {
		t.Fatalf("monday %+v", days[1])
	}
}
