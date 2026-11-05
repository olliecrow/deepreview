//go:build darwin || linux

package deepreview

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const maxArtifactBytes int64 = 8 << 20

func validPathSegment(id string) bool {
	if id == "" || id == "." || id == ".." || strings.Contains(id, "..") {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

func openArtifactRoot(root string) (*os.File, error) {
	cleaned := filepath.Clean(root)
	if cleaned == "" || !filepath.IsAbs(cleaned) || cleaned == string(filepath.Separator) {
		return nil, NewDeepReviewError("artifact root must be an absolute directory")
	}
	// O_NOFOLLOW applies to the final component. Callers pass a directory they
	// created. Later components are opened from that directory fd.
	fd, err := syscall.Open(cleaned, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), cleaned), nil
}

func artifactParts(rel string) ([]string, error) {
	cleaned := filepath.Clean(rel)
	if cleaned == "" || cleaned == "." || filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return nil, NewDeepReviewError("artifact path escapes its root")
	}
	parts := strings.Split(cleaned, string(filepath.Separator))
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, NewDeepReviewError("artifact path escapes its root")
		}
	}
	return parts, nil
}

func openFromRoot(root *os.File, rel string, finalFlags int) (*os.File, error) {
	parts, err := artifactParts(rel)
	if err != nil {
		return nil, err
	}
	current := root
	opened := make([]*os.File, 0, len(parts))
	defer func() {
		for _, file := range opened {
			_ = file.Close()
		}
	}()
	for i, part := range parts {
		flags := syscall.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
		last := i == len(parts)-1
		if last {
			flags |= finalFlags
		} else {
			flags |= syscall.O_DIRECTORY
		}
		fd, err := syscall.Openat(int(current.Fd()), part, flags, 0)
		if err != nil {
			return nil, err
		}
		file := os.NewFile(uintptr(fd), part)
		if !last {
			opened = append(opened, file)
			current = file
			continue
		}
		info, err := file.Stat()
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		if !info.Mode().IsRegular() {
			_ = file.Close()
			return nil, NewDeepReviewError("artifact is not a regular file")
		}
		if info.Size() > maxArtifactBytes {
			_ = file.Close()
			return nil, NewDeepReviewError("artifact exceeds %d bytes", maxArtifactBytes)
		}
		return file, nil
	}
	return nil, NewDeepReviewError("artifact path escapes its root")
}

func readFileFromRoot(root, rel string) ([]byte, error) {
	rootFile, err := openArtifactRoot(root)
	if err != nil {
		return nil, err
	}
	defer rootFile.Close()
	file, err := openFromRoot(rootFile, rel, syscall.O_NONBLOCK)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, NewDeepReviewError("file does not exist: %s", rel)
		}
		return nil, err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, maxArtifactBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxArtifactBytes {
		return nil, NewDeepReviewError("artifact exceeds %d bytes", maxArtifactBytes)
	}
	return body, nil
}

func writeFileAtomicFromRoot(root, rel string, data []byte, mode os.FileMode) error {
	if int64(len(data)) > maxArtifactBytes {
		return NewDeepReviewError("artifact exceeds %d bytes", maxArtifactBytes)
	}
	parts, err := artifactParts(rel)
	if err != nil {
		return err
	}
	rootFile, err := openArtifactRoot(root)
	if err != nil {
		return err
	}
	defer rootFile.Close()
	parent := rootFile
	opened := make([]*os.File, 0, len(parts))
	defer func() {
		for _, file := range opened {
			_ = file.Close()
		}
	}()
	for _, part := range parts[:len(parts)-1] {
		fd, err := syscall.Openat(int(parent.Fd()), part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if err != nil {
			return err
		}
		file := os.NewFile(uintptr(fd), part)
		opened = append(opened, file)
		parent = file
	}
	base := parts[len(parts)-1]
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	tmpName := ".dr-write-" + hex.EncodeToString(random[:])
	fd, err := syscall.Openat(int(parent.Fd()), tmpName, syscall.O_CREAT|syscall.O_EXCL|syscall.O_WRONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, uint32(mode.Perm()))
	if err != nil {
		return err
	}
	tmp := os.NewFile(uintptr(fd), tmpName)
	renamed := false
	defer func() {
		_ = tmp.Close()
		if !renamed {
			_ = syscall.Unlinkat(int(parent.Fd()), tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := syscall.Renameat(int(parent.Fd()), tmpName, int(parent.Fd()), base); err != nil {
		return err
	}
	renamed = true
	return nil
}

func copyBoundedArtifact(dstRoot, dstRel, srcRoot, srcRel string) error {
	body, err := readFileFromRoot(srcRoot, srcRel)
	if err != nil {
		return err
	}
	return writeFileAtomicFromRoot(dstRoot, dstRel, body, 0o644)
}
