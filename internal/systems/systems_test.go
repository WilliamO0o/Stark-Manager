package systems_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WilliamO0o/Stark-Manager/internal/diagnostic"
	"github.com/WilliamO0o/Stark-Manager/internal/presentation"
	"github.com/WilliamO0o/Stark-Manager/internal/systems"
)

const config = "etc/emulationstation/es_systems.cfg"

func TestSystemsCompareCatalog(t *testing.T) {
	root := fixture()
	put(root, config, `<?xml version="1.0"?>
<systemList>
  <system><name>snes</name><fullname>SuFami Turbo</fullname><path>/roms/snes</path><extension>.st .ST</extension></system>
  <system><name>snes</name><fullname>Super NES</fullname><path>/roms/snes</path><extension>.sfc .zip</extension></system>
  <system><name>ports</name><fullname>Ports</fullname><path>/roms/ports</path><extension>.sh</extension><command>touch /unexpected</command></system>
  <system><name>pico-8</name><path>/roms/pico-8/carts</path><extension>.p8</extension></system>
  <system><name>missing</name><path>/roms/missing</path></system>
  <system><name>offline</name><path>/roms2/gb</path></system>
</systemList>`)
	report := collect(root)
	if report.CatalogStatus != "ok" || len(report.Catalogs) != 1 ||
		report.Catalogs[0].Path != "/"+config || !report.Catalogs[0].Read {
		t.Fatalf("catalog: %+v", report.Catalogs)
	}
	records := report.Catalogs[0].Systems
	if len(records) != 6 || records[0].Name != "snes" || records[1].Name != "snes" ||
		records[0].FullName != "SuFami Turbo" || records[1].FullName != "Super NES" ||
		records[0].Extension != ".st .ST" || records[1].Extension != ".sfc .zip" ||
		records[2].Name != "ports" || records[2].Extension != ".sh" {
		t.Fatalf("entries lost or collapsed: %+v", records)
	}
	if records[3].Location.Status != "present" || records[3].Location.Path != "/roms/pico-8/carts" ||
		records[4].Location.Status != "missing" || records[5].Location.Status != "not_examined" {
		t.Errorf("unexpected path comparisons: %+v", records[3:])
	}
	if got := records[3].Location.Binds; len(got) != 1 || got[0].Source != "/roms/pico-source" ||
		got[0].Target != "/roms/pico-8/carts" || got[0].Status != diagnostic.BindCompatible {
		t.Errorf("proven bind missing: %+v", got)
	}
	if folderStatus(report, "/roms/pico-8") != "declared" ||
		folderStatus(report, "/roms/pessoal") != "undeclared" ||
		folderStatus(report, "/roms/ports") != "declared" {
		t.Errorf("bad folder statuses: %+v", report.Folders)
	}
	first, err := json.Marshal(report)
	if err != nil || !json.Valid(first) || bytes.Contains(first, []byte("touch /unexpected")) {
		t.Fatalf("invalid JSON or leaked command: %s (%v)", first, err)
	}
	second, err := json.Marshal(collect(root))
	if err != nil || !bytes.Equal(first, second) {
		t.Fatal("JSON order changed on the same fixture")
	}
	var text strings.Builder
	if err := presentation.WriteSystems(&text, report); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/" + config, "Bind compatível", "Pasta sem caminho declarado",
		"caminho configurado ausente", "origem não examinada"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("missing %q in %s", want, text.String())
		}
	}
}

func TestSystemsUnavailableOrAmbiguousCatalog(t *testing.T) {
	for _, test := range []struct{ name, contents, want string }{
		{"missing", "", "missing"},
		{"invalid", "<systemList><system>", "invalid"},
		{"multiple XML roots", "<systemList/><systemList/>", "invalid"},
		{"multiple files", "<systemList/>", "ambiguous"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture()
			if test.contents != "" {
				put(root, config, test.contents)
			}
			if test.want == "ambiguous" {
				put(root, "home/test/.emulationstation/es_systems.cfg", "<systemList/>")
			}
			report := collect(root)
			if report.CatalogStatus != test.want || len(report.Limitations) == 0 ||
				folderStatus(report, "/roms/pessoal") != "catalog_indeterminate" {
				t.Fatalf("unjustified catalog comparison: %+v", report)
			}
			if test.want == "ambiguous" && (len(report.Catalogs) != 2 ||
				!report.Catalogs[0].Read || !report.Catalogs[1].Read) {
				t.Errorf("candidates not reported: %+v", report.Catalogs)
			}
		})
	}
}

func fixture() fstest.MapFS {
	root := fstest.MapFS{}
	put(root, "proc/self/mountinfo", `1 1 8:1 / / rw - ext4 /dev/sda1 rw
2 1 179:1 / /roms rw - ext4 /dev/mmcblk0p1 rw
3 2 179:1 /pico-source /roms/pico-8/carts rw - ext4 /dev/mmcblk0p1 rw
`)
	put(root, "etc/fstab", "/roms/pico-source /roms/pico-8/carts none bind\n/roms/pico-source /roms2 none bind,noauto\n")
	for _, dir := range []string{"roms/snes", "roms/ports", "roms/pico-8/carts", "roms/pico-source", "roms/pessoal", "roms2"} {
		root[dir] = &fstest.MapFile{Mode: fs.ModeDir | 0755}
	}
	for name, data := range map[string]string{
		"mmcblk0/dev": "179:0", "mmcblk0/ro": "0", "mmcblk0/size": "100",
		"mmcblk0/mmcblk0p1/dev": "179:1", "mmcblk0p1/dev": "179:1",
		"mmcblk0p1/partition": "1", "mmcblk0p1/ro": "0", "mmcblk0p1/size": "90",
	} {
		put(root, "sys/class/block/"+name, data)
	}
	return root
}

func put(root fstest.MapFS, name, contents string) {
	root[name] = &fstest.MapFile{Data: []byte(contents)}
}

func collect(root fstest.MapFS) systems.Report {
	lstat := func(name string) (fs.FileInfo, error) {
		if name == "/" {
			return fs.Stat(root, ".")
		}
		return fs.Stat(root, strings.TrimPrefix(name, "/"))
	}
	return systems.Collect(root, diagnostic.CollectStorage(root, lstat), lstat, "/home/test", "")
}

func folderStatus(report systems.Report, name string) string {
	for _, folder := range report.Folders {
		if folder.Path == name {
			return folder.Assessment.Status
		}
	}
	return ""
}
