package library

import (
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/WilliamO0o/Stark-Manager/internal/diagnostic"
)

type UpdateOptions struct {
	Cache    *Cache
	Force    bool
	VolumeID func(*diagnostic.Mount) string
	Progress func(Event)
}

type Event struct {
	Phase  string
	Path   string
	Files  int
	Done   int
	Total  int
	Result *Snapshot
}

type folderJob struct {
	system, name string
	identity     familyIdentity
}

func (s *scanner) event(phase string, snapshot *Snapshot) {
	if s.options.Progress != nil {
		s.options.Progress(Event{phase, s.active.name, s.examinedFiles, s.done, len(s.jobs), snapshot})
	}
}

func (s *scanner) updateFolders() {
	sort.Slice(s.jobs, func(i, j int) bool {
		a, b := s.jobs[i], s.jobs[j]
		if a.system != b.system {
			return a.system < b.system
		}
		return a.name < b.name
	})
	all := s.groups
	for _, job := range s.jobs {
		s.active, s.examinedFiles, s.folderEmpty = job, 0, false
		s.groups = make(map[groupKey]*group)
		s.previous, s.current = nil, make(map[string]FileRecord)
		mount := s.mountAt(job.name)
		identity := s.volumeID(mount)
		key := job.name + "\x00" + identity
		if s.options.Cache != nil {
			previous, err := s.options.Cache.load(key)
			if err != nil {
				s.report.Limitations = append(s.report.Limitations, "cache: "+err.Error())
			}
			if previous != nil && previous.Path == job.name && previous.VolumeID == identity {
				s.event("cached", previous)
				if !s.options.Force && previous.RulesVersion == RulesVersion {
					s.previous = previous.Files
				}
			}
		}
		s.event("folder", nil)
		pendingStart, portsStart := len(s.report.Pending), len(s.report.Ports)
		complete := true
		switch {
		case job.system == "ports":
			s.scanPorts(job.name, job.identity.entries)
		case auxiliary(job.system):
			if mount != nil && mount.Filesystem != "autofs" {
				item := s.origin(job.system, fileLocation(mount, job.name).volume, mount)
				item.paths[job.name] = true
				item.result.CatalogOnly = true
			}
		default:
			_, complete = s.walk(job.system, job.name, "", make(map[location]bool))
		}
		result := Snapshot{
			ExaminedAt: time.Now().UTC(), RulesVersion: RulesVersion, System: job.system,
			Path: job.name, VolumeID: identity, Files: s.current,
			Systems: []System{}, Ports: s.report.Ports[portsStart:],
			Pending: s.report.Pending[pendingStart:], Empty: s.folderEmpty, Complete: complete && len(s.report.Pending) == pendingStart,
		}
		if mount != nil {
			result.Device, result.Partition = mount.Device, mount.Partition
		}
		for key, item := range s.groups {
			if all[key] == nil {
				all[key] = s.newGroup(key.system, item.result.Partition, item.result.Device)
			}
			mergeGroup(all[key], item)
			item.finish()
			s.identifyGroup(&item.result)
			result.Systems = append(result.Systems, item.result)
		}
		sort.Slice(result.Systems, func(i, j int) bool { return result.Systems[i].Paths[0] < result.Systems[j].Paths[0] })
		if result.Complete && s.options.Cache != nil {
			if err := s.options.Cache.save(key, result); err != nil {
				s.report.Limitations = append(s.report.Limitations, "cache: "+err.Error())
			}
		}
		if result.Empty {
			s.report.EmptySystemFolders = append(s.report.EmptySystemFolders, job.name)
		}
		s.done++
		s.event("finished", &result)
	}
	s.groups = all
}

func mergeGroup(dst, src *group) {
	for name := range src.paths {
		dst.paths[name] = true
	}
	for category, count := range src.result.Counts {
		dst.result.Counts[category] += count
	}
	dst.result.CatalogOnly = src.result.CatalogOnly
	dst.result.EmptyFolders = append(dst.result.EmptyFolders, src.result.EmptyFolders...)
	dst.result.FoldersWithoutGames = append(dst.result.FoldersWithoutGames, src.result.FoldersWithoutGames...)
	dst.result.Files = append(dst.result.Files, src.result.Files...)
	dst.result.Games = append(dst.result.Games, src.result.Games...)
	dst.result.ROMChecks = append(dst.result.ROMChecks, src.result.ROMChecks...)
	dst.result.UnknownFiles = append(dst.result.UnknownFiles, src.result.UnknownFiles...)
	dst.result.UninspectedPackages = append(dst.result.UninspectedPackages, src.result.UninspectedPackages...)
	dst.result.DuplicateFiles += src.result.DuplicateFiles
	dst.result.DuplicateGames += src.result.DuplicateGames
	dst.related = append(dst.related, src.related...)
}

func (s *scanner) volumeID(mount *diagnostic.Mount) string {
	if mount == nil || s.options.VolumeID == nil {
		return ""
	}
	if id, ok := s.volumeIDs[mount.MajorMinor]; ok {
		return id
	}
	id := s.options.VolumeID(mount)
	s.volumeIDs[mount.MajorMinor] = id
	return id
}

func (s *scanner) noteFile() {
	s.examinedFiles++
	s.event("file", nil)
}

func (s *scanner) listFolder(name string, loc location) ([]fs.DirEntry, error) {
	if entries, ok := s.listings[loc]; ok {
		// An alias may have inaccessible ancestors even when the first
		// path to the directory was readable.
		if _, err := s.lstat(name); err != nil {
			return nil, err
		}
		return entries, nil
	}
	entries, err := fs.ReadDir(s.root, strings.TrimPrefix(name, "/"))
	if err == nil {
		s.listings[loc] = entries
	}
	return entries, err
}

func (s *scanner) cachedFile(name string, mount *diagnostic.Mount, loc location) *fileResult {
	if s.options.Cache == nil {
		return nil
	}
	id := s.volumeID(mount)
	if id == "" {
		return nil
	}
	info, err := s.lstat(name)
	if err != nil {
		s.pending(name, err.Error())
		return nil
	}
	s.current[name] = FileRecord{VolumeID: id, VolumePath: loc.root, Size: info.Size(), Modified: info.ModTime()}
	previous, ok := s.previous[name]
	if !ok || previous.VolumeID != id || previous.VolumePath != loc.root ||
		previous.Size != info.Size() || !previous.Modified.Equal(info.ModTime()) {
		return nil
	}
	return &fileResult{category: previous.Category, complete: true, check: previous.Check}
}

func (s *scanner) rememberFile(name string, mount *diagnostic.Mount, loc location, result fileResult) {
	if s.options.Cache == nil {
		return
	}
	record, ok := s.current[name]
	if !ok {
		if s.volumeID(mount) == "" {
			return
		}
		info, err := s.lstat(name)
		if err != nil {
			s.pending(name, err.Error())
			return
		}
		record = FileRecord{VolumeID: s.volumeID(mount), VolumePath: loc.root, Size: info.Size(), Modified: info.ModTime()}
	}
	record.Category, record.Check = result.category, result.check
	s.current[name] = record
}
