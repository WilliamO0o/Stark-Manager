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
	LocalSources       []*LocalSource  `json:"local_sources,omitempty"`
	SchemaVersion      int             `json:"schema_version"`
	Filter             string          `json:"filter,omitempty"`
	Examined           []string        `json:"examined_roots"`
	Systems            []System        `json:"systems"`
	NotExamined        []NotExamined   `json:"not_examined"`
	Pending            []Pending       `json:"pending"`
	Limitations        []string        `json:"limitations"`
	Ports              []PortInventory `json:"ports"`
	EmptySystemFolders []string        `json:"empty_system_folders"`
}

type System struct {
	LocalSource         *LocalSource     `json:"local_source,omitempty"`
	Files               []ClassifiedFile `json:"files"`
	Name                string           `json:"name"`
	Family              string           `json:"family"`
	Type                string           `json:"type"`
	DisplayName         string           `json:"display_name,omitempty"`
	CatalogEntries      []CatalogEntry   `json:"catalog_entries,omitempty"`
	CatalogOnly         bool             `json:"catalog_only,omitempty"`
	Partition           *string          `json:"partition"`
	Device              *string          `json:"device"`
	Paths               []string         `json:"paths"`
	Counts              map[string]int   `json:"counts"`
	EmptyFolders        []string         `json:"empty_folders"`
	FoldersWithoutGames []string         `json:"folders_without_games"`
	DuplicateFiles      int              `json:"duplicate_files"`
	DuplicateGames      int              `json:"duplicate_games"`
	Games               []Game           `json:"games"`
	ROMChecks           []ROMCheck       `json:"rom_checks"`
	Unassociated        []Related        `json:"unassociated"`
	UnknownFiles        []string         `json:"unknown_files"`
	UninspectedPackages []string         `json:"uninspected_packages"`
}

type ClassifiedFile struct {
	Path           string    `json:"path"`
	Category       string    `json:"category"`
	Format         string    `json:"format,omitempty"`
	ExpectedSystem string    `json:"expected_system,omitempty"`
	ROMCheck       *ROMCheck `json:"rom_check,omitempty"`
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
	source string
	volume string
	root   string
}

type groupKey struct{ system, volume, source string }
type group struct {
	result  System
	paths   map[string]bool
	related []Related
}

type fileResult struct {
	category string
	complete bool
	check    *ROMCheck
}

type scanner struct {
	root             fs.FS
	lstat            func(string) (fs.FileInfo, error)
	mountAt          func(string) *diagnostic.Mount
	report           Report
	groups           map[groupKey]*group
	files            map[location]fileResult
	empty            map[location]bool
	withoutGames     map[location]bool
	catalog          []CatalogEntry
	identities       map[string]familyIdentity
	sharedExtensions map[string]bool
	portEntries      map[location]bool
	options          UpdateOptions
	jobs             []folderJob
	active           folderJob
	done             int
	examinedFiles    int
	folderEmpty      bool
	volumeIDs        map[string]string
	listings         map[location][]fs.DirEntry
	previous         map[string]FileRecord
	current          map[string]FileRecord
}

// Collect inspects conventional ROM roots within the current mount namespace.
// lstat inspects the supplied filesystem without following the final symlink.
func Collect(root fs.FS, storage diagnostic.StorageReport, lstat func(string) (fs.FileInfo, error), filter string, catalog ...CatalogEntry) Report {
	return Update(root, storage, lstat, filter, UpdateOptions{}, catalog...)
}

