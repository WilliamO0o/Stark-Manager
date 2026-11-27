package library_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WilliamO0o/Stark-Manager/internal/diagnostic"
	"github.com/WilliamO0o/Stark-Manager/internal/library"
	"github.com/WilliamO0o/Stark-Manager/internal/presentation"
	"github.com/WilliamO0o/Stark-Manager/internal/systems"
)

func TestLibraryFamiliesFromCatalog(t *testing.T) {
	data, err := os.ReadFile("testdata/families.cfg")
	if err != nil {
		t.Fatal(err)
	}
	root := fstest.MapFS{
		"proc/self/mountinfo":                 {Data: []byte("1 1 8:1 / / rw - ext4 /dev/sda1 rw\n2 1 8:2 / /roms2 rw - ext4 /dev/sda2 rw\n")},
		"etc/fstab":                           {},
		"etc/emulationstation/es_systems.cfg": {Data: data},
		"roms/neshacks/homebrew.nes":          {},
		"roms/folder-personal/.keep":          {},
		"roms/snes/Game.sfc":                  {},
		"roms/vmu/save.vms":                   {},
		"roms/ports/start.sh":                 {},
	}
	catalog := systems.LibraryCatalog(root, "", "")
	for _, entry := range catalog {
		name := strings.TrimPrefix(entry.ResolvedPath, "/")
		if _, present := root[name]; !present {
			root[name] = &fstest.MapFile{Mode: fs.ModeDir | 0755}
		}
	}
	lstat := func(name string) (fs.FileInfo, error) {
		return fs.Stat(root, strings.TrimPrefix(name, "/"))
	}
	storage := diagnostic.CollectStorage(root, nil)
	report := library.Collect(root, storage, lstat, "", catalog...)
	byPath := make(map[string]library.System)
	for _, system := range report.Systems {
		byPath[system.Paths[0]] = system
	}
	families := map[string]string{
		"nintendo": "famicom fds gameandwatch gb gba gbc n64 n64dd nds nes pokemonmini satellaview sfc sgb snes snes-hacks snesmsu1 sufami virtualboy",
		"sega":     "atomiswave dreamcast gamegear genesis mastersystem megadrive naomi saturn sega32x segacd sg-1000 vmu",
		"sony":     "psx", "atari": "atarijaguar", "arcade": "mame fbneo alg neogeo neogeocd", "computers": "amiga amigacd32",
		"engines": "doom", "software": "pico-8 j2me", "support": "movies bios",
		"unidentified": "neshacks folder-personal snes-pessoal msumd pico",
	}
	for family, names := range families {
		for _, name := range strings.Fields(names) {
			system := byPath[path.Join("/roms", name)]
			if system.Family != family || system.Type == "" {
				t.Errorf("%s: got %q/%q, want family %q", name, system.Family, system.Type, family)
			}
		}
	}
	super, sufami := byPath["/roms/snes"], byPath["/roms/sufami"]
	if len(super.CatalogEntries) != 1 || len(sufami.CatalogEntries) != 1 ||
		super.CatalogEntries[0].Name != "snes" || sufami.CatalogEntries[0].Name != "snes" ||
		super.DisplayName != "Super NES" || sufami.DisplayName != "SuFami Turbo" ||
		super.CatalogEntries[0].Extension != ".sfc .SFC .zip" || super.CatalogEntries[0].Path != "/roms/snes/" {
		t.Fatalf("duplicate IDs lost: %+v, %+v", super, sufami)
	}
	if super.Counts["games"] != 1 || !reflect.DeepEqual(sufami.EmptyFolders, []string{"/roms/sufami"}) ||
		byPath["/roms/neshacks"].Counts["games"] != 1 ||
		len(report.Ports) != 1 || report.Ports[0].LauncherCount != 1 {
		t.Error("family changed inventory counts or emptied a nonempty folder")
	}
	if byPath["/roms2/genesis"].Family != "unidentified" {
		t.Error("standard folder or ID overrode contradictory display name")
	}
	if byPath["/roms/naomi"].Type != "arcade" || byPath["/roms/gamegear"].Type != "handheld" ||
		byPath["/roms/vmu"].Type != "auxiliary" || byPath["/roms/movies"].Type != "media" {
		t.Error("hardware type missing")
	}
	var text strings.Builder
	if err := presentation.WriteLibrary(&text, report); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"\nNintendo\n", "\nSega\n", "Super NES", "conteúdo auxiliar", "\nNão identificados\n"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("missing terminal output %q", want)
		}
	}
	text.Reset()
	if err := presentation.WriteLibrary(&text, library.Collect(root, storage, lstat, "sufami", catalog...)); err != nil ||
		!strings.Contains(text.String(), "SuFami Turbo") || !strings.Contains(text.String(), "Pasta vazia") {
		t.Errorf("filtered empty folder: %s (%v)", text.String(), err)
	}
	encoded, err := json.Marshal(report)
	if err != nil || !strings.Contains(string(encoded), `"family":"nintendo","type":"console"`) {
		t.Errorf("family/type missing in JSON: %v", err)
	}
}
