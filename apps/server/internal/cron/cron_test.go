package cron

import (
	"testing"
	"time"
)

func TestParseErrors(t *testing.T) {
	for _, expr := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "*/0 * * * *", "a * * * *", "5-1 * * * *"} {
		if _, err := Parse(expr); err == nil {
			t.Errorf("esperaba error para %q", expr)
		}
	}
}

func TestNext(t *testing.T) {
	base := time.Date(2026, 11, 16, 10, 7, 30, 0, time.UTC) // lunes
	cases := []struct {
		expr string
		want time.Time
	}{
		{"*/15 * * * *", time.Date(2026, 11, 16, 10, 15, 0, 0, time.UTC)},
		{"0 2 * * *", time.Date(2026, 11, 17, 2, 0, 0, 0, time.UTC)},
		{"@hourly", time.Date(2026, 11, 16, 11, 0, 0, 0, time.UTC)},
		{"30 1 * * 0", time.Date(2026, 11, 22, 1, 30, 0, 0, time.UTC)},
		{"30 1 * * 7", time.Date(2026, 11, 22, 1, 30, 0, 0, time.UTC)},
		{"0 0 1 * *", time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)},
		{"0 9 * * 1-5", time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)},
		{"0 0 29 2 *", time.Date(2028, 2, 29, 0, 0, 0, 0, time.UTC)},
		// día del mes O día de la semana cuando ambos están restringidos
		{"0 0 20 * 3", time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		s, err := Parse(c.expr)
		if err != nil {
			t.Fatalf("%s: %v", c.expr, err)
		}
		if got := s.Next(base); !got.Equal(c.want) {
			t.Errorf("%s: got %v want %v", c.expr, got, c.want)
		}
	}
}

func TestNextTimezone(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Skip("sin tzdata")
	}
	s, _ := Parse("0 2 * * *")
	got := s.Next(time.Date(2026, 11, 16, 12, 0, 0, 0, loc))
	if got.Hour() != 2 || got.Location() != loc {
		t.Fatalf("got %v", got)
	}
}

func TestWindow(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 1, 1, h, m, 0, 0, time.UTC) }
	cases := []struct {
		start, end string
		t          time.Time
		want       bool
	}{
		{"01:00", "05:00", at(3, 0), true},
		{"01:00", "05:00", at(5, 0), false},
		{"22:00", "06:00", at(23, 30), true},
		{"22:00", "06:00", at(2, 0), true},
		{"22:00", "06:00", at(12, 0), false},
	}
	for _, c := range cases {
		got, err := InWindow(c.t, c.start, c.end)
		if err != nil || got != c.want {
			t.Errorf("%s-%s @%v: got %v err %v", c.start, c.end, c.t, got, err)
		}
	}
	if _, err := InWindow(at(0, 0), "25:00", "01:00"); err == nil {
		t.Error("esperaba error")
	}
}
