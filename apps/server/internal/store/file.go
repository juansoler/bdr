package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// File es un almacenamiento duradero en un único archivo JSON (state.json)
// más un registro de auditoría append-only (audit.jsonl) en el mismo directorio.
type File struct {
	*Memory
	path string
}

// legacyState es el formato 0.2/0.3 (listas sin metadatos).
type legacyState struct {
	Tokens      []EnrollmentToken `json:"tokens"`
	Agents      []Agent           `json:"agents"`
	Plans       []Plan            `json:"plans"`
	Assignments []Assignment      `json:"assignments"`
	Jobs        []Job             `json:"jobs"`
}

// NewFile abre o crea un almacén en path.
func NewFile(path string) (*File, error) {
	st, err := loadState(path)
	if err != nil {
		return nil, err
	}
	audit, err := OpenAuditFile(filepath.Join(filepath.Dir(path), "audit.jsonl"))
	if err != nil {
		return nil, err
	}
	f := &File{path: path}
	f.Memory = newMemory(st, f.write, audit)
	return f, nil
}

func loadState(path string) (*State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewState(time.Now().UTC()), nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return NewState(time.Now().UTC()), nil
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(b, &probe); err != nil {
		return nil, fmt.Errorf("estado corrupto en %s: %w", path, err)
	}
	if _, ok := probe["meta"]; !ok {
		return migrateLegacy(b, path)
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("estado corrupto en %s: %w", path, err)
	}
	if st.Meta.SchemaVersion > SchemaVersion {
		return nil, fmt.Errorf("state.json usa el esquema %d, más nuevo que el soportado (%d)", st.Meta.SchemaVersion, SchemaVersion)
	}
	st.normalize()
	return &st, nil
}

func migrateLegacy(b []byte, path string) (*State, error) {
	var old legacyState
	if err := json.Unmarshal(b, &old); err != nil {
		return nil, fmt.Errorf("estado 0.x corrupto en %s: %w", path, err)
	}
	// Copia de seguridad del formato antiguo antes de reescribirlo.
	if err := os.WriteFile(path+".v1.bak", b, 0o600); err != nil {
		return nil, fmt.Errorf("no se pudo guardar la copia del estado antiguo: %w", err)
	}
	st := NewState(time.Now().UTC())
	for i := range old.Tokens {
		t := old.Tokens[i]
		st.Tokens[t.ID] = &t
	}
	for i := range old.Agents {
		a := old.Agents[i]
		st.Agents[a.ID] = &a
	}
	for i := range old.Plans {
		p := old.Plans[i]
		st.Plans[p.ID] = &p
	}
	for i := range old.Assignments {
		a := old.Assignments[i]
		st.Assignments[a.ID] = &a
	}
	for i := range old.Jobs {
		j := old.Jobs[i]
		st.Jobs[j.ID] = &j
	}
	st.normalize()
	return st, nil
}

// write serializa el estado con temporal + fsync + rename.
func (f *File) write(st *State) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// En Windows, Rename sobre un destino existente puede devolver acceso
	// denegado. El fallback no es atómico, pero el contenido ya está en un
	// temporal sincronizado.
	if err := os.Rename(name, f.path); err != nil {
		if removeErr := os.Remove(f.path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return err
		}
		return os.Rename(name, f.path)
	}
	return nil
}

// Path devuelve la ruta de state.json.
func (f *File) Path() string { return f.path }
