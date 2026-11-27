package library

import (
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/WilliamO0o/Stark-Manager/internal/diagnostic"
)

type RootCandidate struct {
	Path     string
	Observed bool
}

func romRoot(name string) bool {
	suffix, ok := strings.CutPrefix(strings.ToLower(name), "roms")
	return ok && strings.Trim(suffix, "0123456789") == ""
}

func auxiliary(name string) bool {
	switch name {
	case "tools", "ports", "bios", "saves", "savestates", "states", "images", "media",
		"videos", "covers", "screenshots", "themes", "metadata", "downloads", "lost+found",
		"backup", "backups", "bezels", "downloaded_images", "downloaded_media", "downloaded_videos":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// DiscoverRoots returns conventional ROM directories to check in the mount namespace.
func DiscoverRoots(storage diagnostic.StorageReport, mountAt func(string) *diagnostic.Mount) []RootCandidate {
	candidates := map[string]bool{"/roms": false, "/roms2": false}
	if storage.Mounts.Data != nil {
		for _, mount := range *storage.Mounts.Data {
			visible := mountAt(mount.Target)
			if visible == nil || visible.ID != mount.ID {
				continue
			}
			if romRoot(path.Base(mount.Target)) {
				candidates[mount.Target] = true
			}
			if (mount.Device != nil || mount.Partition != nil) && !romRoot(path.Base(mount.Target)) {
				for _, folder := range []string{"roms", "roms2"} {
					name := path.Join(mount.Target, folder)
					if _, exists := candidates[name]; !exists {
						candidates[name] = false
					}
				}
			}
		}
	}
	result := make([]RootCandidate, 0, len(candidates))
	for name, observed := range candidates {
		result = append(result, RootCandidate{name, observed})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}

// UnexaminedCards identifies detected card storage without a linked visible mount.
func UnexaminedCards(root fs.FS, storage diagnostic.StorageReport) []NotExamined {
	result := []NotExamined{}
	if storage.Devices.Data == nil {
		return result
	}
	mounted := make(map[string]bool)
	mountAt := storage.MountLookup()
	if storage.Mounts.Data != nil {
		for _, mount := range *storage.Mounts.Data {
			if visible := mountAt(mount.Target); visible == nil || visible.ID != mount.ID {
				continue
			}
			if mount.Partition != nil {
				mounted[*mount.Partition] = true
			} else if mount.Device != nil {
				mounted[*mount.Device] = true
			}
		}
	}
	cards := make(map[string]bool)
	parents := make(map[string]bool)
	for _, device := range *storage.Devices.Data {
		if device.ParentDisk != nil {
			parents[*device.ParentDisk] = true
		}
		if device.Kind == "device" && isCard(root, device.Name) {
			cards[device.Name] = true
		}
	}
	for _, device := range *storage.Devices.Data {
		if mounted[device.Name] {
			continue
		}
		const reason = "sem montagem vinculada; presença de biblioteca desconhecida"
		switch {
		case device.ParentDisk != nil && cards[*device.ParentDisk] && !mounted[*device.ParentDisk]:
			name := device.Name
			result = append(result, NotExamined{Device: *device.ParentDisk, Partition: &name, Reason: reason})
		case cards[device.Name] && !parents[device.Name]:
			result = append(result, NotExamined{Device: device.Name, Reason: reason})
		}
	}
	return result
}

func isCard(root fs.FS, name string) bool {
	kind, err := fs.ReadFile(root, path.Join("sys/class/block", name, "device/type"))
	if err == nil {
		return strings.EqualFold(strings.TrimSpace(string(kind)), "SD")
	}
	// If the kernel omitted the media type, retain bare MMC block devices
	// as candidates. This says nothing about a physical slot or ROM contents.
	number, ok := strings.CutPrefix(name, "mmcblk")
	return ok && number != "" && strings.Trim(number, "0123456789") == ""
}
