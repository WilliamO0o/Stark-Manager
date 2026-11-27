package library_test

import (
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/WilliamO0o/Stark-Manager/internal/diagnostic"
	"github.com/WilliamO0o/Stark-Manager/internal/library"
	"github.com/WilliamO0o/Stark-Manager/internal/presentation"
)

type trackedLibrary struct {
	fstest.MapFS
	opens []string
	dirs  []string
	fail  string
}

func (root *trackedLibrary) Open(name string) (fs.File, error) {
	root.opens = append(root.opens, name)
	return root.MapFS.Open(name)
}

func (root *trackedLibrary) ReadDir(name string) ([]fs.DirEntry, error) {
	root.dirs = append(root.dirs, name)
	if name == root.fail {
		return nil, fs.ErrPermission
	}
	return root.MapFS.ReadDir(name)
}

func updateFixture(t *testing.T) *trackedLibrary {
	return &trackedLibrary{MapFS: fstest.MapFS{
		"proc/self/mountinfo":          {Data: []byte("1 1 8:1 / / rw - ext4 /dev/sda1 rw\n2 1 8:2 / /roms rw - ext4 /dev/sda2 rw\n")},
		"etc/fstab":                    {},
		"roms/atarilynx/unrelated.zip": {Data: zipFixture(t, "unused.gba")},
		"roms/gb/deep/foreign.smc":     {},
		"roms/gb/deep/pack.zip":        {Data: zipFixture(t, "game.gb")},
		"roms/gb/deep/added.sav":       {},
		"roms/gb/remove.png":           {},
		"roms/zz-last/Game.nes":        {},
	}}
}

func update(root *trackedLibrary, filter string, options library.UpdateOptions) library.Report {
	storage := diagnostic.CollectStorage(root, nil)
	lstat := func(name string) (fs.FileInfo, error) { return fs.Stat(root.MapFS, strings.TrimPrefix(name, "/")) }
	return library.Update(root, storage, lstat, filter, options)
}

func TestUpdateScopesReadsAndEmitsFolderImmediately(t *testing.T) {
	root := updateFixture(t)
	report := update(root, "gb", library.UpdateOptions{})
	if contains(root.opens, "roms/atarilynx/unrelated.zip") || contains(root.dirs, "roms/atarilynx") ||
		!contains(root.opens, "roms/gb/deep/pack.zip") {
		t.Fatalf("incorrect scope: opens %v, dirs %v", root.opens, root.dirs)
	}
	if len(report.Systems) != 1 || report.Systems[0].ROMChecks[0].Comparison != library.ROMWrongFolder {
		t.Fatalf("missing SNES warning in GB: %+v", report.Systems)
	}
	root.opens, root.dirs = nil, nil
	var completed []string
	update(root, "", library.UpdateOptions{Progress: func(event library.Event) {
		if event.Phase != "finished" {
			return
		}
		if len(completed) == 0 && (contains(root.dirs, "roms/gb") || event.Done != 1 || event.Total != 3 || event.Files != 1) {
			t.Errorf("first summary not delivered immediately: %+v; dirs %v", event, root.dirs)
		}
		completed = append(completed, event.Path)
	}})
	if !reflect.DeepEqual(completed, []string{"/roms/atarilynx", "/roms/gb", "/roms/zz-last"}) {
		t.Errorf("unstable folder order: %v", completed)
	}
}

func TestUpdateCacheReusesAndPreservesCompleteSnapshot(t *testing.T) {
	root := updateFixture(t)
	cache := library.Cache{Dir: filepath.Join(t.TempDir(), "cache")}
	id := "card-a"
	options := library.UpdateOptions{Cache: &cache, VolumeID: func(*diagnostic.Mount) string { return id }}
	update(root, "gb", options)
	baseline, _ := cache.Snapshots("gb")
	if len(baseline) != 1 {
		t.Fatalf("no snapshot: %+v", baseline)
	}

	root.opens = nil
	delete(root.MapFS, "roms/gb/remove.png")
	root.MapFS["roms/gb/deep/added.gb"] = &fstest.MapFile{}
	updated := update(root, "gb", options).Systems[0]
	if contains(root.opens, "roms/gb/deep/pack.zip") || updated.Counts["images"] != 0 || len(updated.Unassociated) != 0 {
		t.Fatalf("rescan did not reuse or reassociate: %v, %+v", root.opens, updated)
	}
	current, _ := cache.Snapshots("gb")
	if len(current) != 1 || len(current[0].Systems[0].Games[0].Related) != 1 {
		t.Fatalf("save not reassociated in snapshot: %+v", current)
	}
	warnings := presentation.ReadCacheReport(cache, "", true)
	if len(warnings.Coverage) != 1 || len(warnings.Warnings) != 1 ||
		warnings.Warnings[0].Path != "/roms/gb/deep/foreign.smc" || warnings.Warnings[0].ExaminedAt.IsZero() {
		t.Fatalf("offline warnings or coverage: %+v", warnings)
	}

	root.fail = "roms/gb/deep"
	incomplete := update(root, "gb", options)
	after, _ := cache.Snapshots("gb")
	if len(incomplete.Pending) == 0 || !reflect.DeepEqual(current, after) {
		t.Fatalf("partial read replaced completed snapshot: %+v", after)
	}
	root.fail = ""
	interrupt := options
	interrupt.Progress = func(e library.Event) {
		if e.Phase == "file" {
			panic("fixture interruption")
		}
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("fixture failed to interrupt")
			}
		}()
		update(root, "gb", interrupt)
	}()
	after, _ = cache.Snapshots("gb")
	if !reflect.DeepEqual(current, after) {
		t.Fatal("interruption replaced complete snapshot")
	}

	root.opens = nil
	root.MapFS["roms/gb/deep/pack.zip"].ModTime = time.Unix(60, 0)
	update(root, "gb", options)
	if !contains(root.opens, "roms/gb/deep/pack.zip") {
		t.Fatal("changed package was not reopened")
	}

	root.opens = nil
	id = "card-b"
	update(root, "gb", options)
	if !contains(root.opens, "roms/gb/deep/pack.zip") {
		t.Fatal("classification was reused across cards")
	}
	both, _ := cache.Snapshots("gb")
	if len(both) != 2 {
		t.Fatalf("separate origins not retained: %+v", both)
	}

	root.opens = nil
	options.Force = true
	update(root, "gb", options)
	if !contains(root.opens, "roms/gb/deep/pack.zip") {
		t.Fatal("force did not reopen the package")
	}

	options.Force = false
	id = ""
	update(root, "gb", options)
	root.opens = nil
	update(root, "gb", options)
	if !contains(root.opens, "roms/gb/deep/pack.zip") {
		t.Fatal("unidentified volume reused classifications")
	}
}
