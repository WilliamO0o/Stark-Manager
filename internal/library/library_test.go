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
)

func TestLibraryInventory(t *testing.T) {
	root := fixture()
	report := collect(root, root, "")
	if !reflect.DeepEqual(report.Examined, []string{"/media/card/roms", "/roms", "/roms2"}) {
		t.Fatalf("roots = %v", report.Examined)
	}
	if len(report.Systems) != 3 {
		t.Fatalf("systems = %+v", report.Systems)
	}
	if report.Systems[0].Name != "nes" || report.Systems[0].Counts["unknown"] != 1 {
		t.Errorf("NES should be inventoried without guessing games: %+v", report.Systems[0])
	}
	other, snes := report.Systems[1], report.Systems[2]
	if other.Partition == nil || *other.Partition != "mmcblk1p1" || other.Counts["games"] != 1 {
		t.Errorf("second card: %+v", other)
	}
	if snes.Partition == nil || *snes.Partition != "mmcblk0p1" ||
		!reflect.DeepEqual(snes.Paths, []string{"/roms/snes", "/roms2/snes"}) || snes.DuplicateFiles != 10 {
		t.Errorf("first card and bind: %+v", snes)
	}
	counts := map[string]int{"games": 1, "saves": 2, "states": 2, "images": 1, "videos": 1, "metadata": 1, "unknown": 2}
	if !reflect.DeepEqual(snes.Counts, counts) {
		t.Errorf("counts = %v; want %v", snes.Counts, counts)
	}
	if !reflect.DeepEqual(snes.EmptyFolders, []string{"/roms/snes/container/empty", "/roms/snes/empty"}) {
		t.Errorf("empty folders = %v", snes.EmptyFolders)
	}
	for _, name := range []string{"/roms/snes/container", "/roms/snes/hidden", "/roms/snes/media", "/roms/snes/saves"} {
		if !contains(snes.FoldersWithoutGames, name) {
			t.Errorf("missing folder with no identified games: %s (%v)", name, snes.FoldersWithoutGames)
		}
	}
	if len(snes.Games) != 1 || len(snes.Games[0].Related) != 5 || len(snes.Unassociated) != 1 ||
		snes.Unassociated[0].Path != "/roms/snes/saves/Other.sav" {
		t.Errorf("game associations = %+v; unmatched = %+v", snes.Games, snes.Unassociated)
	}
	if len(report.NotExamined) != 1 || report.NotExamined[0].Partition == nil ||
		*report.NotExamined[0].Partition != "mmcblk2p1" {
		t.Errorf("not examined = %+v", report.NotExamined)
	}
	first, err := json.Marshal(report)
	if err != nil || !json.Valid(first) {
		t.Fatalf("JSON = %s, %v", first, err)
	}
	second, err := json.Marshal(collect(root, root, ""))
	if err != nil || !bytes.Equal(first, second) {
		t.Fatal("same fixture should produce deterministic JSON")
	}

	filtered := collect(root, root, "SNES")
	if filtered.Filter != "snes" || !reflect.DeepEqual(filtered.Systems, report.Systems[1:]) {
		t.Errorf("filter changed counts or kept other systems: %+v", filtered.Systems)
	}
	if only := collect(root, root, "nes"); len(only.Systems) != 1 || only.Systems[0].Name != "nes" {
		t.Errorf("filter must accept systems without game rules: %+v", only.Systems)
	}
	var out bytes.Buffer
	if err := presentation.WriteLibrary(&out, report); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Sem jogos identificados", "Pastas vazias: 2", "mmcblk2/mmcblk2p1 — não examinado"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in terminal report:\n%s", want, out.String())
		}
	}
}

