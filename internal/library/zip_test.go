package library_test

import (
	"archive/zip"
	"bytes"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLibrarySNESZIP(t *testing.T) {
	root := fstest.MapFS{
		"proc/self/mountinfo":   {Data: []byte("1 1 8:1 / / rw - ext4 /dev/sda1 rw\n")},
		"etc/fstab":             {},
		"roms/snes/Mario.zip":   {Data: zipFixture(t, "Mario.sfc", "bonus/Alternate.SMC")},
		"roms/snes/Mario.srm":   {},
		"roms/snes/Mario.state": {},
		"roms/snes/Docs.zip":    {Data: zipFixture(t, "readme.txt", "directory.sfc/")},
		"roms/snes/Broken.zip":  {Data: []byte("PK\x03\x04truncated")},
		"roms/snes/Z-Other.sfc": {},
		"roms/nes/NotSNES.zip":  {Data: zipFixture(t, "Mario.sfc")},
	}
	report := collect(root, root, "")
	if len(report.Systems) != 2 {
		t.Fatalf("systems: %+v", report.Systems)
	}
	nes, snes := report.Systems[0], report.Systems[1]
	if nes.Counts["games"] != 1 || len(nes.ROMChecks) != 1 || nes.ROMChecks[0].Comparison != "possible_wrong_folder" {
		t.Errorf("SNES ZIP in NES was not diagnosed: %+v", nes)
	}
	if snes.Counts["games"] != 2 || snes.Counts["unknown"] != 2 ||
		snes.Counts["saves"] != 1 || snes.Counts["states"] != 1 || len(snes.Games) != 2 {
		t.Fatalf("counts or games incorrect: %+v", snes)
	}
	mario := snes.Games[0]
	if mario.Path != "/roms/snes/Mario.zip" || len(mario.Related) != 2 ||
		mario.Related[0].Path != "/roms/snes/Mario.srm" || mario.Related[1].Path != "/roms/snes/Mario.state" ||
		snes.Games[1].Path != "/roms/snes/Z-Other.sfc" {
		t.Errorf("wrong games or associations: %+v", snes.Games)
	}
	if len(report.Pending) != 1 || report.Pending[0].Path != "/roms/snes/Broken.zip" ||
		!strings.Contains(report.Pending[0].Reason, "índice ZIP") {
		t.Errorf("missing ZIP limitation: %+v", report.Pending)
	}

	root["roms/snes/Mario.sfc"] = &fstest.MapFile{}
	snes = collect(root, root, "snes").Systems[0]
	if !reflect.DeepEqual(snes.Unassociated, mario.Related) {
		t.Errorf("ambiguous associations should remain open: %+v", snes.Unassociated)
	}
}

func zipFixture(t *testing.T, entries ...string) []byte {
	t.Helper()
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for _, name := range entries {
		if _, err := writer.Create(name); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}
