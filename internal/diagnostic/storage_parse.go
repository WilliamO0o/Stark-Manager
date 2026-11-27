package diagnostic

import (
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
)

func readRecords[T any](root fs.FS, name string, parse func(string, int) (*T, error)) Section[[]T] {
	data, readErr := fs.ReadFile(root, name)
	if readErr != nil && len(data) == 0 {
		return unavailable[[]T](readErr)
	}
	records := make([]T, 0)
	var issues []string
	if readErr != nil {
		issues = append(issues, readErr.Error())
	}
	for index, line := range strings.Split(string(data), "\n") {
		record, err := parse(line, index+1)
		if err != nil {
			issues = append(issues, fmt.Sprintf("%s:%d: %v", name, index+1, err))
		} else if record != nil {
			records = append(records, *record)
		}
	}
	return section(records, issues)
}

var pathEscapes = strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)

func majorMinor(value string) (string, bool) {
	left, right, found := strings.Cut(value, ":")
	major, majorErr := strconv.ParseUint(left, 10, 32)
	minor, minorErr := strconv.ParseUint(right, 10, 32)
	if !found || majorErr != nil || minorErr != nil {
		return "", false
	}
	return fmt.Sprintf("%d:%d", major, minor), true
}

func newMountParser() func(string, int) (*Mount, error) {
	ids := make(map[uint64]bool)
	return func(line string, _ int) (*Mount, error) {
		fields := strings.FieldsFunc(line, func(r rune) bool { return r == ' ' })
		if len(fields) == 0 {
			return nil, nil
		}
		if len(fields) < 10 {
			return nil, fmt.Errorf("registro de montagem incompleto")
		}
		// Only search the optional-field region, since a source may be "-".
		optionalCount := slices.Index(fields[6:], "-")
		sep := 6 + optionalCount
		if optionalCount < 0 || len(fields) != sep+4 {
			return nil, fmt.Errorf("separador ou campos de montagem inválidos")
		}
		id, idErr := strconv.ParseUint(fields[0], 10, 64)
		parent, parentErr := strconv.ParseUint(fields[1], 10, 64)
		dev, ok := majorMinor(fields[2])
		filesystem := fields[sep+1]
		root, rootOK := mountPath(fields[3])
		if filesystem == "nsfs" && namespaceRoot(fields[3]) {
			root, rootOK = fields[3], true
		}
		target, targetOK := mountPath(fields[4])
		if idErr != nil || parentErr != nil || id == 0 || ids[id] || !ok ||
			!rootOK || !targetOK {
			return nil, fmt.Errorf("identificador ou caminho de montagem inválido")
		}
		ids[id] = true
		return &Mount{
			ID: id, ParentID: parent, MajorMinor: dev, Root: root, Target: target,
			Options: strings.Split(fields[5], ","), OptionalFields: fields[6:sep],
			Filesystem: filesystem, MountSource: pathEscapes.Replace(fields[sep+2]),
			SuperOptions: strings.Split(fields[sep+3], ","), Subtree: root != "/" && strings.HasPrefix(root, "/"),
		}, nil
	}
}

func namespaceRoot(value string) bool {
	id, ok := strings.CutPrefix(value, "mnt:[")
	if !ok || !strings.HasSuffix(id, "]") {
		return false
	}
	_, err := strconv.ParseUint(strings.TrimSuffix(id, "]"), 10, 64)
	return err == nil
}

func parseFstabBind(line string, number int) (*ConfiguredBind, error) {
	// Keep literal Unicode whitespace in filenames.
	fields := strings.FieldsFunc(strings.TrimSuffix(line, "\r"), func(r rune) bool { return r == ' ' || r == '\t' })
	for index, field := range fields {
		if strings.HasPrefix(field, "#") {
			fields = fields[:index]
			break
		}
	}
	if len(fields) == 0 {
		return nil, nil
	}
	if len(fields) < 3 || len(fields) > 6 {
		return nil, fmt.Errorf("registro fstab inválido")
	}
	for _, field := range fields[min(4, len(fields)):] {
		if _, err := strconv.ParseUint(field, 10, 32); err != nil {
			return nil, fmt.Errorf("campo numérico fstab inválido")
		}
	}
	if len(fields) == 3 {
		return nil, nil
	}
	options := strings.Split(fields[3], ",")
	recursive := slices.Contains(options, "rbind")
	if !recursive && !slices.Contains(options, "bind") {
		return nil, nil
	}
	return &ConfiguredBind{
		Line: number, Source: pathEscapes.Replace(fields[0]), Target: pathEscapes.Replace(fields[1]),
		Options: options, Recursive: recursive, NoAuto: slices.Contains(options, "noauto"),
	}, nil
}

func mountPath(raw string) (string, bool) {
	for rest := raw; ; {
		_, suffix, found := strings.Cut(rest, "\\")
		if !found {
			break
		}
		if len(suffix) < 3 {
			return "", false
		}
		switch suffix[:3] {
		case "040", "011", "012", "134":
			rest = suffix[3:]
		default:
			return "", false
		}
	}
	decoded := pathEscapes.Replace(raw)
	return decoded, validAbsolute(decoded)
}

func validAbsolute(value string) bool {
	return strings.HasPrefix(value, "/") && !strings.ContainsRune(value, 0)
}