func TestLibraryKeepsPartialReadsAndStopsBindCycle(t *testing.T) {
	root := fixture()
	root["roms/snes/cycle"] = &fstest.MapFile{Mode: fs.ModeDir | 0755}
	root["proc/self/mountinfo"].Data = append(root["proc/self/mountinfo"].Data,
		[]byte("6 2 179:1 /snes /roms/snes/cycle rw - ext4 /dev/mmcblk0p1 rw\n")...)
	report := collect(partialFS{root}, root, "snes")
	for _, name := range []string{"/roms/snes/cycle", "/roms/snes/hidden"} {
		found := false
		for _, item := range report.Pending {
			found = found || item.Path == name
		}
		if !found {
			t.Errorf("missing pending path %s: %+v", name, report.Pending)
		}
	}
	if report.Systems[1].Counts["games"] != 1 || report.Systems[1].Counts["unknown"] != 2 ||
		contains(report.Systems[1].EmptyFolders, "/roms/snes/hidden") {
		t.Errorf("partial read lost valid entries or reported false emptiness: %+v", report.Systems[1])
	}
}

func TestLibraryLeavesAmbiguousSavesUnassociated(t *testing.T) {
	root := fixture()
	root["roms/snes/alternate/Mario.smc"] = &fstest.MapFile{}
	root["roms2/snes/alternate/Mario.smc"] = &fstest.MapFile{}
	report := collect(root, root, "snes")
	snes := report.Systems[1]
	if len(snes.Games) != 2 || len(snes.Unassociated) != 6 {
		t.Fatalf("ambiguous associations: %+v", snes)
	}
	for _, game := range snes.Games {
		if len(game.Related) != 0 {
			t.Errorf("ambiguous files associated with %s: %+v", game.Path, game.Related)
		}
	}
}

type partialFS struct{ fstest.MapFS }

func (root partialFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := root.MapFS.ReadDir(name)
	if name == "roms/snes/hidden" {
		return entries, fs.ErrPermission
	}
	return entries, err
}

func fixture() fstest.MapFS {
	root := fstest.MapFS{}
	put := func(name, value string) { root[name] = &fstest.MapFile{Data: []byte(value)} }
	put("proc/self/mountinfo", `1 1 8:1 / / rw - ext4 /dev/sda1 rw
2 1 179:1 / /roms rw - ext4 /dev/mmcblk0p1 rw
3 1 179:33 / /media/card rw - ext4 /dev/mmcblk1p1 rw
4 1 179:1 / /roms2 rw - ext4 /dev/mmcblk0p1 rw
`)
	put("etc/fstab", "")
	for _, card := range []struct{ name, disk, partition string }{
		{"mmcblk0", "179:0", "179:1"},
		{"mmcblk1", "179:32", "179:33"},
		{"mmcblk2", "179:64", "179:65"},
	} {
		disk := "sys/class/block/" + card.name
		part := disk + "p1"
		put(disk+"/dev", card.disk)
		put(disk+"/size", "100000")
		put(disk+"/ro", "0")
		put(disk+"/device/type", "SD")
		put(part+"/dev", card.partition)
		put(part+"/size", "90000")
		put(part+"/ro", "0")
		put(part+"/partition", "1")
		put(disk+"/"+card.name+"p1/dev", card.partition)
	}
	for _, name := range []string{
		"roms/snes/Mario.sfc", "roms/snes/Mario.srm", "roms/snes/Mario.state.auto",
		"roms/snes/Mario.state1", "roms/snes/media/Mario-image.png", "roms/snes/media/Mario.mp4",
		"roms/snes/gamelist.xml", "roms/snes/unknown.bin", "roms/snes/hidden/.keep",
		"roms/snes/saves/Other.sav", "roms/nes/Game.nes", "roms/tools/tool.sh",
		"media/card/roms/snes/Other.smc",
	} {
		put(name, "")
		if strings.HasPrefix(name, "roms/") {
			put("roms2/"+strings.TrimPrefix(name, "roms/"), "")
		}
	}
	for _, base := range []string{"roms", "roms2"} {
		for _, dir := range []string{"snes/empty", "snes/container/empty"} {
			root[base+"/"+dir] = &fstest.MapFile{Mode: fs.ModeDir | 0755}
		}
	}
	return root
}

func collect(root fs.FS, metadata fstest.MapFS, filter string) library.Report {
	return library.Collect(root, diagnostic.CollectStorage(root, nil), func(name string) (fs.FileInfo, error) {
		return fs.Stat(metadata, strings.TrimPrefix(name, "/"))
	}, filter)
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
