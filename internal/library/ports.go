package library

import (
	"path"
	"strings"

	"github.com/WilliamO0o/Stark-Manager/internal/diagnostic"
)

type PortItem struct {
	Path        string  `json:"path"`
	Partition   *string `json:"partition"`
	Device      *string `json:"device"`
	MountTarget string  `json:"mount_target"`
}

type PortInventory struct {
	PortItem
	LauncherCount      int              `json:"launcher_count"`
	CatalogEntries     []CatalogEntry   `json:"catalog_entries,omitempty"`
	Files              []ClassifiedFile `json:"files"`
	Launchers          []PortItem       `json:"launchers"`
	RelatedDirectories []PortItem       `json:"related_directories"`
	Metadata           []PortItem       `json:"metadata"`
	DuplicateLaunchers int              `json:"duplicate_launchers"`
}

func portItem(name string, mount *diagnostic.Mount) PortItem {
	return PortItem{Path: name, Partition: mount.Partition, Device: mount.Device, MountTarget: mount.Target}
}

func (s *scanner) scanPorts(name string, entries []CatalogEntry) {
	mount := s.mountAt(name)
	if mount == nil || mount.Filesystem == "autofs" {
		s.pending(name, "origem de Ports indisponível ou automontagem")
		return
	}
	inventory := PortInventory{
		PortItem: portItem(name, mount), CatalogEntries: entries, Files: []ClassifiedFile{}, Launchers: []PortItem{}, RelatedDirectories: []PortItem{}, Metadata: []PortItem{},
	}
	s.walkPorts(&inventory, name, mount, make(map[location]bool))
	inventory.LauncherCount = len(inventory.Launchers)
	s.report.Ports = append(s.report.Ports, inventory)
}

func (s *scanner) walkPorts(inventory *PortInventory, name string, mount *diagnostic.Mount, ancestors map[location]bool) {
	loc := fileLocation(mount, name)
	if ancestors[loc] {
		s.pending(name, "ciclo de diretórios em Ports")
		return
	}
	ancestors[loc] = true
	defer delete(ancestors, loc)

	entries, err := s.listFolder(name, loc)
	if err != nil {
		s.pending(name, err.Error())
	} else if name == s.active.name && len(entries) == 0 {
		s.folderEmpty = true
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			s.noteFile()
		}
		ext := strings.ToLower(path.Ext(entry.Name()))
		launcher := ext == ".sh"
		metadata := ext == ".xml" || ext == ".json" || ext == ".ini" || ext == ".toml" || ext == ".yaml" || ext == ".yml"
		launcher = launcher && entry.Type().IsRegular()
		metadata = metadata && entry.Type().IsRegular()
		child := path.Join(name, entry.Name())
		childMount := s.mountAt(child)
		if childMount == nil || childMount.Filesystem == "autofs" {
			s.pending(child, "origem de Ports indisponível ou automontagem")
			continue
		}
		loc := fileLocation(childMount, child)
		seen := s.portEntries[loc]
		s.portEntries[loc] = true
		item := portItem(child, childMount)
		if !entry.IsDir() && !seen {
			category := "unknown"
			if launcher {
				category = "launcher"
			} else if metadata {
				category = "metadata"
			}
			inventory.Files = append(inventory.Files, ClassifiedFile{Path: child, Category: category, Format: strings.TrimPrefix(ext, ".")})
		}
		switch {
		case entry.IsDir():
			if !seen {
				inventory.RelatedDirectories = append(inventory.RelatedDirectories, item)
			}
			s.walkPorts(inventory, child, childMount, ancestors)
		case launcher && seen:
			inventory.DuplicateLaunchers++
		case launcher:
			inventory.Launchers = append(inventory.Launchers, item)
		case metadata && !seen:
			inventory.Metadata = append(inventory.Metadata, item)
		}
	}
}
