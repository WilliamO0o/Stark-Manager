package diagnostic

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"strings"
)

type mountTree struct {
	root     *Mount
	children map[uint64][]*Mount
	mounts   []Mount
}

func buildMountTree(mounts []Mount) mountTree {
	tree := mountTree{children: make(map[uint64][]*Mount), mounts: mounts}
	ids := make(map[uint64]bool, len(mounts))
	for _, mount := range mounts {
		ids[mount.ID] = true
	}
	for i := range mounts {
		mount := &mounts[i]
		if mount.Target == "/" && (mount.ParentID == mount.ID || !ids[mount.ParentID]) {
			if tree.root != nil {
				return mountTree{}
			}
			tree.root = mount
			continue
		}
		tree.children[mount.ParentID] = append(tree.children[mount.ParentID], mount)
	}
	return tree
}

func within(name, base string) bool {
	return name == base || strings.HasPrefix(name, strings.TrimSuffix(base, "/")+"/")
}

// visible walks parent IDs along the requested path, taking overmounts before
// their descendants. A more specific path beneath a hidden mount is invisible.
func (tree mountTree) visible(name string) *Mount {
	current := tree.root
	for steps := 0; current != nil && steps < len(tree.mounts); steps++ {
		var next *Mount
		ambiguous := false
		for _, child := range tree.children[current.ID] {
			if !within(name, child.Target) || !within(child.Target, current.Target) {
				continue
			}
			switch {
			case next == nil || len(child.Target) < len(next.Target):
				next, ambiguous = child, false
			case child.Target == next.Target:
				ambiguous = true
			}
		}
		if ambiguous {
			return nil
		}
		if next == nil {
			return current
		}
		current = next
	}
	return nil
}

var errUnverifiedPath = errors.New("caminho com link simbólico, automontagem ou topologia ambígua")

// CheckMountedPath checks a clean absolute path without following symlinks
// or walking beneath autofs.
func CheckMountedPath(mountAt func(string) *Mount, name string, lstat func(string) (fs.FileInfo, error)) (fs.FileInfo, error) {
	current := "/"
	var last fs.FileInfo
	for _, component := range strings.Split(strings.TrimPrefix(name, "/"), "/") {
		current = path.Join(current, component)
		mount := mountAt(current)
		if mount == nil || mount.Filesystem == "autofs" {
			return nil, errUnverifiedPath
		}
		info, err := lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, errUnverifiedPath
		}
		last = info
	}
	return last, nil
}

func comparisonPath(name string) (string, bool) {
	if !validAbsolute(name) {
		return "", false
	}
	for _, component := range strings.Split(name, "/") {
		if component == ".." {
			return "", false
		}
	}
	return path.Clean(name), true
}

func compareBind(bind *ConfiguredBind, tree mountTree, lstat func(string) (fs.FileInfo, error)) {
	bind.Status = BindIndeterminate
	if tree.root == nil || lstat == nil {
		bind.Reason = "visão das montagens indisponível, incompleta ou ambígua"
		return
	}
	target, ok := comparisonPath(bind.Target)
	if !ok {
		bind.Reason = "destino configurado não é um caminho absoluto verificável"
		return
	}
	actual := tree.visible(target)
	if actual == nil {
		bind.Reason = "a montagem visível no destino é ambígua"
		return
	}
	targetInfo, err := CheckMountedPath(tree.visible, target, lstat)
	if actual.Target != target && (err == nil || errors.Is(err, fs.ErrNotExist)) {
		bind.Status, bind.Reason = BindNotObserved, "nenhuma montagem visível no destino"
		return
	}
	if err != nil {
		bind.Reason = "não foi possível verificar o caminho do destino: " + err.Error()
		return
	}
	bind.ActiveMountID = &actual.ID
	source, ok := comparisonPath(bind.Source)
	if !ok || within(source, target) {
		bind.Reason = "origem configurada inválida ou encoberta pelo próprio destino"
		return
	}
	sourceInfo, err := CheckMountedPath(tree.visible, source, lstat)
	if err != nil {
		bind.Reason = "não foi possível verificar o caminho da origem: " + err.Error()
		return
	}
	base := tree.visible(source)
	if base.Filesystem == "nsfs" || actual.Filesystem == "nsfs" {
		bind.Reason = "montagem de namespace sem raiz de caminho comparável"
		return
	}
	relative := strings.TrimPrefix(source, base.Target)
	expectedRoot := path.Join(base.Root, relative)
	if base.MajorMinor != actual.MajorMinor || base.Filesystem != actual.Filesystem || expectedRoot != actual.Root {
		// Case folding and hard links can give the same file two names.
		if os.SameFile(sourceInfo, targetInfo) {
			bind.Reason = "os caminhos apontam para o mesmo objeto, mas a raiz registrada diverge"
			return
		}
		bind.Status, bind.Reason = BindDifferent, "a montagem ativa aponta para outro sistema de arquivos ou outra raiz"
		return
	}
	if bind.Recursive {
		for _, mount := range tree.mounts {
			if (mount.Target != source && within(mount.Target, source)) ||
				(mount.Target != target && within(mount.Target, target)) {
				bind.Reason = "raiz compatível, mas as submontagens do rbind não foram confirmadas"
				return
			}
		}
	}
	bind.Status, bind.Reason = BindCompatible, "major:minor, tipo e raiz coincidem com a origem configurada"
}
