package presentation

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/WilliamO0o/Stark-Manager/internal/library"
)

// WriteLibrary prints the library inventory grouped by system and origin.
func WriteLibrary(w io.Writer, report library.Report) error {
	var out strings.Builder
	out.WriteString("BIBLIOTECA DE ROMs\n")
	for _, root := range report.Examined {
		fmt.Fprintf(&out, "Biblioteca examinada: %s\n", strconv.Quote(root))
	}
	for _, system := range report.Systems {
		origin := "origem desconhecida"
		if system.Partition != nil {
			origin = "partição " + *system.Partition
		} else if system.Device != nil {
			origin = "dispositivo " + *system.Device
		}
		paths := make([]string, 0, len(system.Paths))
		for _, name := range system.Paths {
			paths = append(paths, strconv.Quote(name))
		}
		fmt.Fprintf(&out, "\n%s — %s — %s\n", strings.Trim(strconv.Quote(system.Name), "\""), origin, strings.Join(paths, ", "))
		counts := system.Counts
		fmt.Fprintf(&out, "  Jogos: %d | saves: %d | estados: %d | imagens: %d | vídeos: %d | metadados: %d | desconhecidos: %d\n",
			counts["games"], counts["saves"], counts["states"], counts["images"], counts["videos"], counts["metadata"], counts["unknown"])
		var extras []string
		for _, extra := range []struct{ key, label string }{
			{"cheats", "cheats"}, {"audio", "áudio"}, {"replays", "replays"},
			{"metadata_backups", "prováveis backups de metadados"}, {"possible_states", "possível estado"},
			{"uninspected_packages", "pacote não inspecionado"},
		} {
			if count := counts[extra.key]; count != 0 {
				extras = append(extras, fmt.Sprintf("%s: %d", extra.label, count))
			}
		}
		if len(extras) != 0 {
			fmt.Fprintln(&out, " ", strings.Join(extras, " | "))
		}
		fmt.Fprintf(&out, "  Pastas vazias: %d\n", len(system.EmptyFolders))
		if system.DuplicateFiles > 0 {
			fmt.Fprintf(&out, "  Arquivos já contados em outro caminho: %d\n", system.DuplicateFiles)
		}
		if len(system.FoldersWithoutGames) > 0 {
			fmt.Fprintf(&out, "  Pastas com conteúdo, sem jogos identificados: %d\n", len(system.FoldersWithoutGames))
		}
		if counts["games"] == 0 && system.DuplicateGames == 0 {
			out.WriteString("  Sem jogos identificados.\n")
		}
	}
	if len(report.Systems) == 0 {
		if report.Filter != "" {
			fmt.Fprintf(&out, "Sistema %s ausente nas pastas examinadas.\n", strconv.Quote(report.Filter))
		} else {
			out.WriteString("Nenhum sistema encontrado nas bibliotecas examinadas.\n")
		}
	}
	for _, item := range report.NotExamined {
		device := item.Device
		if item.Partition != nil {
			device += "/" + *item.Partition
		}
		fmt.Fprintf(&out, "  %s — não examinado (%s).\n", device, item.Reason)
	}
	for _, item := range report.Pending {
		fmt.Fprintf(&out, "  Pendente %s — %s\n", strconv.Quote(item.Path), item.Reason)
	}
	for _, limitation := range report.Limitations {
		fmt.Fprintf(&out, "  Limitação: %s\n", limitation)
	}
	_, err := io.WriteString(w, out.String())
	return err
}
