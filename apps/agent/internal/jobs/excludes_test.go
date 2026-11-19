package jobs

import (
	"testing"

	"github.com/juansoler/bdr/packages/contracts"
)

func TestIgnorePatternPosix(t *testing.T) {
	cases := []struct {
		exclude, root, want string
		ok                  bool
	}{
		{"/data/tmp", "/data", "/tmp", true},
		{"/data/sub/dir", "/data/", "/sub/dir", true},
		{"/data", "/data", "/", true},
		{"*.tmp", "/data", "*.tmp", true},
		{"cache/", "/data", "cache/", true},
		{"/etc/passwd", "/data", "", false},
		{"/var/tmp", "/data/sub", "", false},
		{"/datax", "/data", "", false},
	}
	for _, c := range cases {
		got, ok := ignorePattern(c.exclude, c.root, false)
		if ok != c.ok || got != c.want {
			t.Errorf("ignorePattern(%q, %q) = (%q,%v), esperaba (%q,%v)",
				c.exclude, c.root, got, ok, c.want, c.ok)
		}
	}
}

func TestIgnorePatternWindows(t *testing.T) {
	cases := []struct {
		exclude, root, want string
		ok                  bool
	}{
		{`C:\Data\Cache`, `C:\Data`, "/Cache", true},
		{`c:/data/cache`, `C:\Data`, "/cache", true},
		{`C:\Data\sub\..\tmp`, `C:\Data`, "/tmp", true},
		{`C:\Data`, `c:\data`, "/", true},
		{`D:\Otro`, `C:\Data`, "", false},
		{`*.tmp`, `C:\Data`, "*.tmp", true},
		{`cache\*`, `C:\Data`, "cache/*", true},
		{`\\server\share\data\x`, `\\server\share\data`, "/x", true},
	}
	for _, c := range cases {
		got, ok := ignorePattern(c.exclude, c.root, true)
		if ok != c.ok || got != c.want {
			t.Errorf("ignorePattern(%q, %q, windows) = (%q,%v), esperaba (%q,%v)",
				c.exclude, c.root, got, ok, c.want, c.ok)
		}
	}
}

func TestRetentionArgs(t *testing.T) {
	args := retentionArgs(contracts.Retention{
		KeepLatest: 5, KeepHourly: 0, KeepDaily: 7,
		KeepWeekly: 4, KeepMonthly: 6, KeepAnnual: 1,
	})
	want := map[string]bool{
		"--keep-latest=5": true, "--keep-hourly=0": true, "--keep-daily=7": true,
		"--keep-weekly=4": true, "--keep-monthly=6": true, "--keep-annual=1": true,
	}
	for _, a := range args {
		if !want[a] {
			t.Errorf("argumento inesperado %q", a)
		}
		delete(want, a)
	}
	if len(want) != 0 {
		t.Errorf("faltan argumentos: %v", want)
	}
}
