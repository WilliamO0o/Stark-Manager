package diagnostic

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
)

type StorageReport struct {
	SchemaVersion int                       `json:"schema_version"`
	Devices       Section[[]StorageDevice]  `json:"devices"`
	Mounts        Section[[]Mount]          `json:"mounts"`
	Configured    Section[[]ConfiguredBind] `json:"configured_binds"`
}

type StorageDevice struct {
	BlockDevice
	MajorMinor *string       `json:"major_minor"`
	ParentDisk *string       `json:"parent_disk"`
	Slot       *PhysicalSlot `json:"physical_slot"`
}

type PhysicalSlot struct {
	Name     string `json:"name"`
	Evidence string `json:"evidence"`
}

type Mount struct {
	ID             uint64   `json:"id"`
	ParentID       uint64   `json:"parent_id"`
	MajorMinor     string   `json:"major_minor"`
	Root           string   `json:"root"`
	Target         string   `json:"target"`
	Filesystem     string   `json:"filesystem"`
	MountSource    string   `json:"mount_source"`
	Options        []string `json:"options"`
	OptionalFields []string `json:"optional_fields"`
	SuperOptions   []string `json:"super_options"`
	Device         *string  `json:"device"`
	Partition      *string  `json:"partition"`
	Subtree        bool     `json:"subtree"`
}

type BindStatus string

const (
	BindCompatible    BindStatus = "compatible"
	BindNotObserved   BindStatus = "not_observed"
	BindDifferent     BindStatus = "different_origin"
	BindIndeterminate BindStatus = "indeterminate"
)

type ConfiguredBind struct {
	Line          int        `json:"line"`
	Source        string     `json:"source"`
	Target        string     `json:"target"`
	Options       []string   `json:"options"`
	Recursive     bool       `json:"recursive"`
	NoAuto        bool       `json:"noauto"`
	Status        BindStatus `json:"status"`
	Reason        string     `json:"reason"`
	ActiveMountID *uint64    `json:"active_mount_id"`
}

// CollectStorage reads the given Linux view. lstat inspects path components in
// the same namespace without following links. Nil leaves comparisons unknown.
func CollectStorage(root fs.FS, lstat func(string) (fs.FileInfo, error)) StorageReport {
	report := StorageReport{
		SchemaVersion: 1,
		Devices:       storageDevices(root),
		Mounts:        readRecords(root, "proc/self/mountinfo", newMountParser()),
		Configured:    readRecords(root, "etc/fstab", parseFstabBind),
	}
	var tree mountTree
	if report.Mounts.Data != nil {
		mounts := *report.Mounts.Data
		sort.Slice(mounts, func(i, j int) bool {
			if mounts[i].Target != mounts[j].Target {
				return mounts[i].Target < mounts[j].Target
			}
			return mounts[i].ID < mounts[j].ID
		})
		if report.Devices.Data != nil {
			linkMountDevices(mounts, *report.Devices.Data)
		}
		if report.Mounts.Status == "ok" {
			tree = buildMountTree(mounts)
			if tree.root == nil {
				report.Mounts = section(mounts, []string{"mountinfo: raiz da árvore de montagens ausente ou ambígua"})
			}
		}
	}
	if report.Configured.Data != nil {
		binds := *report.Configured.Data
		for i := range binds {
			compareBind(&binds[i], tree, lstat)
		}
	}
	return report
}

func storageDevices(root fs.FS) Section[[]StorageDevice] {
	const base = "sys/class/block"
	inventory := collectStorage(root)
	if inventory.Data == nil {
		return Section[[]StorageDevice]{Status: inventory.Status, Issues: inventory.Issues}
	}
	issues := inventory.Issues
	devices := make([]StorageDevice, 0, len(*inventory.Data))
	partitions := make(map[string]int)
	ids := make(map[string]int)
	for _, block := range *inventory.Data {
		device := StorageDevice{BlockDevice: block}
		devPath := path.Join(base, block.Name, "dev")
		raw, err := readText(root, devPath)
		if id, ok := majorMinor(raw); err != nil {
			issues = append(issues, err.Error())
		} else if ok {
			device.MajorMinor = &id
			ids[id]++
		} else {
			issues = append(issues, devPath+": major:minor inválido")
		}
		if block.Kind == "partition" {
			partitions[block.Name] = len(devices)
		}
		devices = append(devices, device)
	}

	// The kernel lists a partition below its parent in sysfs. Confirm the
	// child entry's dev number against the class entry before recording it.
	parents := make(map[string][]string)
	for _, device := range devices {
		if device.Kind != "device" || len(partitions) == 0 {
			continue
		}
		dir := path.Join(base, device.Name)
		children, err := fs.ReadDir(root, dir)
		if err != nil {
			issues = append(issues, err.Error())
			continue
		}
		for _, child := range children {
			index, exists := partitions[child.Name()]
			if !exists || !child.IsDir() || devices[index].MajorMinor == nil {
				continue
			}
			devPath := path.Join(dir, child.Name(), "dev")
			raw := textField(root, devPath, &issues)
			if id, ok := majorMinor(raw); ok && id == *devices[index].MajorMinor {
				parents[child.Name()] = append(parents[child.Name()], device.Name)
			}
		}
	}
	for i := range devices {
		device := &devices[i]
		if device.MajorMinor != nil && ids[*device.MajorMinor] > 1 {
			issues = append(issues, fmt.Sprintf("sysfs: major:minor duplicado para %s", device.Name))
			device.MajorMinor = nil
		}
		if device.Kind == "partition" {
			if names := parents[device.Name]; len(names) == 1 {
				device.ParentDisk = &names[0]
			} else {
				issues = append(issues, fmt.Sprintf("sysfs: disco pai não confirmado para %s", device.Name))
			}
		}
	}
	return section(devices, issues)
}

func linkMountDevices(mounts []Mount, devices []StorageDevice) {
	byID := make(map[string]StorageDevice, len(devices))
	for _, device := range devices {
		if device.MajorMinor != nil {
			byID[*device.MajorMinor] = device
		}
	}
	for i := range mounts {
		mount := &mounts[i]
		if mount.Filesystem == "nsfs" {
			continue
		}
		device, ok := byID[mount.MajorMinor]
		if !ok {
			continue
		}
		switch device.Kind {
		case "device":
			mount.Device = &device.Name
		case "partition":
			mount.Device = device.ParentDisk
			mount.Partition = &device.Name
		}
	}
}

// MountLookup returns the visible mount covering an absolute path, or nil
// when the collected topology cannot resolve the path unambiguously.
func (report StorageReport) MountLookup() func(string) *Mount {
	var tree mountTree
	if report.Mounts.Data != nil {
		tree = buildMountTree(*report.Mounts.Data)
	}
	return tree.visible
}
