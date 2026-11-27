package presentation

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/WilliamO0o/Stark-Manager/internal/diagnostic"
)

func TestWriteStatus(t *testing.T) {
	report := diagnostic.Report{
		System: withData(diagnostic.System{
			Hostname: "Ubuntu-Gamer", Architecture: "amd64", OSName: "Ubuntu 26.04", KernelRelease: "7.0.0-44",
		}),
		Memory: withData(diagnostic.Memory{
			TotalBytes: ptr(uint64(32427003084)), AvailableBytes: ptr(uint64(6335076761)),
		}),
		Batteries: withData([]diagnostic.Battery{
			{Name: "BAT0", Status: "Discharging", CapacityPercent: ptr(uint64(0))},
			{Name: "BAT1", Present: ptr(false)},
		}),
		Storage: withData([]diagnostic.BlockDevice{
			{Name: "loop0", Kind: "device"},
			{Name: "zram0", Kind: "device"},
			{Name: "nvme0n1", Kind: "device", SizeBytes: ptr(uint64(2 << 40))},
			{Name: "nvme0n1p1", Kind: "partition"},
			{Name: "sda", Kind: "device", SizeBytes: ptr(uint64(238 << 30))},
			{Name: "mmcblk0", Kind: "device", SizeBytes: ptr(uint64(512 << 20))},
			{Name: "mmcblk0p1", Kind: "partition"},
			{Name: "sdb", Kind: "device"},
			{Name: "uncertain"},
		}),
	}
	originalStorage := append([]diagnostic.BlockDevice(nil), (*report.Storage.Data)...)
	var out bytes.Buffer
	if err := WriteStatus(&out, "0.1.1", report); err != nil {
		t.Fatal(err)
	}

	const want = `STARK MANAGER v0.1.1

SISTEMA
Dispositivo: Ubuntu-Gamer
Sistema operacional: Ubuntu 26.04
Arquitetura: AMD64
Kernel: 7.0.0-44

MEMÓRIA
RAM total: 30,2 GiB
RAM disponível: 5,9 GiB

BATERIA
BAT0: carga 0% | estado: Descarregando

ARMAZENAMENTO
nvme0n1: 2 TiB
sda: 238 GiB
mmcblk0: 512 MiB
sdb: Indisponível
`
	if out.String() != want {
		t.Errorf("resultado inesperado:\n%s\nesperado:\n%s", out.String(), want)
	}
	if !reflect.DeepEqual(*report.Storage.Data, originalStorage) {
		t.Error("a apresentação alterou o inventário de armazenamento")
	}
}

func TestWriteStatusMissingData(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		batteries              diagnostic.Section[[]diagnostic.Battery]
		storage                diagnostic.Section[[]diagnostic.BlockDevice]
		wantBattery, wantStore string
	}{
		{
			name:        "unavailable",
			wantBattery: "Informações de bateria indisponíveis.",
			wantStore:   "Informações de armazenamento indisponíveis.",
		},
		{
			name:        "empty",
			batteries:   withData([]diagnostic.Battery{}),
			storage:     withData([]diagnostic.BlockDevice{{Name: "loop0", Kind: "device"}}),
			wantBattery: "Nenhuma bateria detectada.",
			wantStore:   "Nenhum dispositivo de armazenamento para exibir.",
		},
		{
			name:        "partial",
			batteries:   diagnostic.Section[[]diagnostic.Battery]{Status: "partial", Data: ptr([]diagnostic.Battery{})},
			storage:     diagnostic.Section[[]diagnostic.BlockDevice]{Status: "partial", Data: ptr([]diagnostic.BlockDevice{})},
			wantBattery: "Informações de bateria incompletas.",
			wantStore:   "Informações de armazenamento incompletas.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			report := diagnostic.Report{
				System:    withData(diagnostic.System{Model: "K36", OSID: "debian", OSVersionID: "13"}),
				Memory:    withData(diagnostic.Memory{AvailableBytes: ptr(uint64(0))}),
				Batteries: tc.batteries,
				Storage:   tc.storage,
			}
			if err := WriteStatus(&out, "0.1.1", report); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{
				"Dispositivo: K36", "Sistema operacional: debian 13", "Kernel: Indisponível",
				"RAM total: Indisponível", "RAM disponível: 0 MiB", tc.wantBattery, tc.wantStore,
			} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("faltou %q na saída:\n%s", want, out.String())
				}
			}
		})
	}
}

func withData[T any](data T) diagnostic.Section[T] {
	return diagnostic.Section[T]{Status: "ok", Data: &data}
}

func ptr[T any](value T) *T {
	return &value
}
