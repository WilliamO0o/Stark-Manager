package diagnostic

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"path"
	"strconv"
	"strings"
)

// Collect reads a Linux filesystem view rooted at /, or a test fixture with
// equivalent paths. architecture is the Go architecture of the collector.
func Collect(root fs.FS, architecture string) Report {
	return Report{
		SchemaVersion: 1,
		System:        collectSystem(root, architecture),
		Memory:        collectMemory(root),
		Batteries:     collectBatteries(root),
		Storage:       collectStorage(root),
	}
}

func section[T any](data T, issues []string) Section[T] {
	status := "ok"
	if len(issues) > 0 {
		status = "partial"
	}
	return Section[T]{Status: status, Data: &data, Issues: issues}
}

func unavailable[T any](err error) Section[T] {
	return Section[T]{Status: "unavailable", Issues: []string{err.Error()}}
}

func readText(root fs.FS, name string) (string, error) {
	data, err := fs.ReadFile(root, name)
	return strings.TrimSpace(string(data)), err
}

func textField(root fs.FS, name string, issues *[]string) string {
	value, err := readText(root, name)
	if err != nil {
		*issues = append(*issues, err.Error())
	}
	return value
}

func collectSystem(root fs.FS, architecture string) Section[System] {
	var issues []string
	system := System{
		Architecture:  architecture,
		Hostname:      textField(root, "proc/sys/kernel/hostname", &issues),
		KernelRelease: textField(root, "proc/sys/kernel/osrelease", &issues),
	}

	release, err := readText(root, "etc/os-release")
	if errors.Is(err, fs.ErrNotExist) {
		release, err = readText(root, "usr/lib/os-release")
	}
	if err != nil {
		issues = append(issues, err.Error())
	} else {
		for _, line := range strings.Split(release, "\n") {
			key, value, found := strings.Cut(line, "=")
			if !found {
				continue
			}
			switch key {
			case "ID":
				system.OSID = releaseValue(value)
			case "PRETTY_NAME":
				system.OSName = releaseValue(value)
			case "VERSION_ID":
				system.OSVersionID = releaseValue(value)
			}
		}
		if system.OSID == "" {
			issues = append(issues, "os-release: missing ID")
		}
	}
	for _, name := range []string{"sys/firmware/devicetree/base/model", "sys/class/dmi/id/product_name"} {
		model, err := readText(root, name)
		if err == nil {
			system.Model = strings.TrimSpace(strings.TrimRight(model, "\x00"))
			if system.Model != "" {
				break
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			issues = append(issues, err.Error())
		}
	}
	return section(system, issues)
}

func releaseValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 2 {
		return value
	}
	switch {
	case value[0] == '\'' && value[len(value)-1] == '\'':
		return value[1 : len(value)-1]
	case value[0] == '"' && value[len(value)-1] == '"':
		return strings.NewReplacer(`\\`, `\`, `\"`, `"`, `\$`, `$`, "\\`", "`").Replace(value[1 : len(value)-1])
	default:
		return value
	}
}

func collectMemory(root fs.FS) Section[Memory] {
	data, err := readText(root, "proc/meminfo")
	if err != nil {
		return unavailable[Memory](err)
	}
	var memory Memory
	var issues []string
	for _, line := range strings.Split(data, "\n") {
		key, value, _ := strings.Cut(line, ":")
		var target **uint64
		switch key {
		case "MemTotal":
			target = &memory.TotalBytes
		case "MemAvailable":
			target = &memory.AvailableBytes
		default:
			continue
		}
		fields := strings.Fields(value)
		if len(fields) != 2 || fields[1] != "kB" {
			continue
		}
		kib, err := strconv.ParseUint(fields[0], 10, 64)
		if err == nil && kib <= math.MaxUint64/1024 {
			bytes := kib * 1024
			*target = &bytes
		}
	}
	if memory.TotalBytes == nil {
		issues = append(issues, "proc/meminfo: missing or invalid MemTotal")
	}
	if memory.AvailableBytes == nil {
		issues = append(issues, "proc/meminfo: missing or invalid MemAvailable")
	}
	return section(memory, issues)
}

func collectBatteries(root fs.FS) Section[[]Battery] {
	const directory = "sys/class/power_supply"
	entries, err := fs.ReadDir(root, directory)
	if err != nil {
		return unavailable[[]Battery](err)
	}
	batteries := make([]Battery, 0)
	var issues []string
	for _, entry := range entries {
		base := path.Join(directory, entry.Name())
		if textField(root, path.Join(base, "type"), &issues) != "Battery" {
			continue
		}
		battery := Battery{
			Name:    entry.Name(),
			Present: boolField(root, path.Join(base, "present"), true, &issues),
		}
		if battery.Present == nil || *battery.Present {
			battery.Status = textField(root, path.Join(base, "status"), &issues)
			battery.CapacityPercent = numberField(root, path.Join(base, "capacity"), 1, 100, false, &issues)
		}
		batteries = append(batteries, battery)
	}
	return section(batteries, issues)
}

func collectStorage(root fs.FS) Section[[]BlockDevice] {
	const directory = "sys/class/block"
	entries, err := fs.ReadDir(root, directory)
	if err != nil {
		return unavailable[[]BlockDevice](err)
	}
	devices := make([]BlockDevice, 0, len(entries))
	var issues []string
	for _, entry := range entries {
		base := path.Join(directory, entry.Name())
		_, err := fs.Stat(root, path.Join(base, "partition"))
		kind := "device"
		switch {
		case err == nil:
			kind = "partition"
		case !errors.Is(err, fs.ErrNotExist):
			kind = ""
			issues = append(issues, err.Error())
		}

		// Linux reports block device size in 512-byte sectors.
		devices = append(devices, BlockDevice{
			Name:      entry.Name(),
			Kind:      kind,
			SizeBytes: numberField(root, path.Join(base, "size"), 512, math.MaxUint64/512, false, &issues),
			ReadOnly:  boolField(root, path.Join(base, "ro"), false, &issues),
		})
	}
	return section(devices, issues)
}

func boolField(root fs.FS, name string, optional bool, issues *[]string) *bool {
	number := numberField(root, name, 1, 1, optional, issues)
	if number == nil {
		return nil
	}
	value := *number == 1
	return &value
}

func numberField(root fs.FS, name string, scale, max uint64, optional bool, issues *[]string) *uint64 {
	raw, err := readText(root, name)
	if err != nil {
		if !optional || !errors.Is(err, fs.ErrNotExist) {
			*issues = append(*issues, err.Error())
		}
		return nil
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value > max {
		*issues = append(*issues, fmt.Sprintf("%s: expected an integer between 0 and %d", name, max))
		return nil
	}
	value *= scale
	return &value
}
