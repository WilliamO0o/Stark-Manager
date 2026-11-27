package presentation

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/WilliamO0o/Stark-Manager/internal/diagnostic"
)

// WriteStorage groups observed destinations by block device or partition.
func WriteStorage(w io.Writer, report diagnostic.StorageReport) error {
	var out strings.Builder
	out.WriteString("ARMAZENAMENTO\n")
	mountsByBlock := make(map[string][]diagnostic.Mount)
	mountsByID := make(map[uint64]diagnostic.Mount)
	var unlinked []diagnostic.Mount
	if report.Mounts.Data != nil {
		for _, mount := range *report.Mounts.Data {
			mountsByID[mount.ID] = mount
			switch {
			case mount.Partition != nil:
				mountsByBlock[*mount.Partition] = append(mountsByBlock[*mount.Partition], mount)
			case mount.Device != nil:
				mountsByBlock[*mount.Device] = append(mountsByBlock[*mount.Device], mount)
			case usefulUnlinkedMount(mount):
				unlinked = append(unlinked, mount)
			}
		}
	}

	if report.Devices.Data != nil {
		writeBlockInventory(&out, *report.Devices.Data, mountsByBlock, report.Mounts.Status == "ok")
	}
	if len(unlinked) > 0 {
		out.WriteString("\nMONTAGENS SEM VÍNCULO COM DISPOSITIVO DE BLOCO\n")
		writeMountTargets(&out, unlinked, "  ")
	}

	out.WriteString("\nBINDS CONFIGURADOS\n")
	if report.Configured.Data != nil {
		for _, bind := range *report.Configured.Data {
			writeConfiguredBind(&out, bind, mountsByID)
		}
		if len(*report.Configured.Data) == 0 && report.Configured.Status == "ok" {
			out.WriteString("Nenhum bind configurado no fstab.\n")
		}
	}

	for _, issues := range [][]string{report.Devices.Issues, report.Mounts.Issues, report.Configured.Issues} {
		for _, issue := range issues {
			fmt.Fprintf(&out, "  Limitação: %s\n", issue)
		}
	}
	_, err := io.WriteString(w, out.String())
	return err
}

func writeBlockInventory(out *strings.Builder, devices []diagnostic.StorageDevice, mounts map[string][]diagnostic.Mount, completeMounts bool) {
	partitions := make(map[string][]diagnostic.StorageDevice)
	var top []diagnostic.StorageDevice
	for _, device := range devices {
		if hiddenBlock(device.Name) {
			continue
		}
		if device.ParentDisk != nil && !hiddenBlock(*device.ParentDisk) {
			parent := *device.ParentDisk
			partitions[parent] = append(partitions[parent], device)
		} else {
			top = append(top, device)
		}
	}
	if len(top) == 0 {
		out.WriteString("Nenhum disco ou partição para exibir.\n")
	}
	for _, device := range top {
		if device.Kind == "device" {
			slot := "desconhecido"
			if device.Slot != nil {
				slot = device.Slot.Name + " (" + device.Slot.Evidence + ")"
			}
			fmt.Fprintf(out, "Disco %s — %s | slot físico: %s\n", device.Name, byteSize(device.SizeBytes), slot)
			writeMountTargets(out, mounts[device.Name], "  ")
			for _, partition := range partitions[device.Name] {
				writePartition(out, partition, mounts[partition.Name], "  ", completeMounts)
			}
		} else if device.Kind == "partition" {
			writePartition(out, device, mounts[device.Name], "", completeMounts)
		} else {
			fmt.Fprintf(out, "Dispositivo %s — %s | tipo indeterminado\n", device.Name, byteSize(device.SizeBytes))
		}
	}
}

func writePartition(out *strings.Builder, partition diagnostic.StorageDevice, mounts []diagnostic.Mount, indent string, completeMounts bool) {
	fmt.Fprintf(out, "%sPartição %s — %s", indent, partition.Name, byteSize(partition.SizeBytes))
	if partition.ParentDisk == nil {
		out.WriteString(" | disco pai desconhecido")
	}
	out.WriteByte('\n')
	switch {
	case len(mounts) != 0:
		writeMountTargets(out, mounts, indent+"  ")
	case completeMounts:
		fmt.Fprintf(out, "%s  Sem montagem vinculada.\n", indent)
	default:
		fmt.Fprintf(out, "%s  Nenhuma montagem vinculada nos dados disponíveis.\n", indent)
	}
}

func writeMountTargets(out *strings.Builder, mounts []diagnostic.Mount, indent string) {
	for _, mount := range mounts {
		fmt.Fprintf(out, "%s→ %s (%s", indent, strconv.Quote(mount.Target), mount.Filesystem)
		if mount.Subtree {
			fmt.Fprintf(out, ", subárvore %s; origem de eventual bind indeterminada", strconv.Quote(mount.Root))
		}
		out.WriteString(")\n")
	}
}

func writeConfiguredBind(out *strings.Builder, bind diagnostic.ConfiguredBind, mounts map[uint64]diagnostic.Mount) {
	mode := "bind"
	if bind.Recursive {
		mode = "rbind"
	}
	if bind.NoAuto {
		mode += ", noauto"
	}
	status := "indeterminado"
	switch bind.Status {
	case diagnostic.BindCompatible:
		status = "compatível com a montagem ativa"
	case diagnostic.BindNotObserved:
		status = "sem montagem observada"
	case diagnostic.BindDifferent:
		status = "destino ocupado por outra origem"
	}
	fmt.Fprintf(out, "  %s → %s (%s): %s", strconv.Quote(bind.Source), strconv.Quote(bind.Target), mode, status)
	if bind.ActiveMountID != nil {
		if mount := mounts[*bind.ActiveMountID]; mount.Partition != nil {
			fmt.Fprintf(out, " | partição ativa: %s", *mount.Partition)
		}
	}
	if bind.Status == diagnostic.BindIndeterminate {
		fmt.Fprintf(out, " — %s", bind.Reason)
	}
	out.WriteByte('\n')
}

func hiddenBlock(name string) bool {
	return strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "zram")
}

func usefulUnlinkedMount(mount diagnostic.Mount) bool {
	if strings.HasPrefix(mount.MountSource, "/dev/loop") || strings.HasPrefix(mount.MountSource, "/dev/zram") {
		return false
	}
	if strings.HasPrefix(mount.MountSource, "/dev/") {
		return true
	}
	switch mount.Filesystem {
	case "nfs", "nfs4", "cifs", "smb3", "fuse.sshfs":
		return true
	}
	// Keep unexplained subtrees outside the kernel and service directories.
	return mount.Subtree && !strings.HasPrefix(mount.Target, "/proc/") &&
		!strings.HasPrefix(mount.Target, "/sys/") && !strings.HasPrefix(mount.Target, "/run/") &&
		!strings.HasPrefix(mount.Target, "/dev/")
}
