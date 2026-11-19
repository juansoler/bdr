// Package cron interpreta expresiones cron de 5 campos y ventanas horarias.
//
// Sintaxis: "minuto hora día-del-mes mes día-de-la-semana", con "*", listas
// "1,15", rangos "1-5", pasos "*/15" y "10-50/10". Día de la semana 0–7
// (0 y 7 = domingo). Alias: @hourly, @daily, @midnight, @weekly, @monthly.
// Igual que en cron clásico, si día-del-mes y día-de-la-semana están
// restringidos a la vez, basta con que coincida uno de los dos.
package cron

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule es una expresión cron compilada.
type Schedule struct {
	expr                   string
	minute, hour, dom, mon uint64
	dow                    uint64
	domStar, dowStar       bool
}

type field struct {
	name     string
	min, max int
}

var fields = []field{
	{"minuto", 0, 59},
	{"hora", 0, 23},
	{"día del mes", 1, 31},
	{"mes", 1, 12},
	{"día de la semana", 0, 7},
}

var aliases = map[string]string{
	"@hourly":   "0 * * * *",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@weekly":   "0 0 * * 0",
	"@monthly":  "0 0 1 * *",
}

// Parse compila una expresión.
func Parse(expr string) (*Schedule, error) {
	expr = strings.TrimSpace(expr)
	if a, ok := aliases[strings.ToLower(expr)]; ok {
		expr = a
	}
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return nil, fmt.Errorf("la expresión cron debe tener 5 campos, tiene %d", len(parts))
	}
	s := &Schedule{expr: expr}
	sets := make([]uint64, 5)
	for i, p := range parts {
		set, err := parseField(p, fields[i])
		if err != nil {
			return nil, err
		}
		sets[i] = set
	}
	s.minute, s.hour, s.dom, s.mon, s.dow = sets[0], sets[1], sets[2], sets[3], sets[4]
	if s.dow&(1<<7) != 0 {
		s.dow |= 1
		s.dow &^= 1 << 7
	}
	s.domStar = parts[2] == "*" || strings.HasPrefix(parts[2], "*/")
	s.dowStar = parts[4] == "*" || strings.HasPrefix(parts[4], "*/")
	return s, nil
}

func parseField(p string, f field) (uint64, error) {
	var set uint64
	for _, item := range strings.Split(p, ",") {
		if item == "" {
			return 0, fmt.Errorf("campo %s vacío", f.name)
		}
		step := 1
		if i := strings.Index(item, "/"); i >= 0 {
			n, err := strconv.Atoi(item[i+1:])
			if err != nil || n < 1 {
				return 0, fmt.Errorf("paso inválido en %s: %q", f.name, item)
			}
			step = n
			item = item[:i]
		}
		lo, hi := f.min, f.max
		switch {
		case item == "*":
		case strings.Contains(item, "-"):
			a, b, _ := strings.Cut(item, "-")
			var err1, err2 error
			lo, err1 = strconv.Atoi(a)
			hi, err2 = strconv.Atoi(b)
			if err1 != nil || err2 != nil {
				return 0, fmt.Errorf("rango inválido en %s: %q", f.name, item)
			}
		default:
			n, err := strconv.Atoi(item)
			if err != nil {
				return 0, fmt.Errorf("valor inválido en %s: %q", f.name, item)
			}
			lo, hi = n, n
			if step > 1 {
				hi = f.max
			}
		}
		if lo < f.min || hi > f.max || lo > hi {
			return 0, fmt.Errorf("%s fuera de rango (%d–%d): %q", f.name, f.min, f.max, item)
		}
		for v := lo; v <= hi; v += step {
			set |= 1 << uint(v)
		}
	}
	return set, nil
}

// String devuelve la expresión normalizada.
func (s *Schedule) String() string { return s.expr }

// Next devuelve la primera activación estrictamente posterior a t, en la zona de t.
func (s *Schedule) Next(t time.Time) time.Time {
	loc := t.Location()
	t = t.Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(5, 0, 0)
	for t.Before(limit) {
		if s.mon&(1<<uint(t.Month())) == 0 {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, loc)
			continue
		}
		if !s.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, loc)
			continue
		}
		if s.hour&(1<<uint(t.Hour())) == 0 {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, loc)
			continue
		}
		if s.minute&(1<<uint(t.Minute())) == 0 {
			t = t.Add(time.Minute)
			continue
		}
		return t
	}
	return time.Time{}
}

func (s *Schedule) dayMatches(t time.Time) bool {
	domOK := s.dom&(1<<uint(t.Day())) != 0
	dowOK := s.dow&(1<<uint(t.Weekday())) != 0
	if s.domStar || s.dowStar {
		return domOK && dowOK
	}
	return domOK || dowOK
}

// NextN devuelve las n siguientes activaciones posteriores a t.
func (s *Schedule) NextN(t time.Time, n int) []time.Time {
	out := make([]time.Time, 0, n)
	for i := 0; i < n; i++ {
		t = s.Next(t)
		if t.IsZero() {
			break
		}
		out = append(out, t)
	}
	return out
}

// ErrWindow indica una ventana mal formada.
var ErrWindow = errors.New("la ventana debe tener formato HH:MM–HH:MM")

// ParseClock interpreta "HH:MM" como minutos desde medianoche.
func ParseClock(s string) (int, error) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok {
		return 0, ErrWindow
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, ErrWindow
	}
	return hh*60 + mm, nil
}

// InWindow indica si t (ya en la zona del plan) cae dentro de [start, end).
// Si end <= start la ventana cruza la medianoche.
func InWindow(t time.Time, start, end string) (bool, error) {
	a, err := ParseClock(start)
	if err != nil {
		return false, err
	}
	b, err := ParseClock(end)
	if err != nil {
		return false, err
	}
	m := t.Hour()*60 + t.Minute()
	if a == b {
		return true, nil
	}
	if a < b {
		return m >= a && m < b, nil
	}
	return m >= a || m < b, nil
}
