// Constructor de cadenas de restauración desde el catálogo (RF-42):
// dado un nombre de base y un punto en el tiempo (opcional), selecciona
// del catálogo de puntos SQL la cadena mínima FULL → DIFF → LOGs con
// continuidad de LSN y devuelve la lista de ficheros en orden de
// aplicación. El algoritmo es una función pura sobre los puntos del store
// para poder testearla sin SQL Server.
package control

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/juansoler/bdr/apps/server/internal/store"
	"github.com/juansoler/bdr/internal/connector"
	"github.com/juansoler/bdr/packages/contracts"
)

// sqlBackupPoints agrupa los candidatos por tipo para la selección.
type sqlBackupPoints struct {
	fulls, diffs, logs []*store.RestorePoint
}

// buildSQLRestoreChain construye la cadena de restauración de `database`
// hasta stopAt (nil = el punto más reciente del catálogo).
//
// Reglas (msdb):
//   - La base es el último FULL no COPY_ONLY con fin <= corte.
//   - Un DIFF es válido para un FULL si su database_backup_lsn coincide
//     con el del FULL (el DIFF hereda el checkpoint del FULL base) y es
//     posterior a él.
//   - Los LOGs se aplican por orden de first_lsn cubriendo el hueco desde
//     el último elemento aplicado (los DIFF no truncan el log: el primer
//     LOG tras un DIFF puede empezar antes del last_lsn del DIFF; lo que
//     SQL exige es que el LOG cubra y extienda el punto de redo).
//
// Devuelve la cadena en orden de aplicación (full, diff?, logs…).
func buildSQLRestoreChain(points []*store.RestorePoint, database string, stopAt *time.Time) ([]*store.RestorePoint, error) {
	candidates := classifySQLBackups(points, database)
	if len(candidates.fulls) == 0 {
		return nil, fmt.Errorf("no hay backups FULL de %s en el catálogo", database)
	}

	cutoff := farFuture()
	if stopAt != nil {
		cutoff = *stopAt
	}

	full := latestSQLBackupBefore(candidates.fulls, cutoff, time.Time{})
	if full == nil {
		return nil, fmt.Errorf("no hay backups FULL de %s anteriores al punto solicitado", database)
	}
	chain := []*store.RestorePoint{full}
	lastLSN := full.SQL.LastLSN

	// DIFF: el más reciente con la misma base (database_backup_lsn del
	// FULL) y posterior al FULL. Con corte: su fin <= cutoff.
	diff := latestSQLBackupBefore(candidates.diffs, cutoff, full.EndTime)
	for diff != nil {
		if diff.SQL.DatabaseLSN == full.SQL.DatabaseLSN {
			chain = append(chain, diff)
			lastLSN = diff.SQL.LastLSN
			break
		}
		// Busca el siguiente DIFF más antiguo con la base correcta.
		prev := previousSQLBackupBefore(candidates.diffs, cutoff, diff.EndTime, full.EndTime)
		if prev == nil {
			break
		}
		diff = prev
	}

	// LOGs: ordenados por first_lsn, cubriendo y extendiendo lastLSN.
	logs := make([]*store.RestorePoint, 0, len(candidates.logs))
	for _, lp := range candidates.logs {
		if !lp.EndTime.After(full.EndTime) || lp.EndTime.After(cutoff) {
			continue
		}
		logs = append(logs, lp)
	}
	sort.Slice(logs, func(i, j int) bool {
		if c := connector.LSN(logs[i].SQL.FirstLSN).Compare(connector.LSN(logs[j].SQL.FirstLSN)); c != 0 {
			return c < 0
		}
		return logs[i].EndTime.Before(logs[j].EndTime)
	})
	for _, lp := range logs {
		if connector.LSN(lp.SQL.FirstLSN).Compare(connector.LSN(lastLSN)) > 0 {
			return nil, fmt.Errorf("discontinuidad en la cadena de logs de %s: hay un hueco entre %s y %s",
				database, shortLSN(lastLSN), shortLSN(lp.SQL.FirstLSN))
		}
		if connector.LSN(lp.SQL.LastLSN).Compare(connector.LSN(lastLSN)) > 0 {
			chain = append(chain, lp)
			lastLSN = lp.SQL.LastLSN
		}
	}
	return chain, nil
}

