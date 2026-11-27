package diagnostic_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WilliamO0o/Stark-Manager/internal/diagnostic"
	"github.com/WilliamO0o/Stark-Manager/internal/presentation"
)

func TestStorageK36(t *testing.T) {
	root := fstest.MapFS{}
	addBlock(root, "mmcblk0", "179:0", "67108864", "")
	addBlock(root, "mmcblk0p1", "179:1", "2097152", "mmcblk0")
	addBlock(root, "mmcblk1", "179:32", "134217728", "")
	addBlock(root, "mmcblk1p1", "179:33", "104857600", "mmcblk1")
	addBlock(root, "mmcblk1p2", "179:34", "8388608", "mmcblk1")
	addDirs(root, "/storage", "/storage/ROM Files", "/mirror", "/mirror/nes",
		"/games", "/manual", "/wrong", "/subvolume", "/save copy")
	put(root, "proc/self/mountinfo", `13 1 179:33 /ROM\040Files /mirror rw shared:7 future:field - ext4 /dev/mmcblk1p1 rw
1 90 179:1 / / rw - ext4 /dev/mmcblk0p1 rw
17 1 179:33 /saves /save\040copy rw - ext4 /dev/mmcblk1p1 rw
11 1 179:33 / /storage rw - ext4 /dev/mmcblk1p1 rw
14 1 179:33 /ROM\040Files/nes /games rw - ext4 /dev/mmcblk1p1 rw
15 1 179:1 / /wrong rw - ext4 /dev/mmcblk0p1 rw
16 1 0:29 /@ /subvolume rw - btrfs /dev/mmcblk1p1 rw
18 1 0:4 mnt:[4026532918] /run/snapd/ns/firefox.mnt rw - nsfs nsfs rw
19 1 0:4 mnt:[4026532921] /run/snapd/ns/snap-store.mnt rw - nsfs nsfs rw
`)
	put(root, "etc/fstab", `# K36 simulated
/dev/mmcblk0p1 / ext4 defaults 0 1
/storage/ROM\040Files /mirror none bind 0 0
/mirror/nes /games none rbind 0 0
/storage/ROM\040Files /manual none bind,noauto 0 0
/storage/ROM\040Files /wrong none bind 0 0
`)
	report := collectFixture(root)
	if report.Devices.Status != "ok" || report.Mounts.Status != "ok" || report.Configured.Status != "ok" {
		t.Fatalf("unexpected source issues: %+v %+v %+v", report.Devices, report.Mounts, report.Configured)
	}
	devices := *report.Devices.Data
	if len(devices) != 5 {
		t.Fatalf("devices: %+v", devices)
	}
	for _, device := range devices {
		if device.Slot != nil {
			t.Fatalf("slot guessed: %+v", device)
		}
		if device.Name == "mmcblk1p2" && (device.ParentDisk == nil || *device.ParentDisk != "mmcblk1") {
			t.Fatalf("unmounted partition lost its parent: %+v", device)
		}
	}

	byTarget := map[string]diagnostic.Mount{}
	for _, mount := range *report.Mounts.Data {
		byTarget[mount.Target] = mount
	}
	for _, target := range []string{"/storage", "/mirror", "/games", "/save copy"} {
		mount := byTarget[target]
		if mount.Partition == nil || *mount.Partition != "mmcblk1p1" || mount.Device == nil || *mount.Device != "mmcblk1" {
			t.Errorf("mount not linked to partition and disk: %+v", mount)
		}
	}
	if m := byTarget["/mirror"]; m.Root != "/ROM Files" || len(m.OptionalFields) != 2 || m.OptionalFields[1] != "future:field" {
		t.Errorf("mount did not preserve escape or optional fields: %+v", m)
	}
	if m := byTarget["/subvolume"]; !m.Subtree || m.Partition != nil || m.Root != "/@" {
		t.Errorf("subvolume inference exceeded available evidence: %+v", m)
	}
	for target, root := range map[string]string{
		"/run/snapd/ns/firefox.mnt":    "mnt:[4026532918]",
		"/run/snapd/ns/snap-store.mnt": "mnt:[4026532921]",
	} {
		m := byTarget[target]
		if m.Filesystem != "nsfs" || m.Root != root || m.Subtree || m.Device != nil || m.Partition != nil {
			t.Errorf("invalid namespace mount for %s: %+v", target, m)
		}
	}
	want := []diagnostic.BindStatus{diagnostic.BindCompatible, diagnostic.BindCompatible, diagnostic.BindNotObserved, diagnostic.BindDifferent}
	binds := *report.Configured.Data
	if len(binds) != len(want) {
		t.Fatalf("binds: %+v", binds)
	}
	for i, status := range want {
		if binds[i].Status != status {
			t.Errorf("bind %d: got %+v, want %s", i, binds[i], status)
		}
	}
	if !binds[1].Recursive || !binds[2].NoAuto || binds[2].ActiveMountID != nil || binds[0].Source != "/storage/ROM Files" {
		t.Errorf("bind flags, escaped path or active IDs are wrong: %+v", binds)
	}
	encoded, err := json.Marshal(report)
	if err != nil || !json.Valid(encoded) || !strings.Contains(string(encoded), `"physical_slot":null`) {
		t.Fatalf("invalid JSON or missing explicit null slot: %s, %v", encoded, err)
	}
	again, err := json.Marshal(collectFixture(root))
	if err != nil || !bytes.Equal(encoded, again) {
		t.Fatalf("unstable JSON: %v", err)
	}

	var output bytes.Buffer
	if err := presentation.WriteStorage(&output, report); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, want := range []string{"slot físico: desconhecido", `→ "/save copy"`, `→ "/subvolume"`,
		"origem de eventual bind indeterminada", "Partição mmcblk1p2", "Sem montagem vinculada.",
		"compatível com a montagem ativa", "sem montagem observada", "destino ocupado por outra origem"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Count(text, "50 GiB") != 1 {
		t.Errorf("partition capacity should appear once:\n%s", text)
	}
	t.Logf("K36 simulado:\n%s", text)
}

func TestStorageDesktopAndMissingSources(t *testing.T) {
	root := fstest.MapFS{}
	addBlock(root, "nvme0n1", "259:0", "100000", "")
	addBlock(root, "nvme0n1p1", "259:1", "50000", "nvme0n1")
	addBlock(root, "sda", "invalid", "unknown", "")
	put(root, "proc/self/mountinfo", "1 1 259:1 / / rw - ext4 /dev/nvme0n1p1 rw\n"+
		"malformed\n2 1 x:y / /bad rw - ext4 none rw\n"+
		"3 1 0:8 /bad\\777 /bad rw - tmpfs none rw\n")
	put(root, "etc/fstab", "bad line\n/home /mirror none bind\n")
	report := collectFixture(root)
	if report.Devices.Status != "partial" || len(report.Devices.Issues) != 2 ||
		(*report.Devices.Data)[2].MajorMinor != nil {
		t.Fatalf("invalid sysfs data should be reported without discarding other devices: %+v", report.Devices)
	}
	if report.Mounts.Status != "partial" || len(report.Mounts.Issues) != 3 || len(*report.Mounts.Data) != 1 {
		t.Fatalf("mount data or limitations lost: %+v", report.Mounts)
	}
	if report.Configured.Status != "partial" || (*report.Configured.Data)[0].Status != diagnostic.BindIndeterminate {
		t.Fatalf("configuration should still appear, with an indeterminate comparison: %+v", report.Configured)
	}
	mount := (*report.Mounts.Data)[0]
	if mount.Partition == nil || *mount.Partition != "nvme0n1p1" || (*report.Devices.Data)[0].Slot != nil {
		t.Fatalf("desktop partition or unknown slot missing: %+v", mount)
	}

	missing := collectFixture(fstest.MapFS{"etc/fstab": {Data: []byte("/origin /target none bind 0 0")}})
	if missing.Devices.Status != "unavailable" || missing.Mounts.Status != "unavailable" ||
		(*missing.Configured.Data)[0].Status != diagnostic.BindIndeterminate {
		t.Fatalf("missing sources: %+v", missing)
	}
	empty := collectFixture(fstest.MapFS{})
	if empty.Configured.Status != "unavailable" || len(empty.Configured.Issues) == 0 {
		t.Fatalf("missing fstab: %+v", empty.Configured)
	}
}

func TestStorageDoesNotInferThroughHiddenMounts(t *testing.T) {
	root := fstest.MapFS{}
	addDirs(root, "/data", "/data/nested", "/target", "/auto", "/clone", "/sym")
	put(root, "proc/self/mountinfo", `1 1 8:1 / / rw - ext4 /dev/sda1 rw
2 1 8:2 / /data rw - ext4 /dev/sda2 rw
3 2 8:3 / /data/nested rw - ext4 /dev/sda3 rw
4 2 8:4 / /data rw - ext4 /dev/sda4 rw
5 1 8:4 /nested /target rw - ext4 /dev/sda4 rw
6 1 0:23 / /auto rw - autofs systemd rw
7 1 8:4 / /clone rw - ext4 /dev/sda4 rw
`)
	put(root, "etc/fstab", `/data/nested /target none bind
/auto/source /target none bind
/sym/source /target none bind
/data /clone none rbind
`)
	report := diagnostic.CollectStorage(root, func(name string) (fs.FileInfo, error) {
		if strings.HasPrefix(name, "/auto") || strings.HasPrefix(name, "/sym/") {
			t.Fatalf("unexpected metadata access through %q", name)
		}
		if name == "/sym" {
			info, err := fs.Stat(root, "sym")
			return symlinkInfo{info}, err
		}
		return fs.Stat(root, fixturePath(name))
	})
	want := []diagnostic.BindStatus{diagnostic.BindCompatible, diagnostic.BindIndeterminate,
		diagnostic.BindIndeterminate, diagnostic.BindIndeterminate}
	for i, bind := range *report.Configured.Data {
		if bind.Status != want[i] {
			t.Errorf("bind %d: got %+v, want %s", i, bind, want[i])
		}
	}
}

type symlinkInfo struct{ fs.FileInfo }

func (symlinkInfo) Mode() fs.FileMode { return fs.ModeSymlink }

func collectFixture(root fstest.MapFS) diagnostic.StorageReport {
	return diagnostic.CollectStorage(root, func(name string) (fs.FileInfo, error) {
		return fs.Stat(root, fixturePath(name))
	})
}

func addBlock(root fstest.MapFS, name, id, sectors, parent string) {
	base := "sys/class/block/" + name + "/"
	put(root, base+"dev", id+"\n")
	put(root, base+"size", sectors+"\n")
	put(root, base+"ro", "0\n")
	if parent != "" {
		put(root, base+"partition", "1\n")
		put(root, "sys/class/block/"+parent+"/"+name+"/dev", id+"\n")
	}
}

func addDirs(root fstest.MapFS, dirs ...string) {
	for _, dir := range dirs {
		root[strings.TrimPrefix(dir, "/")] = &fstest.MapFile{Mode: fs.ModeDir | 0755}
	}
}

func put(root fstest.MapFS, name, data string) {
	root[name] = &fstest.MapFile{Data: []byte(data)}
}

func fixturePath(name string) string {
	if name == "/" {
		return "."
	}
	return strings.TrimPrefix(name, "/")
}
