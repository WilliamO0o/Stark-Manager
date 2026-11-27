package library_test

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestLibraryNestedSNESZIPs(t *testing.T) {
	for _, test := range []struct {
		fixture string
		games   int
		reasons []string
	}{
		{"solid", 1, nil},
		{"no-rom", 0, nil},
		{"broken", 0, []string{"bad.zip"}},
		{"mixed", 1, []string{"01-bad.zip"}},
		{"encrypted", 0, []string{"locked.zip", "criptografada"}},
		{"limits", 0, []string{"limite de tamanho", "limite de profundidade", "limite de entradas"}},
	} {
		t.Run(test.fixture, func(t *testing.T) {
			const base = "/roms/snes/Mr. Bloopy Saves the World (USA) (Proto)"
			root := fstest.MapFS{
				"proc/self/mountinfo":   {Data: []byte("1 1 8:1 / / rw - ext4 /dev/sda1 rw\n")},
				"etc/fstab":             {},
				base[1:] + ".7z":        {Data: read7zFixture(t, "nested-"+test.fixture+".7z")},
				base[1:] + ".srm":       {},
				"roms/snes/Z-Other.sfc": {},
			}
			report := collect(root, root, "snes")
			system := report.Systems[0]
			if system.Counts["games"] != test.games+1 || len(system.Games) != test.games+1 ||
				system.Games[len(system.Games)-1].Path != "/roms/snes/Z-Other.sfc" {
				t.Fatalf("expected one count per package and the remaining ROM: %+v", system)
			}
			if test.games == 1 {
				game := system.Games[0]
				if game.Path != base+".7z" || len(game.Related) != 1 || game.Related[0].Path != base+".srm" {
					t.Errorf("missing outer package or save association: %+v", game)
				}
			} else if len(system.UnknownFiles) != 1 || system.UnknownFiles[0] != base+".7z" {
				t.Errorf("unrecognized package missing from unknowns: %v", system.UnknownFiles)
			}
			if len(test.reasons) == 0 {
				if len(report.Pending) != 0 {
					t.Errorf("unexpected pending: %+v", report.Pending)
				}
				return
			}
			if len(report.Pending) != 1 || report.Pending[0].Path != base+".7z" {
				t.Fatalf("expected pending inspection on outer package: %+v", report.Pending)
			}
			for _, text := range test.reasons {
				if !strings.Contains(report.Pending[0].Reason, text) {
					t.Errorf("missing %q in %s", text, report.Pending[0].Reason)
				}
			}
		})
	}
}