// classifySQLBackups separa los puntos de la base pedida por tipo,
// excluyendo los COPY_ONLY como base (no reinician la cadena; los LOG
// copy-only sí son aplicables).
func classifySQLBackups(points []*store.RestorePoint, database string) sqlBackupPoints {
	out := sqlBackupPoints{}
	for _, rp := range points {
		if rp == nil || rp.SQL == nil || rp.SnapshotID == "" {
			continue
		}
		if !strings.EqualFold(rp.SQL.Database, database) {
			continue
		}
		switch strings.ToUpper(rp.SQL.BackupType) {
		case "FULL":
			out.fulls = append(out.fulls, rp)
		case "DIFFERENTIAL":
			out.diffs = append(out.diffs, rp)
		case "LOG":
			out.logs = append(out.logs, rp)
		}
	}
	// Más recientes primero en cada grupo (EndTime = backup_finish de msdb).
	sortDescByEnd := func(list []*store.RestorePoint) {
		sort.Slice(list, func(i, j int) bool { return list[i].EndTime.After(list[j].EndTime) })
	}
	sortDescByEnd(out.fulls)
	sortDescByEnd(out.diffs)
	return out
}

// latestSQLBackupBefore devuelve el más reciente con fin <= cutoff y, si
// after es distinto de cero, con fin > after.
func latestSQLBackupBefore(list []*store.RestorePoint, cutoff, after time.Time) *store.RestorePoint {
	for _, rp := range list { // la lista va más reciente primero
		if rp.EndTime.After(cutoff) {
			continue
		}
		if !after.IsZero() && !rp.EndTime.After(after) {
			continue
		}
		return rp
	}
	return nil
}

// previousSQLBackupBefore devuelve el siguiente más antiguo que `pivot`
// (y posterior a `floor` si no es cero).
func previousSQLBackupBefore(list []*store.RestorePoint, cutoff, pivot, floor time.Time) *store.RestorePoint {
	for _, rp := range list {
		if rp.EndTime.After(cutoff) || !rp.EndTime.Before(pivot) {
			continue
		}
		if !floor.IsZero() && !rp.EndTime.After(floor) {
			continue
		}
		return rp
	}
	return nil
}

func farFuture() time.Time { return time.Now().UTC().AddDate(100, 0, 0) }

func shortLSN(l string) string {
	if len(l) > 12 {
		return l[:8] + "…"
	}
	return l
}

// sqlRestoreChainView convierte la cadena a la vista de la API.
func sqlRestoreChainView(chain []*store.RestorePoint, database string, stopAt *time.Time) contracts.SQLRestoreChainView {
	v := contracts.SQLRestoreChainView{
		Database: database,
		Valid:    true,
	}
	if stopAt != nil {
		v.StopAt = stopAt
	}
	for _, rp := range chain {
		v.Files = append(v.Files, rp.SnapshotID)
		v.Steps = append(v.Steps, contracts.SQLRestoreChainStep{
			RestorePointID: rp.ID,
			JobID:          rp.JobID,
			BackupType:     rp.SQL.BackupType,
			BackupFinish:   rp.EndTime,
			FirstLSN:       rp.SQL.FirstLSN,
			LastLSN:        rp.SQL.LastLSN,
			RepoURI:        rp.SnapshotID,
			SizeBytes:      rp.SizeBytes,
		})
	}
	return v
}

// handleSQLRestoreChain calcula la cadena de restauración para una base
// (y punto en el tiempo opcional) desde el catálogo de puntos SQL.
// POST /api/v1/admin/sql/restore-chain
func (s *Server) handleSQLRestoreChain(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	r, _, ok := s.authorize(w, r, "read")
	if !ok {
		return
	}
	var req struct {
		Database string     `json:"database"`
		AgentID  string     `json:"agent_id,omitempty"` // filtro opcional
		StopAt   *time.Time `json:"stop_at,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "cuerpo inválido")
		return
	}
	if req.Database == "" {
		s.problem(w, r, http.StatusBadRequest, "E_VALIDATION", "database es obligatorio")
		return
	}

	var view contracts.SQLRestoreChainView
	var chainErr error
	err := s.Store.View(r.Context(), func(st *store.State) error {
		var points []*store.RestorePoint
		for _, rp := range st.RestorePoints {
			if rp.SQL == nil || rp.SnapshotID == "" {
				continue
			}
			if req.AgentID != "" && rp.AgentID != req.AgentID {
				continue
			}
			if strings.EqualFold(rp.SQL.Database, req.Database) {
				points = append(points, rp)
			}
		}
		chain, berr := buildSQLRestoreChain(points, req.Database, req.StopAt)
		if berr != nil {
			chainErr = berr
			return nil
		}
		view = sqlRestoreChainView(chain, req.Database, req.StopAt)
		return nil
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if chainErr != nil {
		view = contracts.SQLRestoreChainView{Database: req.Database, Valid: false, Error: chainErr.Error()}
		if req.StopAt != nil {
			view.StopAt = req.StopAt
		}
		writeJSON(w, http.StatusOK, view) // cadena inválida: 200 con valid=false
		return
	}
	writeJSON(w, http.StatusOK, view)
}
