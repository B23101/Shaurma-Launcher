package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ErrUnsafePath = errors.New("unsafe installation path")

// installFS keeps each operation inside an opened installation root, including
// when a directory is replaced by a symlink between operations. Paths remain
// compatible with Runner's existing absolute/relative InstallDir bookkeeping.
type installFS struct {
	root *os.Root
	base string
}

func openInstallFS(base string) (*installFS, error) {
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	return &installFS{root: root, base: base}, nil
}
func (f *installFS) Close() error { return f.root.Close() }
func (f *installFS) rel(name string) (string, error) {
	rel, err := filepath.Rel(f.base, name)
	if err != nil || !filepath.IsLocal(rel) || rel == "." {
		return "", fmt.Errorf("%w: %q", ErrUnsafePath, name)
	}
	return rel, nil
}
func (f *installFS) Open(name string) (*os.File, error) {
	rel, err := f.rel(name)
	if err != nil {
		return nil, err
	}
	return f.root.Open(rel)
}
func (f *installFS) Create(name string) (*os.File, error) {
	rel, err := f.rel(name)
	if err != nil {
		return nil, err
	}
	return f.root.Create(rel)
}
func (f *installFS) Stat(name string) (os.FileInfo, error) {
	rel, err := f.rel(name)
	if err != nil {
		return nil, err
	}
	return f.root.Stat(rel)
}
func (f *installFS) OpenFile(name string, flag int, mode os.FileMode) (*os.File, error) {
	rel, err := f.rel(name)
	if err != nil {
		return nil, err
	}
	return f.root.OpenFile(rel, flag, mode)
}
func (f *installFS) Remove(name string) error {
	rel, err := f.rel(name)
	if err != nil {
		return err
	}
	return f.root.Remove(rel)
}
func (f *installFS) RemoveAll(name string) error {
	rel, err := f.rel(name)
	if err != nil {
		return err
	}
	return f.root.RemoveAll(rel)
}
func (f *installFS) MkdirAll(name string, mode os.FileMode) error {
	if filepath.Clean(name) == filepath.Clean(f.base) {
		return nil
	}
	rel, err := f.rel(name)
	if err != nil {
		return err
	}
	return f.root.MkdirAll(rel, mode)
}
func (f *installFS) Rename(old, next string) error {
	a, err := f.rel(old)
	if err != nil {
		return err
	}
	b, err := f.rel(next)
	if err != nil {
		return err
	}
	return f.root.Rename(a, b)
}
func (f *installFS) Truncate(name string, size int64) error {
	file, err := f.OpenFile(name, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Truncate(size)
}

func (f *installFS) WriteFile(name string, data []byte, mode os.FileMode) error {
	if err := f.Remove(name); err != nil && !os.IsNotExist(err) {
		return err
	}
	out, err := f.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, err = out.Write(data)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}
