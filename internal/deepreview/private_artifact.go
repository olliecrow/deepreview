package deepreview

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	privateDirMode  = 0o700
	privateFileMode = 0o600
)

func mkdirPrivate(path string) error {
	cleaned := filepath.Clean(path)
	if cleaned == "." || cleaned == string(os.PathSeparator) {
		return errors.New("private artifact directory is required")
	}
	if err := os.MkdirAll(cleaned, privateDirMode); err != nil {
		return err
	}
	return chmodOwnedDirectory(cleaned)
}

func chmodOwnedDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("private artifact path is a symlink")
	}
	if !info.IsDir() {
		return errors.New("private artifact path is not a directory")
	}
	return os.Chmod(path, privateDirMode)
}

func writePrivateFile(path string, data []byte) error {
	file, err := openPrivateFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(data)
	return err
}

func openPrivateFile(path string, flag int) (*os.File, error) {
	if err := mkdirPrivate(filepath.Dir(path)); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, flag|unix.O_NOFOLLOW, privateFileMode)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, errors.New("private artifact path is a symlink")
		}
		return nil, err
	}
	if err := file.Chmod(privateFileMode); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func chmodRegularPrivateFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("private artifact path is a symlink")
	}
	if !info.Mode().IsRegular() {
		return errors.New("private artifact path is not a regular file")
	}
	return os.Chmod(path, privateFileMode)
}

type discardWriteCloser struct{}

func (discardWriteCloser) Write(p []byte) (int, error) { return len(p), nil }

func (discardWriteCloser) Close() error { return nil }
