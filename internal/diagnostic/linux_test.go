package diagnostic

import (
	"encoding/json"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func TestCollect(t *testing.T) {
	root := fstest.MapFS{
		"etc/os-release":                       {Data: []byte("ID=debian\nPRETTY_NAME=\"Debian \\\"Fixture\\\"\"\nVERSION_ID='13'\n")},
		"proc/sys/kernel/hostname":             {Data: []byte("fixture\n")},
		"proc/sys/kernel/osrelease":            {Data: []byte("6.x-fixture\n")},
		"proc/meminfo":                         {Data: []byte("MemTotal: 2048 kB\nMemAvailable: 0 kB\n")},
		"sys/firmware/devicetree/base/model":   {Data: []byte("Fixture board\x00")},
		"sys/class/power_supply/AC/type":       {Data: []byte("Mains\n")},
		"sys/class/power_supply/BAT0/type":     {Data: []byte("Battery\n")},
		"sys/class/power_supply/BAT0/present":  {Data: []byte("1\n")},
		"sys/class/power_supply/BAT0/status":   {Data: []byte("Discharging\n")},
		"sys/class/power_supply/BAT0/capacity": {Data: []byte("0\n")},
		"sys/class/power_supply/BAT1/type":     {Data: []byte("Battery\n")},
		"sys/class/power_supply/BAT1/present":  {Data: []byte("0\n")},
		"sys/class/block/mmcblk0/size":         {Data: []byte("8192\n")},
		"sys/class/block/mmcblk0/ro":           {Data: []byte("0\n")},
		"sys/class/block/mmcblk0p1/partition":  {Data: []byte("1\n")},
		"sys/class/block/mmcblk0p1/size":       {Data: []byte("1024\n")},
		"sys/class/block/mmcblk0p1/ro":         {Data: []byte("1\n")},
	}
	report := Collect(root, "arm64")
	if report.SchemaVersion != 1 {
		t.Fatalf("schema version = %d", report.SchemaVersion)
	}
	if report.System.Status != "ok" || report.Memory.Status != "ok" ||
		report.Batteries.Status != "ok" || report.Storage.Status != "ok" {
		t.Fatalf("unexpected section status: %+v", report)
	}
	wantSystem := System{"fixture", "arm64", "6.x-fixture", "debian", `Debian "Fixture"`, "13", "Fixture board"}
	if *report.System.Data != wantSystem {
		t.Errorf("system = %+v; want %+v", *report.System.Data, wantSystem)
	}
	memory := report.Memory.Data
	assertValue(t, memory.TotalBytes, uint64(2097152))
	assertValue(t, memory.AvailableBytes, uint64(0))

	batteries := *report.Batteries.Data
	if len(batteries) != 2 || batteries[0].Name != "BAT0" || batteries[0].Status != "Discharging" || batteries[1].Name != "BAT1" {
		t.Fatalf("batteries = %+v", batteries)
	}
	assertValue(t, batteries[0].Present, true)
	assertValue(t, batteries[0].CapacityPercent, uint64(0))
	assertValue(t, batteries[1].Present, false)
	if batteries[1].CapacityPercent != nil {
		t.Error("absent battery has a capacity reading")
	}

	devices := *report.Storage.Data
	if len(devices) != 2 || devices[0].Name != "mmcblk0" || devices[0].Kind != "device" ||
		devices[1].Name != "mmcblk0p1" || devices[1].Kind != "partition" {
		t.Fatalf("devices = %+v", devices)
	}
	assertValue(t, devices[0].SizeBytes, uint64(4194304))
	assertValue(t, devices[1].SizeBytes, uint64(524288))
	assertValue(t, devices[0].ReadOnly, false)
	assertValue(t, devices[1].ReadOnly, true)
}

func TestIncompleteSources(t *testing.T) {
	root := fstest.MapFS{
		"usr/lib/os-release":                   {Data: []byte("ID=debian\n")},
		"sys/class/dmi/id/product_name":        {Data: []byte("Fixture PC\n")},
		"proc/meminfo":                         {Data: []byte("MemTotal: 18446744073709551615 kB\nMemAvailable: 1 kB\n")},
		"sys/class/block/loop0/size":           {Data: []byte("18446744073709551615\n")},
		"sys/class/block/loop0/ro":             {Data: []byte("2\n")},
		"sys/class/power_supply/BAT0/type":     {Data: []byte("Battery\n")},
		"sys/class/power_supply/BAT0/status":   {Data: []byte("Unknown\n")},
		"sys/class/power_supply/BAT0/capacity": {Data: []byte("101\n")},
	}
	report := Collect(root, "amd64")
	if report.System.Status != "partial" || report.System.Data.OSID != "debian" ||
		report.System.Data.Model != "Fixture PC" || len(report.System.Issues) != 2 {
		t.Errorf("system = %+v, data = %+v", report.System, report.System.Data)
	}
	if report.Memory.Status != "partial" || report.Memory.Data.TotalBytes != nil {
		t.Errorf("invalid memory total was accepted: %+v", report.Memory)
	}
	assertValue(t, report.Memory.Data.AvailableBytes, uint64(1024))
	if report.Storage.Status != "partial" || len(report.Storage.Issues) != 2 ||
		(*report.Storage.Data)[0].SizeBytes != nil || (*report.Storage.Data)[0].ReadOnly != nil {
		t.Errorf("invalid storage data was accepted: %+v", report.Storage)
	}
	if report.Batteries.Status != "partial" || len(report.Batteries.Issues) != 1 ||
		(*report.Batteries.Data)[0].CapacityPercent != nil || (*report.Batteries.Data)[0].Present != nil {
		t.Errorf("unexpected battery diagnostics: %+v", report.Batteries)
	}
}

func TestJSONSeparatesEmptyFromUnavailable(t *testing.T) {
	root := fstest.MapFS{
		"sys/class/power_supply": {Mode: fs.ModeDir},
	}
	data, err := json.Marshal(Collect(root, "amd64"))
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`"schema_version":1`,
		`"batteries":{"status":"ok","data":[]}`,
		`"storage":{"status":"unavailable","issues":`,
		`"memory":{"status":"unavailable","issues":`,
	} {
		if !strings.Contains(string(data), fragment) {
			t.Errorf("report missing %s: %s", fragment, data)
		}
	}
}

func assertValue[T comparable](t *testing.T, value *T, want T) {
	t.Helper()
	if value == nil {
		t.Fatalf("field missing; want %v", want)
	}
	if *value != want {
		t.Errorf("field = %v; want %v", *value, want)
	}
}
