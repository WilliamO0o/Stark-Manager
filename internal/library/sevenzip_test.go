package library_test

import (
	"encoding/binary"
	"hash/crc32"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WilliamO0o/Stark-Manager/internal/presentation"
)

func TestLibrarySNES7z(t *testing.T) {
	game := read7zFixture(t, "snes-index.7z")
	oversized := append([]byte(nil), game...)
	binary.LittleEndian.PutUint64(oversized[20:28], 1<<40)
	binary.LittleEndian.PutUint32(oversized[8:12], crc32.ChecksumIEEE(oversized[12:32]))
	root := fstest.MapFS{
		"proc/self/mountinfo":        {Data: []byte("1 1 8:1 / / rw - ext4 /dev/sda1 rw\n")},
		"etc/fstab":                  {},
		"roms/snes/Mario.7z":         {Data: game},
		"roms/snes/Mario.srm":        {},
		"roms/snes/Mario.state":      {},
		"roms/snes/NoROM.7z":         {Data: read7zFixture(t, "no-rom.7z")},
		"roms/snes/Encrypted.7z":     {Data: read7zFixture(t, "encrypted-index.7z")},
		"roms/snes/Invalid.7z":       {Data: append([]byte(nil), game...)},
		"roms/snes/Oversized.7z":     {Data: oversized},
		"roms/snes/Uninspected.rar":  {},
		"roms/snes/Mario.cht":        {},
		"roms/snes/Mario.spc":        {},
		"roms/snes/Mario.zmv":        {},
		"roms/snes/Mario.zm1":        {},
		"roms/snes/Mario.zm9":        {},
		"roms/snes/Mario.s00":        {},
		"roms/snes/Mario.s01":        {},
		"roms/snes/Mario.zss":        {},
		"roms/snes/gamelist.xml.old": {},
		"roms/snes/keep.old":         {},
		"roms/nes/Mario.cht":         {},
		"roms/nes/Mario.spc":         {},
		"roms/nes/Mario.zmv":         {},
		"roms/nes/Mario.s00":         {},
		"roms/nes/gamelist.xml.old":  {},
	}
	broken := root["roms/snes/Invalid.7z"].Data
	broken[len(broken)-1] ^= 0xff
	report := collect(root, root, "")
	if len(report.Systems) != 2 || report.Systems[0].Counts["unknown"] != 5 {
		t.Fatalf("SNES rules leaked to other systems: %+v", report.Systems)
	}
	snes := report.Systems[1]
	wantCounts := map[string]int{
		"games": 1, "saves": 1, "states": 1, "unknown": 5, "uninspected_packages": 1,
		"cheats": 1, "audio": 1, "replays": 3, "possible_states": 3, "metadata_backups": 1,
	}
	for category, count := range snes.Counts {
		if count != wantCounts[category] {
			t.Errorf("%s: got %d, want %d", category, count, wantCounts[category])
		}
	}
	for category, want := range wantCounts {
		if snes.Counts[category] != want {
			t.Errorf("%s: got %d, want %d", category, snes.Counts[category], want)
		}
	}
	if len(snes.Games) != 1 || snes.Games[0].Path != "/roms/snes/Mario.7z" || len(snes.Games[0].Related) != 10 {
		t.Errorf("package basename or game count: %+v", snes.Games)
	}
	if !reflect.DeepEqual(snes.UnknownFiles, []string{
		"/roms/snes/Encrypted.7z", "/roms/snes/Invalid.7z", "/roms/snes/NoROM.7z", "/roms/snes/Oversized.7z", "/roms/snes/keep.old",
	}) || !reflect.DeepEqual(snes.UninspectedPackages, []string{"/roms/snes/Uninspected.rar"}) {
		t.Errorf("unexpected unknown or uninspected paths: %v, %v", snes.UnknownFiles, snes.UninspectedPackages)
	}
	wantIssues := map[string]string{"Encrypted.7z": "criptografado", "Invalid.7z": "inválido", "Oversized.7z": "limite"}
	for _, issue := range report.Pending {
		name := strings.TrimPrefix(issue.Path, "/roms/snes/")
		want, ok := wantIssues[name]
		if !ok || !strings.Contains(issue.Reason, want) {
			t.Errorf("unexpected issue: %+v", issue)
		}
		delete(wantIssues, name)
	}
	if len(wantIssues) != 0 {
		t.Errorf("missing issues: %v", wantIssues)
	}
	var out strings.Builder
	if err := presentation.WriteLibrary(&out, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "pacote não inspecionado: 1") || !strings.Contains(out.String(), "possível estado: 3") {
		t.Errorf("missing classifications in the summary:\n%s", out.String())
	}

	root["roms/snes/Mario.sfc"] = &fstest.MapFile{}
	filtered := collect(root, root, "snes").Systems[0]
	if len(filtered.Games) != 2 || len(filtered.Unassociated) != 10 {
		t.Errorf("ambiguous associations should stay unresolved: %+v", filtered)
	}
}

func read7zFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
