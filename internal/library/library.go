// Package library inventories existing ROM trees using read-only filesystem access.
package library

import (
	"errors"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/WilliamO0o/Stark-Manager/internal/diagnostic"
)

type Report struct {
	SchemaVersion int           `json:"schema_version"`
	Filter        string        `json:"filter,omitempty"`
	Examined      []string      `json:"examined_roots"`
	Systems       []System      `json:"systems"`
	NotExamined   []NotExamined `json:"not_examined"`
	Pending       []Pending     `json:"pending"`
	Limitations   []string      `json:"limitations"`
}

type System struct {
	Name                string         `json:"name"`
	Partition           *string        `json:"partition"`
	Device              *string        `json:"device"`
	Paths               []string       `json:"paths"`
	Counts              map[string]int `json:"counts"`
	EmptyFolders        []string       `json:"empty_folders"`
	FoldersWithoutGames []string       `json:"folders_without_games"`
	DuplicateFiles      int            `json:"duplicate_files"`
	DuplicateGames      int            `json:"duplicate_games"`
	Games               []Game         `json:"games"`
	Unassociated        []Related      `json:"unassociated"`
}

type Game struct {
	Path    string    `json:"path"`
	Related []Related `json:"related"`
}

type Related struct {
	Path     string `json:"path"`
	Category string `json:"category"`
}

type Pending struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type NotExamined struct {
	Device    string  `json:"device"`
	Partition *string `json:"partition"`
	Reason    string  `json:"reason"`
}

type location struct {
	volume string
	root   string
}

type groupKey struct{ system, volume string }
type group struct {
	result  System
	paths   map[string]bool
	related []Related
}

type fileResult struct {
	category string
	complete bool
}

type scanner struct {
	root         fs.FS
	lstat        func(string) (fs.FileInfo, error)
	mountAt      func(string) *diagnostic.Mount
	report       Report
	groups       map[groupKey]*group
	files        map[location]fileResult
	empty        map[location]bool
	withoutGames map[location]bool
}

// Collect inspects conventional ROM roots within the current mount namespace.
// lstat inspects the supplied filesystem without following the final symlink.
func Collect(root fs.FS, storage diagnostic.StorageReport, lstat func(string) (fs.FileInfo, error), filter string) Report {
	s := scanner{
		root: root, lstat: lstat, mountAt: storage.MountLookup(),
		report: Report{
			SchemaVersion: 1, Filter: strings.ToLower(filter), Examined: []string{},
			Systems: []System{}, NotExamined: unmountedCards(root, storage),
			Pending: []Pending{}, Limitations: []string{},
		},
		groups: make(map[groupKey]*group), files: make(map[location]fileResult), empty: make(map[location]bool), withoutGames: make(map[location]bool),
	}
	s.report.Limitations = append(s.report.Limitations, storage.Devices.Issues...)
	s.report.Limitations = append(s.report.Limitations, storage.Mounts.Issues...)
	for _, candidate := range libraryRoots(storage, s.mountAt) {
		s.scanRoot(candidate)
	}
	for _, item := range s.groups {
		if s.report.Filter != "" && item.result.Name != s.report.Filter {
			continue
		}
		item.finish()
		s.report.Systems = append(s.report.Systems, item.result)
	}
	sort.Slice(s.report.Systems, func(i, j int) bool {
		a, b := s.report.Systems[i], s.report.Systems[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Paths[0] < b.Paths[0]
	})
	return s.report
}

func (s *scanner) scanRoot(candidate libraryRoot) {
	if mount := s.mountAt(candidate.path); mount == nil || mount.Filesystem == "autofs" {
		s.pending(candidate.path, "topologia indisponível ou automontagem")
		return
	}
	info, err := s.lstat(candidate.path)
	if err != nil {
		if candidate.observed || !errors.Is(err, fs.ErrNotExist) {
			s.pending(candidate.path, err.Error())
		}
		return
	}
	if !info.IsDir() {
		if info.Mode()&fs.ModeSymlink != 0 || candidate.observed {
			s.pending(candidate.path, "o caminho não é um diretório direto")
		}
		return
	}
	entries, err := fs.ReadDir(s.root, strings.TrimPrefix(candidate.path, "/"))
	if err != nil {
		s.pending(candidate.path, err.Error())
	}
	if err == nil || len(entries) > 0 {
		s.report.Examined = append(s.report.Examined, candidate.path)
	}
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if auxiliary(name) {
			continue
		}
		child := path.Join(candidate.path, entry.Name())
		if entry.IsDir() {
			s.walk(name, child, "", make(map[location]bool))
		} else if entry.Type()&fs.ModeSymlink != 0 {
			s.pending(child, "link simbólico não seguido")
		}
	}
}

