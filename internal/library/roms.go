package library

import (
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/WilliamO0o/Stark-Manager/internal/diagnostic"
)

const (
	ROMCompatible    = "compatible"
	ROMWrongFolder   = "possible_wrong_folder"
	ROMIndeterminate = "indeterminate"
)

type ROMCheck struct {
	Path              string   `json:"path"`
	Format            string   `json:"format"`
	ROMExtensions     []string `json:"rom_extensions"`
	RecognizedSystems []string `json:"recognized_systems"`
	ExpectedSystem    string   `json:"expected_system"`
	Comparison        string   `json:"comparison"`
	Partition         *string  `json:"partition"`
	Device            *string  `json:"device"`
	MountTarget       string   `json:"mount_target"`
	Limitations       []string `json:"limitations"`
}

var romSystemByExtension = map[string]string{
	".gb": "gb", ".gbc": "gbc", ".gba": "gba",
	".nes": "nes", ".fds": "nes", ".unf": "nes", ".unif": "nes",
	".sfc": "snes", ".smc": "snes", ".swc": "snes", ".fig": "snes",
}

// These extensions are used by more than one platform or for non-ROM files.
var ambiguousROMExtensions = []string{".bin", ".rom", ".st", ".bs", ".bsx", ".dmg"}

type romFormats []string

func (formats *romFormats) add(ext string) {
	if !slices.Contains(*formats, ext) {
		*formats = append(*formats, ext)
	}
}

func knownROMExtension(ext string) bool {
	return romSystemByExtension[ext] != "" || slices.Contains(ambiguousROMExtensions, ext)
}

func platformForFolder(folder string) string {
	switch folder {
	case "gb", "gbc", "gba", "sufami":
		return folder
	case "nes", "famicom", "fds":
		return "nes"
	case "snes", "sfc", "superfamicom", "snes-hacks", "snesmsu1", "satellaview", "sgb":
		return "snes"
	}
	if kind := standardFamilies[folder].kind; kind == "console" || kind == "handheld" || kind == "arcade" || kind == "computer" || kind == "tabletop" {
		return folder
	}
	return ""
}

type catalogLabel struct{ id, name string }

var catalogROMPlatforms = buildCatalogROMPlatforms()

func buildCatalogROMPlatforms() map[catalogLabel]string {
	platforms := make(map[catalogLabel]string)
	for key := range exactFamilies {
		folder, _, _ := strings.Cut(key.relative, "/")
		label := catalogLabel{key.name, key.fullName}
		platform := platformForFolder(folder)
		if old, seen := platforms[label]; seen && old != platform {
			platform = ""
		}
		platforms[label] = platform
	}
	return platforms
}

func catalogROMPlatform(entry CatalogEntry) string {
	return catalogROMPlatforms[catalogLabel{normalized(entry.Name), normalized(entry.FullName)}]
}

func catalogExtensions(entry CatalogEntry) []string {
	return strings.Fields(strings.ToLower(entry.Extension))
}

type romExpectation struct {
	system     string
	extensions map[string]bool
}

func (s *scanner) expectedROM(name string) romExpectation {
	for folder := path.Dir(name); folder != "/"; folder = path.Dir(folder) {
		identity, ok := s.identities[folder]
		if !ok {
			continue
		}
		expected := romExpectation{system: platformForFolder(strings.ToLower(path.Base(folder)))}
		longest := 0
		for _, entry := range identity.entries {
			depth := len(entry.ResolvedPath)
			if depth < longest || !strings.HasPrefix(name, entry.ResolvedPath+"/") {
				continue
			}
			platform := catalogROMPlatform(entry)
			if depth > longest {
				longest = depth
				expected = romExpectation{system: platform, extensions: make(map[string]bool)}
			} else if expected.system != platform {
				expected.system = ""
			}
			for _, ext := range catalogExtensions(entry) {
				expected.extensions[ext] = true
			}
		}
		return expected
	}
	return romExpectation{}
}

