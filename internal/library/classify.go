package library

import (
	"path"
	"sort"
	"strings"
)

func classify(system, name string) string {
	base := strings.ToLower(path.Base(name))
	ext := path.Ext(base)
	if _, ok := stateStem(base); ok {
		return "states"
	}
	switch ext {
	case ".srm", ".rtc":
		return "saves"
	case ".sav":
		for _, folder := range strings.Split(strings.ToLower(path.Dir(name)), "/") {
			switch folder {
			case "states", "savestates", "save_states":
				return "states"
			}
		}
		return "saves"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg":
		return "images"
	case ".mp4", ".mkv", ".avi", ".webm", ".mov", ".m4v":
		return "videos"
	case ".nfo":
		return "metadata"
	}
	switch base {
	case "gamelist.xml", "gamelist.json", "metadata.xml", "metadata.json", "desktop.ini", "thumbs.db":
		return "metadata"
	}
	if isSNES(system) {
		switch {
		case base == "gamelist.xml.old":
			return "metadata_backups"
		case ext == ".cht":
			return "cheats"
		case ext == ".spc":
			return "audio"
		case ext == ".zmv" || (len(ext) == 4 && strings.HasPrefix(ext, ".zm") && ext[3] >= '1' && ext[3] <= '9'):
			return "replays"
		case ext == ".s00" || ext == ".s01" || ext == ".zss":
			return "possible_states"
		case ext == ".rar":
			return "uninspected_packages"
		}
		if snesROMExtension(ext) {
			return "games"
		}
		if ext == ".zst" {
			return "states"
		}
		if len(ext) == 4 && strings.HasPrefix(ext, ".zs") && digits(ext[3:]) {
			return "states"
		}
	}
	return "unknown"
}

func isSNES(system string) bool {
	return system == "snes" || system == "sfc" || system == "superfamicom"
}

func snesROMExtension(ext string) bool {
	switch ext {
	case ".sfc", ".smc", ".swc", ".fig":
		return true
	default:
		return false
	}
}

func stateStem(base string) (string, bool) {
	for _, suffix := range []string{".quicksave", ".autosave", ".quickstate"} {
		if stem, ok := strings.CutSuffix(base, suffix); ok && stem != "" {
			return stem, true
		}
	}
	if marker := strings.LastIndex(base, ".state"); marker > 0 {
		suffix := strings.TrimPrefix(base[marker+len(".state"):], ".")
		if suffix == "" || suffix == "auto" || suffix == "autosave" || suffix == "quick" || digits(suffix) {
			return base[:marker], true
		}
	}
	return "", false
}

func digits(value string) bool {
	return value != "" && strings.Trim(value, "0123456789") == ""
}

func (item *group) finish() {
	for name := range item.paths {
		item.result.Paths = append(item.result.Paths, name)
	}
	sort.Strings(item.result.Paths)
	sort.Strings(item.result.EmptyFolders)
	sort.Strings(item.result.FoldersWithoutGames)
	sort.Strings(item.result.UnknownFiles)
	sort.Strings(item.result.UninspectedPackages)
	games := item.result.Games
	sort.Slice(games, func(i, j int) bool { return games[i].Path < games[j].Path })
	sort.Slice(item.related, func(i, j int) bool { return item.related[i].Path < item.related[j].Path })
	byStem := make(map[string][]int)
	for i, game := range games {
		base := strings.ToLower(path.Base(game.Path))
		stem := strings.TrimSuffix(base, path.Ext(base))
		byStem[stem] = append(byStem[stem], i)
	}
	for _, file := range item.related {
		candidates := matchingGames(file, byStem)
		if len(candidates) == 1 {
			game := &games[candidates[0]]
			game.Related = append(game.Related, file)
		} else {
			item.result.Unassociated = append(item.result.Unassociated, file)
		}
	}
}

func matchingGames(file Related, byStem map[string][]int) []int {
	base := strings.ToLower(path.Base(file.Path))
	stem := strings.TrimSuffix(base, path.Ext(base))
	if file.Category == "states" {
		if stateGame, ok := stateStem(base); ok {
			stem = stateGame
		}
	}
	candidates := byStem[stem]
	if file.Category == "images" || file.Category == "videos" {
		// If both the full stem and a conventional media suffix match games,
		// neither association is strong enough.
		for _, suffix := range []string{"-image", "-cover", "-boxart", "-video", "-screenshot"} {
			if gameStem, ok := strings.CutSuffix(stem, suffix); ok {
				candidates = append(append([]int(nil), candidates...), byStem[gameStem]...)
				break
			}
		}
	}
	return candidates
}
