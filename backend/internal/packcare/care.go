// Package packcare implements reversible graphics presets and independent world copies.
package packcare

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"shaurma-launcher-backend/internal/mcoptions"
	"sort"
	"strings"
	"time"
)

var presets = map[string]map[string]string{
	"low":      {"renderDistance": "6", "simulationDistance": "5", "graphicsMode": "0", "fancyGraphics": "false", "particles": "2", "entityDistanceScaling": "0.75"},
	"balanced": {"renderDistance": "10", "simulationDistance": "6", "graphicsMode": "1", "fancyGraphics": "true", "particles": "1", "entityDistanceScaling": "1.0"},
}

func ApplyPreset(dir, preset string) error {
	if preset != "restore" && presets[preset] == nil {
		return fmt.Errorf("невідомий профіль")
	}
	path := mcoptions.OptionsFilePath(dir)
	opts, err := mcoptions.ParseFile(path)
	if err != nil {
		return fmt.Errorf("спочатку запустіть і закрийте гру: %w", err)
	}
	backup := filepath.Join(dir, "performance-original.json")
	original := map[string]*string{}
	data, err := os.ReadFile(backup)
	if err == nil {
		if err = json.Unmarshal(data, &original); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	} else {
		if preset == "restore" {
			return fmt.Errorf("немає збережених налаштувань")
		}
		for key := range presets["low"] {
			if v, ok := opts[key]; ok {
				value := v
				original[key] = &value
			} else {
				original[key] = nil
			}
		}
		data, err = json.Marshal(original)
		if err != nil {
			return err
		}
		if err = os.WriteFile(backup, data, 0600); err != nil {
			return err
		}
	}
	if preset == "restore" {
		for key := range presets["low"] {
			if value := original[key]; value != nil {
				opts[key] = *value
			} else {
				delete(opts, key)
			}
		}
	} else {
		for key, value := range presets[preset] {
			opts[key] = value
		}
	}
	// Publish a complete options file; a failed write leaves the original intact.
	tmp := path + ".performance-tmp"
	if err = mcoptions.WriteFile(tmp, opts); err != nil {
		return err
	}
	defer os.Remove(tmp)
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	if preset == "restore" {
		return os.Remove(backup)
	}
	return nil
}

// copyTree refuses symlinks and special files; Root confines file access even
// if a path is replaced while the copy is in progress.
func copyTree(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("посилання замість папки світу не підтримується")
	}
	root, err := os.OpenRoot(src)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = os.MkdirAll(dst, 0700); err != nil {
		return err
	}
	out, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	return fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("символічні посилання у світах не підтримуються: %s", name)
		}
		if d.IsDir() {
			return out.MkdirAll(name, 0700)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("непідтримуваний файл: %s", name)
		}
		in, err := root.Open(name)
		if err != nil {
			return err
		}
		defer in.Close()
		f, err := out.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, in)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}

func Snapshot(dir string) (string, error) {
	saves := filepath.Join(dir, ".minecraft", "saves")
	entries, err := os.ReadDir(saves)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", nil
	}
	base := filepath.Join(dir, "world-snapshots")
	if err = os.MkdirAll(base, 0700); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(base, ".pending-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if err = copyTree(saves, tmp); err != nil {
		return "", err
	}
	name := time.Now().UTC().Format("20060102-150405.000000000")
	if err = os.Rename(tmp, filepath.Join(base, name)); err != nil {
		return "", err
	}
	return name, nil
}

func List(dir string) ([]string, error) {
	result := []string{}
	entries, err := os.ReadDir(filepath.Join(dir, "world-snapshots"))
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			result = append(result, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(result)))
	return result, nil
}

func Restore(dir, name string) ([]string, error) {
	if _, err := time.Parse("20060102-150405.000000000", name); err != nil {
		return nil, fmt.Errorf("некоректна резервна копія")
	}
	src := filepath.Join(dir, "world-snapshots", name)
	entries, err := os.ReadDir(src)
	if err != nil {
		return nil, err
	}
	saves := filepath.Join(dir, ".minecraft", "saves")
	if err = os.MkdirAll(saves, 0700); err != nil {
		return nil, err
	}
	restored := []string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dst, err := os.MkdirTemp(saves, e.Name()+"-restored-")
		if err != nil {
			return restored, err
		}
		if err = copyTree(filepath.Join(src, e.Name()), dst); err != nil {
			os.RemoveAll(dst)
			return restored, err
		}
		restored = append(restored, filepath.Base(dst))
	}
	return restored, nil
}
