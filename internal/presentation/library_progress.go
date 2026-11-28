package presentation

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/WilliamO0o/Stark-Manager/internal/library"
)

type LibraryProgress struct {
	out    io.Writer
	filter string
	last   time.Time
	empty  int
	line   bool
}

func NewLibraryProgress(out io.Writer, filter string) *LibraryProgress {
	p := &LibraryProgress{out: out, filter: filter}
	fmt.Fprintln(out, "Descobrindo pastas…")
	return p
}

func IsTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0 && file.Name() != os.DevNull
}

func (p *LibraryProgress) clear() {
	if p.line {
		fmt.Fprint(p.out, "\r\x1b[2K")
		p.line = false
	}
}

func (p *LibraryProgress) Event(event library.Event) {
	switch event.Phase {
	case "cached", "finished":
		p.clear()
		if event.Phase == "cached" {
			writeSnapshot(p.out, *event.Result, true)
		} else if event.Result.Empty && p.filter == "" {
			p.empty++
		} else {
			writeSnapshot(p.out, *event.Result, false)
		}
	case "folder", "file":
		if event.Phase == "file" && time.Since(p.last) < 100*time.Millisecond {
			return
		}
		p.last = time.Now()
		p.clear()
		percent := 0
		if event.Total > 0 {
			percent = event.Done * 100 / event.Total
		}
		fmt.Fprintf(p.out, "%q | arquivos examinados: %d | pastas: %d/%d (%d%%)", event.Path, event.Files, event.Done, event.Total, percent)
		p.line = true
	}
	if event.Phase == "finished" {
		fmt.Fprintf(p.out, "Pastas concluídas: %d/%d (%d%%)\n", event.Done, event.Total, event.Done*100/event.Total)
	}
}

func (p *LibraryProgress) Finish() {
	p.clear()
	if p.filter == "" {
		fmt.Fprintf(p.out, "Pastas de sistemas vazias: %d\n", p.empty)
	}
}

func snapshotOrigin(snapshot library.Snapshot) string {
	origin := "origem indeterminada"
	if snapshot.LocalSource != nil {
		return "backup local " + strconv.Quote(snapshot.LocalSource.Name)
	}
	if snapshot.Partition != nil {
		origin = *snapshot.Partition
	} else if snapshot.Device != nil {
		origin = *snapshot.Device
	}
	if snapshot.VolumeID != "" {
		origin += " (" + snapshot.VolumeID + ")"
	}
	return origin
}

func writeSnapshot(out io.Writer, snapshot library.Snapshot, cached bool) {
	status := ""
	if cached {
		status = " [em cache, " + snapshot.ExaminedAt.Format(time.RFC3339) + "]"
	}
	if !snapshot.Complete {
		status += " [leitura parcial]"
	}
	family := ""
	if len(snapshot.Systems) != 0 {
		for _, item := range libraryFamilies {
			if item.id == snapshot.Systems[0].Family {
				family = item.label + " | "
				break
			}
		}
	} else if snapshot.System == "ports" {
		family = "Ports | "
	}
	fmt.Fprintf(out, "%s%q — %s%s", family, snapshot.Path, snapshotOrigin(snapshot), status)
	if snapshot.Empty {
		fmt.Fprintln(out, " | pasta vazia")
		return
	}
	var games, wrong, launchers, other, duplicates int
	for _, system := range snapshot.Systems {
		games += system.Counts["games"]
		duplicates += system.DuplicateGames
		for category, count := range system.Counts {
			if category != "games" {
				other += count
			}
		}
		for _, check := range system.ROMChecks {
			if check.Comparison == library.ROMWrongFolder {
				wrong++
			}
		}
	}
	for _, ports := range snapshot.Ports {
		launchers += ports.LauncherCount
	}
	if snapshot.System == "ports" {
		fmt.Fprintf(out, " | lançadores: %d\n", launchers)
		return
	}
	if len(snapshot.Systems) == 1 && snapshot.Systems[0].CatalogOnly {
		fmt.Fprintln(out, " | pasta auxiliar")
		return
	}
	fmt.Fprintf(out, " | jogos: %d | outros arquivos: %d | possíveis jogos fora da pasta: %d", games, other, wrong)
	if duplicates > 0 {
		fmt.Fprintf(out, " | jogos já contados por outro caminho: %d", duplicates)
	}
	if games == 0 && duplicates == 0 && snapshot.Complete {
		fmt.Fprint(out, " | contém arquivos, sem jogos identificados")
	}
	fmt.Fprintln(out)
}

type CachedWarning struct {
	library.ROMCheck
	ExaminedAt  time.Time            `json:"examined_at"`
	VolumeID    string               `json:"volume_id"`
	LocalSource *library.LocalSource `json:"local_source,omitempty"`
}

type CacheReport struct {
	Coverage    []library.Snapshot `json:"coverage"`
	Warnings    []CachedWarning    `json:"warnings,omitempty"`
	Limitations []string           `json:"limitations"`
}

func ReadCacheReport(cache library.Cache, filter string, warningsOnly bool) CacheReport {
	snapshots, issues := cache.Snapshots(filter)
	if warningsOnly {
		issues = append(issues, "cobertura limitada às pastas registradas no cache")
	}
	report := CacheReport{Coverage: snapshots, Warnings: []CachedWarning{}, Limitations: issues}
	if warningsOnly {
		for i, snapshot := range snapshots {
			for _, system := range snapshot.Systems {
				for _, check := range system.ROMChecks {
					if check.Comparison == library.ROMWrongFolder {
						origin := snapshot.Files[check.Path].VolumeID
						report.Warnings = append(report.Warnings, CachedWarning{check, snapshot.ExaminedAt, origin, snapshot.LocalSource})
					}
				}
			}
			report.Coverage[i].Systems = nil
			report.Coverage[i].Ports = nil
		}
	}
	for i := range report.Coverage {
		report.Coverage[i].Files = nil
	}
	return report
}

// WriteCachedFolder displays the previous inventory before a text refresh.
func WriteCachedFolder(out io.Writer, snapshot library.Snapshot) {
	writeSnapshot(out, snapshot, true)
}

func WriteCacheReport(out io.Writer, report CacheReport, warningsOnly bool) error {
	var text strings.Builder
	fmt.Fprintln(&text, "INVENTÁRIO EM CACHE")
	if len(report.Coverage) == 0 {
		fmt.Fprintln(&text, "Nenhuma pasta registrada para esta consulta.")
	}
	for _, snapshot := range report.Coverage {
		if warningsOnly {
			fmt.Fprintf(&text, "  Cobertura: %q — %s — %s\n", snapshot.Path, snapshotOrigin(snapshot), snapshot.ExaminedAt.Format(time.RFC3339))
		} else {
			writeSnapshot(&text, snapshot, true)
		}
	}
	for _, warning := range report.Warnings {
		origin := warning.MountTarget
		if warning.LocalSource != nil {
			origin = "backup local " + strconv.Quote(warning.LocalSource.Name)
		} else if warning.Partition != nil {
			origin = *warning.Partition
		} else if warning.Device != nil {
			origin = *warning.Device
		}
		fmt.Fprintf(&text, "  Possível pasta errada: %q — %s (%s) — %s\n",
			warning.Path, origin, warning.VolumeID, warning.ExaminedAt.Format(time.RFC3339))
	}
	for _, limitation := range report.Limitations {
		fmt.Fprintln(&text, "  Limitação:", limitation)
	}
	_, err := io.WriteString(out, text.String())
	return err
}
