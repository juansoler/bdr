package contracts

import "testing"

func TestParseSMBPath(t *testing.T) {
	cases := []struct {
		path             string
		wantOK           bool
		host, share, sub string
	}{
		{"//nas-casa/backups", true, "nas-casa", "backups", ""},
		{"//nas-casa/backups/datos/lxc", true, "nas-casa", "backups", "datos/lxc"},
		{"smb://nas-casa/backups", true, "nas-casa", "backups", ""},
		{"smb://nas-casa:445/backups/sub", true, "nas-casa:445", "backups", "sub"},
		{"//203.0.113.50/backup", true, "203.0.113.50", "backup", ""},
		{"//[fd00::10]:445/backups", true, "[fd00::10]:445", "backups", ""},
		{"  //nas/backups  ", true, "nas", "backups", ""},
		{"/mnt/backups", false, "", "", ""},
		{"C:\\backups", false, "", "", ""},
		{"//nas", false, "", "", ""},
		{"//nas/", false, "", "", ""},
		{"smb://", false, "", "", ""},
		{"", false, "", "", ""},
		{"//nas/IPC$", false, "", "", ""},
	}
	for _, c := range cases {
		got, ok := ParseSMBPath(c.path)
		if ok != c.wantOK {
			t.Fatalf("ParseSMBPath(%q): ok=%v, esperaba %v", c.path, ok, c.wantOK)
		}
		if !c.wantOK {
			continue
		}
		if got.Host != c.host || got.Share != c.share || got.Subdir != c.sub {
			t.Fatalf("ParseSMBPath(%q): %+v, esperaba host=%q share=%q sub=%q",
				c.path, got, c.host, c.share, c.sub)
		}
	}
}

func TestNormalizeSMBPath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"smb://nas/backups", "//nas/backups"},
		{"//nas/backups/", "//nas/backups"},
		{"//nas//backups//datos//", "//nas/backups/datos"},
		{"smb://nas:445/b/d", "//nas:445/b/d"},
		{"/mnt/local", "/mnt/local"}, // no SMB: sin cambios (recortada)
	}
	for _, c := range cases {
		if got := NormalizeSMBPath(c.in); got != c.want {
			t.Fatalf("NormalizeSMBPath(%q) = %q, esperaba %q", c.in, got, c.want)
		}
	}
}
