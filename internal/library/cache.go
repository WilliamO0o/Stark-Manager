package library

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/WilliamO0o/Stark-Manager/internal/diagnostic"
)

// Bump RulesVersion when file classification or archive inspection changes.
const RulesVersion = 1

type Cache struct{ Dir string }

type FileRecord struct {
	VolumeID   string    `json:"volume_id"`
	VolumePath string    `json:"volume_path"`
	Size       int64     `json:"size"`
	Modified   time.Time `json:"modified"`
	Category   string    `json:"category"`
	Check      *ROMCheck `json:"rom_check,omitempty"`
}

type Snapshot struct {
	LocalSource  *LocalSource          `json:"local_source,omitempty"`
	ExaminedAt   time.Time             `json:"examined_at"`
	RulesVersion int                   `json:"rules_version"`
	System       string                `json:"system"`
	Path         string                `json:"path"`
	VolumeID     string                `json:"volume_id"`
	Partition    *string               `json:"partition"`
	Device       *string               `json:"device"`
	Systems      []System              `json:"systems"`
	Ports        []PortInventory       `json:"ports"`
	Pending      []Pending             `json:"pending"`
	Empty        bool                  `json:"empty"`
	Complete     bool                  `json:"complete"`
	Files        map[string]FileRecord `json:"files,omitempty"`
}

func (c Cache) filename(key string) string {
	return filepath.Join(c.Dir, fmt.Sprintf("%x.json", sha256.Sum256([]byte(key))))
}

func readSnapshot(name string) (*Snapshot, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var snapshot Snapshot
	if err := json.NewDecoder(file).Decode(&snapshot); err != nil {
		return nil, err
	}
	if !snapshot.Complete || snapshot.Path == "" || snapshot.ExaminedAt.IsZero() {
		return nil, errors.New("inventário salvo inválido")
	}
	return &snapshot, nil
}

func (c Cache) load(key string) (*Snapshot, error) {
	snapshot, err := readSnapshot(c.filename(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return snapshot, err
}

func (c Cache) save(key string, snapshot Snapshot) error {
	if err := os.MkdirAll(c.Dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(c.Dir, ".inventory-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	err = json.NewEncoder(file).Encode(snapshot)
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), c.filename(key))
}

// Snapshots reads only the cache directory. It requires no mounted library.
func (c Cache) Snapshots(filter string) ([]Snapshot, []string) {
	snapshots, issues := []Snapshot{}, []string{}
	entries, err := os.ReadDir(c.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return snapshots, issues
	}
	if err != nil {
		return snapshots, []string{err.Error()}
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		snapshot, err := readSnapshot(filepath.Join(c.Dir, entry.Name()))
		if err != nil {
			issues = append(issues, entry.Name()+": "+err.Error())
			continue
		}
		if filter == "" || strings.EqualFold(filter, snapshot.System) {
			snapshots = append(snapshots, *snapshot)
		}
	}
	return snapshots, issues
}

// LocalVolumeID uses the kernel MMC card ID or the filesystem UUID exposed by
// udev. Device names and mount targets alone are never treated as identities.
func LocalVolumeID(mount *diagnostic.Mount) string {
	name := mount.Partition
	if name == nil {
		name = mount.Device
	}
	if name == nil {
		return ""
	}
	if mount.Device != nil {
		cid, err := os.ReadFile(filepath.Join("/sys/class/block", *mount.Device, "device/cid"))
		if err == nil && strings.TrimSpace(string(cid)) != "" {
			if mount.Partition == nil {
				return "mmc:" + strings.TrimSpace(string(cid))
			}
			number, err := os.ReadFile(filepath.Join("/sys/class/block", *mount.Partition, "partition"))
			if err == nil && strings.TrimSpace(string(number)) != "" {
				return "mmc:" + strings.TrimSpace(string(cid)) + ":" + strings.TrimSpace(string(number))
			}
		}
	}
	uuids, _ := os.ReadDir("/dev/disk/by-uuid")
	for _, entry := range uuids {
		link, err := os.Readlink(filepath.Join("/dev/disk/by-uuid", entry.Name()))
		if err == nil && filepath.Base(link) == *name {
			return "uuid:" + entry.Name()
		}
	}
	return ""
}
