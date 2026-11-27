package presentation

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/WilliamO0o/Stark-Manager/internal/library"
)

var libraryFamilies = []struct{ id, label string }{
	{"nintendo", "Nintendo"}, {"sega", "Sega"}, {"sony", "Sony"}, {"atari", "Atari"},
	{"arcade", "Arcade"}, {"computers", "Computadores"}, {"engines", "Engines"},
	{"ports", "Ports"}, {"software", "Plataformas de software"}, {"support", "Suporte e mídia"}, {"other", "Outros sistemas"},
	{"unidentified", "Não identificados"},
}

// WriteLibrary prints the library inventory grouped by family and origin.
func WriteLibrary(w io.Writer, report library.Report) error {
	return writeLibrary(w, report, true)
}

// WriteLibraryNotes emits the remaining details after interactive folder summaries.
func WriteLibraryNotes(w io.Writer, report library.Report) error {
	return writeLibrary(w, report, false)
}

func writeLibrary(w io.Writer, report library.Report, summaries bool) error {
	var out strings.Builder
	out.WriteString("BIBLIOTECA DE ROMs\n")
	for _, root := range report.Examined {
		fmt.Fprintf(&out, "Biblioteca examinada: %s\n", strconv.Quote(root))
	}
	if summaries {
		empty := func(paths []string) bool {
			if len(paths) == 0 {
				return false
			}
			for _, name := range paths {
				if !slices.Contains(report.EmptySystemFolders, name) {
					return false
				}
			}
			return true
		}
		systems := slices.Clone(report.Systems)
		familyIndex := func(family string) int {
			index := slices.IndexFunc(libraryFamilies, func(item struct{ id, label string }) bool { return item.id == family })
			if index < 0 {
				return len(libraryFamilies) - 1
			}
			return index
		}
		slices.SortStableFunc(systems, func(a, b library.System) int { return familyIndex(a.Family) - familyIndex(b.Family) })
		lastFamily := -1
		for _, system := range systems {
			if report.Filter == "" && empty(system.Paths) {
				continue
			}
			family := familyIndex(system.Family)
			if family != lastFamily {
				fmt.Fprintf(&out, "\n%s\n", libraryFamilies[family].label)
				lastFamily = family
			}
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
			name := strings.Trim(strconv.Quote(system.Name), "\"")
			if system.DisplayName != "" {
				name = strconv.Quote(system.DisplayName) + " (" + name + ")"
			}
			if system.Type == "auxiliary" {
				name += " [conteúdo auxiliar]"
			}
			fmt.Fprintf(&out, "  %s — %s — %s\n", name, origin, strings.Join(paths, ", "))
			if empty(system.Paths) {
				out.WriteString("    Pasta vazia.\n")
				continue
			}
			if system.CatalogOnly {
				out.WriteString("    Pasta observada sem contagem de arquivos.\n")
				continue
			}
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
			wrongFolder := 0
			for _, check := range system.ROMChecks {
				if check.Comparison == library.ROMWrongFolder {
					wrongFolder++
				}
			}
			if wrongFolder > 0 {
				fmt.Fprintf(&out, "  Possíveis arquivos na pasta errada: %d\n", wrongFolder)
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
		if len(report.Ports) != 0 {
			out.WriteString("\nPorts\n")
			for _, ports := range report.Ports {
				if empty([]string{ports.Path}) {
					if report.Filter != "" {
						fmt.Fprintf(&out, "  %q — pasta vazia.\n", ports.Path)
					}
					continue
				}
				origin := "origem desconhecida"
				if ports.Partition != nil {
					origin = "partição " + *ports.Partition
				} else if ports.Device != nil {
					origin = "dispositivo " + *ports.Device
				}
				fmt.Fprintf(&out, "  %s — %s | lançadores: %d | pastas relacionadas: %d | metadados: %d\n",
					strconv.Quote(ports.Path), origin, ports.LauncherCount, len(ports.RelatedDirectories), len(ports.Metadata))
				if ports.DuplicateLaunchers > 0 {
					fmt.Fprintf(&out, "    Lançadores já contados em outro caminho: %d\n", ports.DuplicateLaunchers)
				}
			}
		}
		if report.Filter == "" {
			fmt.Fprintf(&out, "Pastas de sistemas vazias: %d\n", len(report.EmptySystemFolders))
		}
	}
	if len(report.Systems) == 0 && len(report.Ports) == 0 {
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
