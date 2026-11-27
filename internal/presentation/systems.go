package presentation

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/WilliamO0o/Stark-Manager/internal/systems"
)

// WriteSystems summarizes the catalog and its comparison with mounted libraries.
func WriteSystems(w io.Writer, report systems.Report) error {
	var out strings.Builder
	out.WriteString("CATÁLOGO DO EMULATIONSTATION\n")
	seenBinds := make(map[[2]string]bool)
	writeBinds := func(assessment systems.Assessment) {
		for _, bind := range assessment.Binds {
			key := [2]string{bind.Source, bind.Target}
			if !seenBinds[key] {
				seenBinds[key] = true
				fmt.Fprintf(&out, "  Bind compatível: %s → %s\n", strconv.Quote(bind.Source), strconv.Quote(bind.Target))
			}
		}
	}
	for _, catalog := range report.Catalogs {
		switch catalog.Status {
		case "ok":
			fmt.Fprintf(&out, "Arquivo lido: %s (%d entradas)\n", strconv.Quote(catalog.Path), len(catalog.Systems))
		case "invalid":
			fmt.Fprintf(&out, "Arquivo lido: %s (XML inválido)\n", strconv.Quote(catalog.Path))
		default:
			fmt.Fprintf(&out, "Arquivo indisponível: %s\n", strconv.Quote(catalog.Path))
		}
		for _, entry := range catalog.Systems {
			writeSystemEntry(&out, entry)
			writeBinds(entry.Location)
		}
	}
	outside := 0
	for _, folder := range report.Folders {
		switch folder.Assessment.Status {
		case "undeclared":
			outside++
			fmt.Fprintf(&out, "  Pasta sem caminho declarado: %s\n", strconv.Quote(folder.Path))
		case "not_examined":
			fmt.Fprintf(&out, "  Origem não examinada: %s (%s)\n", strconv.Quote(folder.Path), folder.Assessment.Reason)
		}
		writeBinds(folder.Assessment)
	}
	if report.CatalogStatus == "ok" {
		fmt.Fprintf(&out, "Pastas observadas: %d | sem caminho declarado: %d\n", len(report.Folders), outside)
	} else {
		fmt.Fprintf(&out, "Pastas observadas: %d | comparação com o catálogo indeterminada\n", len(report.Folders))
	}
	for _, item := range report.NotExamined {
		device := item.Device
		if item.Partition != nil {
			device = *item.Partition
		}
		fmt.Fprintf(&out, "  Origem não examinada: %s (%s)\n", device, item.Reason)
	}
	for _, item := range report.Pending {
		fmt.Fprintf(&out, "  Caminho pendente: %s (%s)\n", strconv.Quote(item.Path), item.Reason)
	}
	for _, issue := range report.Limitations {
		fmt.Fprintf(&out, "  Limitação: %s\n", issue)
	}
	_, err := io.WriteString(w, out.String())
	return err
}

func writeSystemEntry(out *strings.Builder, entry systems.System) {
	// The compact view lists exceptions. The JSON
	// includes the full record for entries whose path was found as well.
	switch entry.Location.Status {
	case "missing":
		fmt.Fprintf(out, "  %s — caminho configurado ausente: %s\n", strconv.Quote(entry.Name), strconv.Quote(entry.Path))
	case "not_examined":
		fmt.Fprintf(out, "  %s — origem não examinada: %s (%s)\n",
			strconv.Quote(entry.Name), strconv.Quote(entry.Path), entry.Location.Reason)
	}
}
