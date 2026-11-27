// Package presentation formats diagnostic reports for terminal users.
package presentation

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/WilliamO0o/Stark-Manager/internal/diagnostic"
)

const unavailable = "Indisponível"

// WriteStatus prints a concise summary of a collected report.
func WriteStatus(w io.Writer, version string, report diagnostic.Report) error {
	var out strings.Builder
	fmt.Fprintf(&out, "STARK MANAGER v%s\n\n", version)

	out.WriteString("SISTEMA\n")
	system := report.System.Data
	if system == nil {
		system = &diagnostic.System{}
	}
	device := system.Hostname
	if device == "" {
		device = system.Model
	}
	osName := system.OSName
	if osName == "" {
		osName = strings.TrimSpace(system.OSID + " " + system.OSVersionID)
	}
	fmt.Fprintf(&out, "Dispositivo: %s\nSistema operacional: %s\nArquitetura: %s\nKernel: %s\n",
		valueOrUnavailable(device), valueOrUnavailable(osName),
		valueOrUnavailable(strings.ToUpper(system.Architecture)), valueOrUnavailable(system.KernelRelease))

	out.WriteString("\nMEMÓRIA\n")
	memory := report.Memory.Data
	if memory == nil {
		memory = &diagnostic.Memory{}
	}
	fmt.Fprintf(&out, "RAM total: %s\nRAM disponível: %s\n", byteSize(memory.TotalBytes), byteSize(memory.AvailableBytes))

	out.WriteString("\nBATERIA\n")
	writeBatteries(&out, report.Batteries)

	out.WriteString("\nARMAZENAMENTO\n")
	writeStorage(&out, report.Storage)
	_, err := io.WriteString(w, out.String())
	return err
}

func writeBatteries(out *strings.Builder, section diagnostic.Section[[]diagnostic.Battery]) {
	if section.Data == nil {
		out.WriteString("Informações de bateria indisponíveis.\n")
		return
	}
	found := false
	for _, battery := range *section.Data {
		if battery.Present != nil && !*battery.Present {
			continue
		}
		found = true
		capacity := unavailable
		if battery.CapacityPercent != nil {
			capacity = fmt.Sprintf("%d%%", *battery.CapacityPercent)
		}
		fmt.Fprintf(out, "%s: carga %s | estado: %s\n", battery.Name, strings.ToLower(capacity), batteryState(battery.Status))
	}
	if section.Status == "partial" {
		out.WriteString("Informações de bateria incompletas.\n")
	} else if !found {
		out.WriteString("Nenhuma bateria detectada.\n")
	}
}

func batteryState(status string) string {
	switch status {
	case "Charging":
		return "Carregando"
	case "Discharging":
		return "Descarregando"
	case "Not charging":
		return "Sem carregar"
	case "Full":
		return "Carga completa"
	case "Unknown":
		return "Desconhecido"
	default:
		return valueOrUnavailable(status)
	}
}

func writeStorage(out *strings.Builder, section diagnostic.Section[[]diagnostic.BlockDevice]) {
	if section.Data == nil {
		out.WriteString("Informações de armazenamento indisponíveis.\n")
		return
	}
	found := false
	for _, device := range *section.Data {
		// Only include entries identified as whole block devices.
		if device.Kind != "device" || strings.HasPrefix(device.Name, "loop") || strings.HasPrefix(device.Name, "zram") {
			continue
		}
		found = true
		fmt.Fprintf(out, "%s: %s\n", device.Name, byteSize(device.SizeBytes))
	}
	if section.Status == "partial" {
		out.WriteString("Informações de armazenamento incompletas.\n")
	} else if !found {
		out.WriteString("Nenhum dispositivo de armazenamento para exibir.\n")
	}
}

func valueOrUnavailable(value string) string {
	if value == "" {
		return unavailable
	}
	return value
}

func byteSize(bytes *uint64) string {
	if bytes == nil {
		return unavailable
	}

	const (
		mib = 1 << 20
		gib = 1 << 30
		tib = 1 << 40
	)
	scale, unit := float64(mib), "MiB"
	switch {
	case *bytes >= tib:
		scale, unit = tib, "TiB"
	case *bytes >= gib:
		scale, unit = gib, "GiB"
	}
	size := float64(*bytes) / scale
	if size > 0 && size < 0.05 {
		return "< 0,1 " + unit
	}
	rounded := math.Round(size*10) / 10
	return strings.Replace(strconv.FormatFloat(rounded, 'f', -1, 64), ".", ",", 1) + " " + unit
}
