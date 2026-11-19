package jobs

import (
	"testing"

	pkgimg "github.com/juansoler/bdr/packages/image"
)

// filterImageDisks: vacío = todos; índices concretos filtran; índices que
// no coinciden con ningún disco fallan con E_VALIDATION.
func TestFilterImageDisks(t *testing.T) {
	disks := []pkgimg.Disk{
		{Index: 0, DevicePath: "/dev/sda", TotalBytes: 100},
		{Index: 1, DevicePath: "/dev/sdb", TotalBytes: 200},
		{Index: 2, DevicePath: "/dev/sdc", TotalBytes: 300},
	}

	// Sin filtro: todos.
	got, err := filterImageDisks(disks, nil)
	if err != nil || len(got) != 3 {
		t.Fatalf("sin filtro: %v err=%v", got, err)
	}
	got, err = filterImageDisks(disks, []int{})
	if err != nil || len(got) != 3 {
		t.Fatalf("filtro vacío: %v err=%v", got, err)
	}

	// Filtrar por índice mantiene el orden de enumeración.
	got, err = filterImageDisks(disks, []int{2, 0})
	if err != nil {
		t.Fatalf("filtro 0,2: %v", err)
	}
	if len(got) != 2 || got[0].Index != 0 || got[1].Index != 2 {
		t.Fatalf("filtro 0,2: %v", got)
	}
	for _, d := range got {
		if d.TotalBytes != 100 && d.TotalBytes != 300 {
			t.Fatalf("disco del filtro con datos equivocados: %+v", d)
		}
	}

	// Duplicados en el filtro: un disco aparece una sola vez.
	got, err = filterImageDisks(disks, []int{1, 1})
	if err != nil || len(got) != 1 || got[0].Index != 1 {
		t.Fatalf("filtro duplicado: %v err=%v", got, err)
	}

	// Ningún índice coincide → E_VALIDATION.
	_, err = filterImageDisks(disks, []int{7})
	if err == nil || ErrorCode(err) != "E_VALIDATION" {
		t.Fatalf("índice inexistente debe dar E_VALIDATION: %v", err)
	}
}