func (s *scanner) walk(system, name, previousVolume string, ancestors map[location]bool) (hasGames, complete bool) {
	mount := s.mountAt(name)
	if mount == nil || mount.Filesystem == "autofs" {
		s.pending(name, "topologia indisponível ou automontagem")
		return false, false
	}
	loc := fileLocation(mount, name)
	if ancestors[loc] {
		s.pending(name, "ciclo de diretórios detectado")
		return false, false
	}
	ancestors[loc] = true
	defer delete(ancestors, loc)
	item := s.origin(system, loc.volume, mount)
	if previousVolume != loc.volume {
		item.paths[name] = true
	}
	entries, err := fs.ReadDir(s.root, strings.TrimPrefix(name, "/"))
	complete = err == nil
	if err != nil {
		s.pending(name, err.Error())
	} else if len(entries) == 0 && !s.empty[loc] {
		s.empty[loc] = true
		item.result.EmptyFolders = append(item.result.EmptyFolders, name)
	}
	for _, entry := range entries {
		child := path.Join(name, entry.Name())
		var childGames, childComplete bool
		if entry.IsDir() {
			childGames, childComplete = s.walk(system, child, loc.volume, ancestors)
		} else {
			childGames, childComplete = s.recordFile(system, child, loc.volume, entry.Type().IsRegular())
		}
		hasGames = hasGames || childGames
		complete = complete && childComplete
	}
	if complete && len(entries) > 0 && !hasGames && !s.withoutGames[loc] {
		s.withoutGames[loc] = true
		item.result.FoldersWithoutGames = append(item.result.FoldersWithoutGames, name)
	}
	return hasGames, complete
}

func (s *scanner) recordFile(system, name, previousVolume string, regular bool) (hasGame, complete bool) {
	mount := s.mountAt(name)
	if mount == nil || mount.Filesystem == "autofs" {
		s.pending(name, "topologia indisponível ou automontagem")
		return false, false
	}
	loc := fileLocation(mount, name)
	item := s.origin(system, loc.volume, mount)
	if loc.volume != previousVolume {
		item.paths[name] = true
	}
	if previous, found := s.files[loc]; found {
		item.result.DuplicateFiles++
		if previous.category == "games" {
			item.result.DuplicateGames++
		}
		return previous.category == "games", previous.complete
	}
	category := "unknown"
	complete = true
	if regular {
		if isSNES(system) && strings.EqualFold(path.Ext(name), ".zip") {
			game, err := zipHasSNESROM(s.root, name)
			if err != nil {
				s.pending(name, "não foi possível ler o índice ZIP: "+err.Error())
				complete = false
			} else if game {
				category = "games"
			}
		} else {
			category = classify(system, name)
		}
	}
	s.files[loc] = fileResult{category, complete}
	item.result.Counts[category]++
	switch category {
	case "games":
		item.result.Games = append(item.result.Games, Game{Path: name, Related: []Related{}})
	case "saves", "states", "images", "videos":
		item.related = append(item.related, Related{Path: name, Category: category})
	}
	return category == "games", complete
}

func fileLocation(mount *diagnostic.Mount, name string) location {
	if !strings.HasPrefix(mount.Root, "/") {
		// Namespace mounts do not have a filesystem pathname as their root.
		return location{volume: mount.Filesystem + ":" + mount.MajorMinor, root: name}
	}
	return location{
		volume: mount.Filesystem + ":" + mount.MajorMinor,
		root:   path.Join(mount.Root, strings.TrimPrefix(name, mount.Target)),
	}
}

func (s *scanner) origin(system, volume string, mount *diagnostic.Mount) *group {
	key := groupKey{system, volume}
	if s.groups[key] == nil {
		s.groups[key] = &group{paths: make(map[string]bool), result: System{
			Name: system, Partition: mount.Partition, Device: mount.Device,
			Paths: []string{}, EmptyFolders: []string{}, FoldersWithoutGames: []string{}, Games: []Game{}, Unassociated: []Related{},
			Counts: map[string]int{"games": 0, "saves": 0, "states": 0, "images": 0, "videos": 0, "metadata": 0, "unknown": 0},
		}}
	}
	return s.groups[key]
}

func (s *scanner) pending(name, reason string) {
	s.report.Pending = append(s.report.Pending, Pending{Path: name, Reason: reason})
}
