package library_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WilliamO0o/Stark-Manager/internal/diagnostic"
	"github.com/WilliamO0o/Stark-Manager/internal/library"
	"github.com/WilliamO0o/Stark-Manager/internal/presentation"
	"github.com/WilliamO0o/Stark-Manager/internal/systems"
)

func TestLibraryROMFormatsAndPorts(t *testing.T) {
	root := fstest.MapFS{
		"proc/self/mountinfo": {Data: []byte("1 1 8:1 / / rw - ext4 /dev/sda1 rw\n2 1 179:1 / /roms rw - ext4 /dev/mmcblk0p1 rw\n")},
		"etc/fstab":           {},
		"etc/emulationstation/es_systems.cfg": {Data: []byte(`<systemList>
		<system><name>gb</name><fullname>Game Boy</fullname><path>/roms/gb</path><extension>.GB .zip</extension></system>
		<system><name>gbc</name><fullname>Game Boy Color</fullname><path>/roms/gbc</path><extension>.gb .gbc .zip</extension></system>
		<system><name>gba</name><fullname>Game Boy Advance</fullname><path>/roms/custom/advance</path><extension>.gba .7Z</extension></system>
		<system><name>famicom</name><fullname>Famicom</fullname><path>/roms/famicom</path><extension>.nes .unif .zip</extension></system>
		<system><name>snes</name><fullname>Super NES</fullname><path>/roms/snes</path><extension>.sfc .zip</extension></system>
		<system><name>snes</name><fullname>SuFami Turbo</fullname><path>/roms/sufami</path><extension>.st .smc</extension></system>
		<system><name>ports</name><fullname>Ports</fullname><path>/roms/ports</path><extension>.sh</extension></system>
		</systemList>`)},
		"roms/gb/platform.gb":                   {},
		"roms/gb/color.gbc":                     {},
		"roms/gbc/color.GBC":                    {},
		"roms/gbc/shared.gb":                    {},
		"roms/gbc/advance.GBA":                  {},
		"roms/custom/advance/deep/game.gba":     {},
		"roms/custom/before/unknown.gba":        {},
		"roms/custom/advance/game.7z":           {Data: read7zFixture(t, "gba-index.7z")},
		"roms/custom/advance/unsupported.zip":   {Data: zipFixture(t, "game.gba")},
		"roms/famicom/deep/game.unif":           {},
		"roms/famicom/plain.NES":                {},
		"roms/famicom/packed.zip":               {Data: zipFixture(t, "game.nes")},
		"roms/famicom/mixed.zip":                {Data: zipFixture(t, "game.nes", "game.gba")},
		"roms/famicom/ambiguous.bin":            {},
		"roms/famicom/broken.zip":               {Data: []byte("invalid")},
		"roms/famicom/skipped.rar":              {},
		"roms/snes/regular.sfc":                 {},
		"roms/snes/advance.zip":                 {Data: zipFixture(t, "game.gba")},
		"roms/sufami/variant.smc":               {},
		"roms/sufami/ambiguous.st":              {},
		"roms/ports/play.sh":                    {Data: []byte("#!/bin/sh\nexit 99\n")},
		"roms/ports/data/another.SH":            {},
		"roms/ports/data/manifest.json":         {},
		"roms/ports/data/assets/payload.bin":    {},
		"roms/ports/gamelist.xml":               {Data: []byte("<incomplete")},
		"roms/ports_scripts/scripts.sh":         {},
		"roms/tools/utility.sh":                 {},
		"sys/class/block/mmcblk0/dev":           {Data: []byte("179:0")},
		"sys/class/block/mmcblk0/ro":            {Data: []byte("0")},
		"sys/class/block/mmcblk0/size":          {Data: []byte("100")},
		"sys/class/block/mmcblk0/mmcblk0p1/dev": {Data: []byte("179:1")},
		"sys/class/block/mmcblk0p1/dev":         {Data: []byte("179:1")},
		"sys/class/block/mmcblk0p1/ro":          {Data: []byte("0")},
		"sys/class/block/mmcblk0p1/size":        {Data: []byte("90")},
		"sys/class/block/mmcblk0p1/partition":   {Data: []byte("1")},
	}
	catalog := systems.LibraryCatalog(root, "", "")
	storage := diagnostic.CollectStorage(root, nil)
	lstat := func(name string) (fs.FileInfo, error) { return fs.Stat(root, strings.TrimPrefix(name, "/")) }
	report := library.Collect(root, storage, lstat, "", catalog...)
	checks := make(map[string]library.ROMCheck)
	for _, system := range report.Systems {
		if system.Name == "ports" {
			t.Fatal("Ports appeared as an ordinary system")
		}
		for _, check := range system.ROMChecks {
			checks[check.Path] = check
		}
	}
	for _, test := range []struct{ file, system, format, result string }{
		{"gb/platform.gb", "gb", "gb", library.ROMIndeterminate},
		{"gb/color.gbc", "gb", "gbc", library.ROMIndeterminate},
		{"gbc/color.GBC", "gbc", "gbc", library.ROMCompatible},
		{"gbc/shared.gb", "gbc", "gb", library.ROMIndeterminate},
		{"gbc/advance.GBA", "gbc", "gba", library.ROMWrongFolder},
		{"custom/advance/deep/game.gba", "gba", "gba", library.ROMCompatible},
		{"custom/before/unknown.gba", "unknown", "gba", library.ROMIndeterminate},
		{"custom/advance/game.7z", "gba", "7z", library.ROMCompatible},
		{"custom/advance/unsupported.zip", "gba", "zip", library.ROMIndeterminate},
		{"famicom/deep/game.unif", "nes", "unif", library.ROMCompatible},
		{"famicom/plain.NES", "nes", "nes", library.ROMCompatible},
		{"famicom/packed.zip", "nes", "zip", library.ROMCompatible},
		{"famicom/mixed.zip", "nes", "zip", library.ROMIndeterminate},
		{"famicom/ambiguous.bin", "nes", "bin", library.ROMIndeterminate},
		{"famicom/broken.zip", "nes", "zip", library.ROMIndeterminate},
		{"famicom/skipped.rar", "nes", "rar", library.ROMIndeterminate},
		{"snes/regular.sfc", "snes", "sfc", library.ROMCompatible},
		{"snes/advance.zip", "snes", "zip", library.ROMWrongFolder},
		{"sufami/variant.smc", "sufami", "smc", library.ROMIndeterminate},
		{"sufami/ambiguous.st", "sufami", "st", library.ROMIndeterminate},
	} {
		check := checks["/roms/"+test.file]
		if check.ExpectedSystem != test.system || check.Format != test.format || check.Comparison != test.result ||
			check.Partition == nil || *check.Partition != "mmcblk0p1" || check.MountTarget != "/roms" {
			t.Errorf("%s: %+v; want %s/%s/%s", test.file, check, test.system, test.format, test.result)
		}
		if test.result == library.ROMIndeterminate && len(check.Limitations) == 0 {
			t.Errorf("missing limitation for %s", test.file)
		}
	}
	if got := checks["/roms/custom/advance/game.7z"].RecognizedSystems; !reflect.DeepEqual(got, []string{"gba"}) {
		t.Errorf("7z must report its internal ROM system: %v", got)
	}
	if len(report.Pending) != 1 || report.Pending[0].Path != "/roms/famicom/broken.zip" {
		t.Errorf("unexpected limitations: %+v", report.Pending)
	}
	if len(report.Ports) != 1 {
		t.Fatalf("ports: %+v", report.Ports)
	}
	ports := report.Ports[0]
	if ports.LauncherCount != 2 || len(ports.RelatedDirectories) != 2 || len(ports.Metadata) != 2 ||
		len(ports.CatalogEntries) != 1 || ports.CatalogEntries[0].Extension != ".sh" ||
		ports.Launchers[0].Partition == nil || *ports.Launchers[0].Partition != "mmcblk0p1" {
		t.Errorf("unexpected Ports report: %+v", ports)
	}
	filter := library.Collect(root, storage, lstat, "ports", catalog...)
	if len(filter.Systems) != 0 || !reflect.DeepEqual(filter.Ports, report.Ports) {
		t.Errorf("Ports filter: %+v", filter)
	}

	first, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(library.Collect(root, storage, lstat, "", catalog...))
	if err != nil || !bytes.Equal(first, second) {
		t.Fatal("reports must be deterministic")
	}
	var terminal strings.Builder
	if err := presentation.WriteLibrary(&terminal, report); err != nil {
		t.Fatal(err)
	}
	text := terminal.String()
	if !strings.Contains(text, "Possíveis arquivos na pasta errada: 1") || !strings.Contains(text, "lançadores: 2") ||
		strings.Contains(text, "play.sh") || strings.Contains(text, "advance.GBA") {
		t.Errorf("expected compact Ports and compatibility summaries:\n%s", text)
	}
}
