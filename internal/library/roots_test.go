package library_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WilliamO0o/Stark-Manager/internal/diagnostic"
	"github.com/WilliamO0o/Stark-Manager/internal/library"
	"github.com/WilliamO0o/Stark-Manager/internal/presentation"
)

type unreadableDirectory struct {
	fs.FS
	name string
}

func (f unreadableDirectory) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == f.name {
		return nil, fs.ErrPermission
	}
	return fs.ReadDir(f.FS, name)
}

func TestLocalBackupSources(t *testing.T) {
	base := t.TempDir()
	a, b := filepath.Join(base, "Backup com espaços", "EASYROMS"), filepath.Join(base, "EEROMS")
	for name, data := range map[string][]byte{
		filepath.Join(a, "gb", "nested", "game.zip"):  zipFixture(t, "first.gb"),
		filepath.Join(b, "gb", "nested", "game.zip"):  zipFixture(t, "other.smc"),
		filepath.Join(a, "gb", "locked", "hidden.gb"): {},
	} {
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	storage := diagnostic.CollectStorage(fstest.MapFS{
		"proc/self/mountinfo": {Data: []byte("1 1 8:1 / / rw - ext4 /dev/sda1 rw\n")},
		"etc/fstab":           {},
	}, nil)
	cache := library.Cache{Dir: filepath.Join(base, "cache")}
	opts := library.UpdateOptions{
		Roots: []string{a + "/", b}, Cache: &cache,
		VolumeID: func(*diagnostic.Mount) string { return "fixture-shared-volume" },
	}
	report := library.Update(os.DirFS("/"), storage, os.Lstat, "gb", opts)
	if len(report.LocalSources) != 2 || report.LocalSources[0].OriginalPath != a+"/" ||
		len(report.Systems) != 2 || report.Systems[0].LocalSource.Name != "EASYROMS" {
		t.Fatalf("missing or merged sources: %+v", report)
	}
	if report.Systems[0].Counts["games"] != 2 || report.Systems[1].Counts["games"] != 1 ||
		report.Systems[1].ROMChecks[0].Comparison != library.ROMWrongFolder || report.Systems[0].Partition != nil {
		t.Fatalf("incorrect backup inventory: %+v", report.Systems)
	}
	saved, issues := cache.Snapshots("gb")
	if len(saved) != 2 || len(issues) != 0 || saved[0].LocalSource.Path == saved[1].LocalSource.Path {
		t.Fatalf("cache origins were merged: %+v, %v", saved, issues)
	}
	var cached int
	opts.Progress = func(event library.Event) {
		if event.Phase == "cached" {
			cached++
		}
	}
	first, _ := json.Marshal(report)
	second, _ := json.Marshal(library.Update(os.DirFS("/"), storage, os.Lstat, "gb", opts))
	if !bytes.Equal(first, second) || cached != 2 {
		t.Fatalf("repeat changed report or did not find both snapshots; cached=%d", cached)
	}
	var out strings.Builder
	if err := presentation.WriteLibrary(&out, report); err != nil ||
		!strings.Contains(out.String(), "Backup local \"EASYROMS\"") || !strings.Contains(out.String(), "Backup com espaços") {
		t.Fatalf("backup names not shown: %s (%v)", out.String(), err)
	}

	if err := os.Remove(filepath.Join(b, "gb", "nested", "game.zip")); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(base, "missing")
	opts.Roots = append(opts.Roots, missing)
	partial := library.Update(unreadableDirectory{os.DirFS("/"), strings.TrimPrefix(filepath.Join(a, "gb", "locked"), "/")}, storage, os.Lstat, "gb", opts)
	if len(partial.Pending) != 2 || partial.Pending[0].Path != missing ||
		partial.Systems[0].Counts["games"] != 1 || partial.Systems[1].Counts["games"] != 0 {
		t.Fatalf("partial results lost or deleted file retained: %+v", partial)
	}
	for _, snapshot := range mustSnapshots(t, cache) {
		if snapshot.LocalSource.Path == a && snapshot.Systems[0].Counts["games"] != 2 {
			t.Fatal("partial read replaced the last complete backup snapshot")
		}
	}
}

func TestLocalBackupWithoutMountAndCacheInsideRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "gb"), 0700); err != nil {
		t.Fatal(err)
	}
	cache := library.Cache{Dir: filepath.Join(root, "cache")}
	report := library.Update(os.DirFS("/"), diagnostic.StorageReport{}, os.Lstat, "", library.UpdateOptions{
		Roots: []string{root}, Cache: &cache,
	})
	if len(report.Systems) != 1 || report.Systems[0].Partition != nil || report.Systems[0].Device != nil ||
		len(report.Limitations) != 1 || !strings.Contains(report.Limitations[0], "cache") {
		t.Fatalf("backup needs no mount, and cache exclusion must be reported: %+v", report)
	}
	if _, err := os.Stat(cache.Dir); !os.IsNotExist(err) {
		t.Fatalf("cache was created in backup: %v", err)
	}
}

func mustSnapshots(t *testing.T, cache library.Cache) []library.Snapshot {
	t.Helper()
	saved, issues := cache.Snapshots("")
	if len(saved) != 2 || len(issues) != 0 {
		t.Fatalf("snapshots: %v, %v", saved, issues)
	}
	return saved
}
