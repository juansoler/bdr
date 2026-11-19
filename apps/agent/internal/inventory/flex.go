// Package inventory recoge el inventario del equipo.
package inventory

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// flexString acepta cadenas, números, booleanos o null en la salida de
// herramientas externas (lsblk, PowerShell) y lo normaliza a string.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		*f = ""
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flexString(s)
		return nil
	}
	*f = flexString(strings.Trim(string(b), `"`))
	return nil
}

func (f flexString) String() string { return string(f) }

// flexInt64 acepta números o cadenas numéricas.
type flexInt64 int64

func (f *flexInt64) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		// Algunas herramientas devuelven decimales.
		fl, ferr := strconv.ParseFloat(s, 64)
		if ferr != nil {
			return err
		}
		n = int64(fl)
	}
	*f = flexInt64(n)
	return nil
}

func (f flexInt64) Int64() int64 { return int64(f) }

// flexBool acepta true/false, "true"/"false", "0"/"1", 0/1 o null.
type flexBool bool

func (f *flexBool) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	switch strings.ToLower(s) {
	case "", "null", "false", "0":
		*f = false
	default:
		*f = true
	}
	return nil
}

func (f flexBool) Bool() bool { return bool(f) }