func sharedROMExtension(ext string, systems []CatalogEntry) bool {
	platform := romSystemByExtension[ext]
	if platform == "" {
		return true
	}
	for _, entry := range systems {
		other := catalogROMPlatform(entry)
		// SuFami has a separate path and shares the SNES file formats.
		if other == "sufami" && platform == "snes" {
			continue
		}
		if other != platform && slices.Contains(catalogExtensions(entry), ext) {
			return true
		}
	}
	return false
}

func (s *scanner) inspectROM(name string, mount *diagnostic.Mount, previous *fileResult) (ROMCheck, bool, bool) {
	ext := strings.ToLower(path.Ext(name))
	if !knownROMExtension(ext) && ext != ".zip" && ext != ".7z" && ext != ".rar" {
		return ROMCheck{}, false, true
	}
	expected := s.expectedROM(name)
	check := ROMCheck{
		Path: name, Format: strings.TrimPrefix(ext, "."), ExpectedSystem: expected.system,
		Comparison: ROMIndeterminate, Partition: mount.Partition, Device: mount.Device, MountTarget: mount.Target,
		ROMExtensions: []string{}, RecognizedSystems: []string{}, Limitations: []string{},
	}
	if check.ExpectedSystem == "" {
		check.ExpectedSystem = "unknown"
	}
	var formats romFormats
	var inspector func(fs.FS, string) (romFormats, error)
	switch ext {
	case ".zip":
		inspector = zipROMFormats
	case ".7z":
		inspector = sevenZipROMFormats
	case ".rar":
		check.Limitations = append(check.Limitations, "pacote RAR não inspecionado")
	default:
		formats.add(ext)
	}
	complete := true
	if inspector != nil {
		if previous != nil && previous.check != nil && previous.check.Format == check.Format {
			formats, complete = previous.check.ROMExtensions, previous.complete
			if !complete {
				check.Limitations = append(check.Limitations, previous.check.Limitations...)
			}
		} else {
			var readErr error
			formats, readErr = inspector(s.root, name)
			if readErr != nil {
				formatName := check.Format
				if ext == ".zip" {
					formatName = "ZIP"
				}
				check.Limitations = append(check.Limitations, "inspeção incompleta do índice "+formatName+": "+readErr.Error())
				s.pending(name, check.Limitations[0])
				complete = false
			}
		}
	}

	ambiguous := false
	for _, extension := range formats {
		if slices.Contains(check.ROMExtensions, extension) {
			continue
		}
		check.ROMExtensions = append(check.ROMExtensions, extension)
		platform := romSystemByExtension[extension]
		if platform != "" && !slices.Contains(check.RecognizedSystems, platform) {
			check.RecognizedSystems = append(check.RecognizedSystems, platform)
		}
		ambiguous = ambiguous || s.sharedExtensions[extension] || platform == ""
	}
	slices.Sort(check.ROMExtensions)
	slices.Sort(check.RecognizedSystems)
	switch {
	case !complete || ext == ".rar":
	case len(check.RecognizedSystems) == 0 && !ambiguous:
		check.Limitations = append(check.Limitations, "nenhuma extensão de ROM reconhecida no pacote")
	case ambiguous || len(check.RecognizedSystems) != 1:
		check.Limitations = append(check.Limitations, "extensão ambígua, compartilhada ou pacote com vários sistemas")
	case expected.system == "":
		check.Limitations = append(check.Limitations, "sistema esperado indeterminado")
	case expected.system != check.RecognizedSystems[0]:
		switch {
		case (expected.system == "sufami" && check.RecognizedSystems[0] == "snes") ||
			(expected.system == "gb" && check.RecognizedSystems[0] == "gbc") ||
			(expected.system == "gbc" && check.RecognizedSystems[0] == "gb"):
			check.Limitations = append(check.Limitations, "formatos compartilhados ou variantes do mesmo sistema")
		default:
			check.Comparison = ROMWrongFolder
		}
	case len(expected.extensions) != 0 && !expected.extensions[ext]:
		check.Limitations = append(check.Limitations, "extensão externa não declarada no catálogo para este caminho")
	default:
		check.Comparison = ROMCompatible
	}
	return check, true, complete
}