// Update inventories the chosen folders and optionally reports progress and saves snapshots.
func Update(root fs.FS, storage diagnostic.StorageReport, lstat func(string) (fs.FileInfo, error), filter string, options UpdateOptions, catalog ...CatalogEntry) Report {
	s := scanner{
		root: root, lstat: lstat, mountAt: storage.MountLookup(), options: options,
		volumeIDs: make(map[string]string), listings: make(map[location][]fs.DirEntry),
		catalog: catalog, identities: make(map[string]familyIdentity),
		sharedExtensions: make(map[string]bool), portEntries: make(map[location]bool),
		report: Report{
			SchemaVersion: 1, Filter: strings.ToLower(filter), Examined: []string{},
			Systems: []System{}, NotExamined: []NotExamined{},
			Pending: []Pending{}, Limitations: []string{}, Ports: []PortInventory{}, EmptySystemFolders: []string{},
		},
		groups: make(map[groupKey]*group), files: make(map[location]fileResult), empty: make(map[location]bool), withoutGames: make(map[location]bool),
	}
	s.report.Limitations = append(s.report.Limitations, storage.Devices.Issues...)
	s.report.Limitations = append(s.report.Limitations, storage.Mounts.Issues...)
	for extension := range romSystemByExtension {
		s.sharedExtensions[extension] = sharedROMExtension(extension, catalog)
	}
	if len(options.Roots) > 0 {
		s.discoverLocalSources()
	} else {
		s.report.NotExamined = UnexaminedCards(root, storage)
		for _, candidate := range DiscoverRoots(storage, s.mountAt) {
			s.scanRoot(candidate, nil)
		}
	}
	s.updateFolders()
	for _, item := range s.groups {
		if s.report.Filter != "" && item.result.Name != s.report.Filter {
			continue
		}
		item.finish()
		s.identifyGroup(&item.result)
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

func (s *scanner) scanRoot(candidate RootCandidate, source *LocalSource) {
	if mount := s.scanMount(candidate.Path, source); mount == nil || mount.Filesystem == "autofs" {
		s.pending(candidate.Path, "topologia indisponível ou automontagem")
		return
	}
	info, err := s.lstat(candidate.Path)
	if err != nil {
		if candidate.Observed || !errors.Is(err, fs.ErrNotExist) {
			s.pending(candidate.Path, err.Error())
		}
		return
	}
	if !info.IsDir() {
		if info.Mode()&fs.ModeSymlink != 0 || candidate.Observed {
			s.pending(candidate.Path, "o caminho não é um diretório direto")
		}
		return
	}
	dir := strings.TrimPrefix(candidate.Path, "/")
	if dir == "" {
		dir = "."
	}
	entries, err := fs.ReadDir(s.root, dir)
	if err != nil {
		s.pending(candidate.Path, err.Error())
	}
	if err == nil || len(entries) > 0 {
		s.report.Examined = append(s.report.Examined, candidate.Path)
	}
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if s.report.Filter != "" && name != s.report.Filter {
			continue
		}
		child := path.Join(candidate.Path, entry.Name())
		if entry.IsDir() {
			identity := identifyFolder(candidate.Path, entry.Name(), s.catalog)
			s.identities[child] = identity
			if !auxiliary(name) || name == "ports" || identity.family == "support" {
				s.jobs = append(s.jobs, folderJob{system: name, name: child, identity: identity, source: source})
			}
		} else if !auxiliary(name) && entry.Type()&fs.ModeSymlink != 0 {
			s.pending(child, "link simbólico não seguido")
		}
	}
}

func (s *scanner) walk(system, name, previousVolume string, ancestors map[location]bool) (hasGames, complete bool) {
	mount := s.scanMount(name, s.active.source)
	if mount == nil || mount.Filesystem == "autofs" {
		s.pending(name, "topologia indisponível ou automontagem")
		return false, false
	}
	loc := s.sourceLocation(mount, name)
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
	entries, err := s.listFolder(name, loc)
	if name == s.active.name && err == nil && len(entries) == 0 {
		s.folderEmpty = true
	}
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
	s.noteFile()
	mount := s.scanMount(name, s.active.source)
	if mount == nil || mount.Filesystem == "autofs" {
		s.pending(name, "topologia indisponível ou automontagem")
		return false, false
	}
	loc := s.sourceLocation(mount, name)
	item := s.origin(system, loc.volume, mount)
	if loc.volume != previousVolume {
		item.paths[name] = true
	}
	if previous, found := s.files[loc]; found {
		item.result.DuplicateFiles++
		s.rememberFile(name, mount, loc, previous)
		if regular && previous.check != nil {
			check, applicable, _ := s.inspectROM(name, mount, &previous)
			if applicable && (check.ExpectedSystem != previous.check.ExpectedSystem || check.Comparison != previous.check.Comparison) {
				item.result.ROMChecks = append(item.result.ROMChecks, check)
			}
		}
		if previous.category == "games" {
			item.result.DuplicateGames++
		}
		return previous.category == "games", previous.complete
	}
	category, complete := "unknown", true
	var inspected *ROMCheck
	var cached *fileResult
	if regular {
		cached = s.cachedFile(name, mount, loc)
	}
	if regular {
		if cached != nil {
			category = cached.category
		} else {
			category = classify(system, name)
		}
		if check, applicable, checked := s.inspectROM(name, mount, cached); applicable {
			inspected = &check
			item.result.ROMChecks = append(item.result.ROMChecks, check)
			complete = checked
			if len(check.RecognizedSystems) != 0 {
				category = "games"
			} else if check.Format == "rar" {
				category = "uninspected_packages"
			}
		}
	}
	result := fileResult{category: category, complete: complete, check: inspected}
	s.files[loc] = result
	s.rememberFile(name, mount, loc, result)
	item.result.Files = append(item.result.Files, ClassifiedFile{
		Path: name, Category: category, Format: strings.TrimPrefix(strings.ToLower(path.Ext(name)), "."),
		ExpectedSystem: s.expectedROM(name).system, ROMCheck: inspected,
	})
	item.result.Counts[category]++
	switch category {
	case "games":
		item.result.Games = append(item.result.Games, Game{Path: name, Related: []Related{}})
	case "unknown":
		item.result.UnknownFiles = append(item.result.UnknownFiles, name)
	case "uninspected_packages":
		item.result.UninspectedPackages = append(item.result.UninspectedPackages, name)
	case "saves", "states", "images", "videos", "cheats", "audio", "replays", "possible_states":
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
	key := groupKey{system: system, volume: volume}
	if s.active.source != nil {
		key.source = s.active.source.Path
	}
	if s.groups[key] == nil {
		s.groups[key] = s.newGroup(system, mount.Partition, mount.Device)
	}
	return s.groups[key]
}

func (s *scanner) newGroup(system string, partition, device *string) *group {
	return &group{paths: make(map[string]bool), result: System{
		Name: system, LocalSource: s.active.source, Partition: partition, Device: device, Files: []ClassifiedFile{},
		Paths: []string{}, EmptyFolders: []string{}, FoldersWithoutGames: []string{}, Games: []Game{}, ROMChecks: []ROMCheck{}, Unassociated: []Related{},
		UnknownFiles: []string{}, UninspectedPackages: []string{},
		Counts: map[string]int{"games": 0, "saves": 0, "states": 0, "images": 0, "videos": 0, "metadata": 0, "unknown": 0},
	}}
}

func (s *scanner) pending(name, reason string) {
	s.report.Pending = append(s.report.Pending, Pending{Path: name, Reason: reason})
}
