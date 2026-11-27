// Package diagnostic collects read-only Linux diagnostics.
package diagnostic

// Report is the versioned JSON contract for a local diagnostic.
type Report struct {
	SchemaVersion int                    `json:"schema_version"`
	System        Section[System]        `json:"system"`
	Memory        Section[Memory]        `json:"memory"`
	Batteries     Section[[]Battery]     `json:"batteries"`
	Storage       Section[[]BlockDevice] `json:"storage"`
}

// Section distinguishes complete, partial and unavailable results.
// An available empty list means that no matching devices were found.
type Section[T any] struct {
	Status string   `json:"status"`
	Data   *T       `json:"data,omitempty"`
	Issues []string `json:"issues,omitempty"`
}

type System struct {
	Hostname      string `json:"hostname,omitempty"`
	Architecture  string `json:"architecture"`
	KernelRelease string `json:"kernel_release,omitempty"`
	OSID          string `json:"os_id,omitempty"`
	OSName        string `json:"os_name,omitempty"`
	OSVersionID   string `json:"os_version_id,omitempty"`
	Model         string `json:"model,omitempty"`
}

type Memory struct {
	TotalBytes     *uint64 `json:"total_bytes,omitempty"`
	AvailableBytes *uint64 `json:"available_bytes,omitempty"`
}

type Battery struct {
	Name            string  `json:"name"`
	Status          string  `json:"status,omitempty"`
	CapacityPercent *uint64 `json:"capacity_percent,omitempty"`
	Present         *bool   `json:"present,omitempty"`
}

type BlockDevice struct {
	Name      string  `json:"name"`
	Kind      string  `json:"kind,omitempty"`
	SizeBytes *uint64 `json:"size_bytes,omitempty"`
	ReadOnly  *bool   `json:"read_only,omitempty"`
}
