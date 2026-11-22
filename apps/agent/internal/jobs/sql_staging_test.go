package jobs

import (
	"context"
	"path/filepath"
	"testing"
)

func TestResolveSQLStaging(t *testing.T) {
	t.Run("env tiene prioridad", func(t *testing.T) {
		t.Setenv("BDR_SQL_STAGING", `C:\staging-custom`)
		if got := resolveSQLStaging(context.Background(), nil); got != `C:\staging-custom` {
			t.Fatalf("got %q; want C:\\staging-custom", got)
		}
	})

	t.Run("sin env cae al default de plataforma", func(t *testing.T) {
		t.Setenv("BDR_SQL_STAGING", "")
		got := resolveSQLStaging(context.Background(), nil)
		if filepath.Base(got) != "bdr-sql-staging" {
			t.Fatalf("got %q; esperaba ...bdr-sql-staging", got)
		}
	})
}
