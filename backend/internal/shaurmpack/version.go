package shaurmpack

import (
	"fmt"
	"strconv"
	"strings"
)

// CompareVersions порівнює дві версії (semver або date-based).
// Повертає:
//   - 1 якщо a > b
//   - -1 якщо a < b
//   - 0 якщо a == b
func CompareVersions(a, b string) int {
	// Спроба semver порівняння
	if av, bv, ok := parseSemver(a, b); ok {
		return compareSemver(av, bv)
	}
	// Fallback: строкове порівняння
	if a > b {
		return 1
	}
	if a < b {
		return -1
	}
	return 0
}

// parseSemver парсить дві версії як [major, minor, patch].
func parseSemver(a, b string) ([3]int, [3]int, bool) {
	var av, bv [3]int
	a = strings.TrimPrefix(a, "v")
	b = strings.TrimPrefix(b, "v")

	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")
	if len(partsA) < 1 || len(partsB) < 1 {
		return av, bv, false
	}

	for i := 0; i < 3; i++ {
		var na, nb int
		if i < len(partsA) {
			na, _ = strconv.Atoi(partsA[i])
		}
		if i < len(partsB) {
			nb, _ = strconv.Atoi(partsB[i])
		}
		av[i] = na
		bv[i] = nb
	}
	return av, bv, true
}

func compareSemver(a, b [3]int) int {
	for i := 0; i < 3; i++ {
		if a[i] > b[i] {
			return 1
		}
		if a[i] < b[i] {
			return -1
		}
	}
	return 0
}

// FormatLauncherVersion повертає поточну версію лаунчера.
// Фактична версія підставляється з build-time ldflags.
var CurrentLauncherVersion = "dev"
var CurrentLauncherBuild = 0

// LauncherBuildInfo повертає LauncherMeta з поточною версією.
func LauncherBuildInfo() LauncherMeta {
	return LauncherMeta{
		Name:    "Shaurma Launcher",
		Version: CurrentLauncherVersion,
		Build:   CurrentLauncherBuild,
	}
}

// ValidateManifest перевіряє коректність маніфесту.
func ValidateManifest(m *Manifest) error {
	if m == nil {
		return fmt.Errorf("маніфест порожній")
	}
	if m.Version < 1 || m.Version > FormatVersion {
		return fmt.Errorf("непідтримувана версія формату: %d", m.Version)
	}
	if m.PackID == "" {
		return fmt.Errorf("packId порожній")
	}
	if m.Name == "" {
		return fmt.Errorf("name порожній")
	}
	if m.GameVersion == "" {
		return fmt.Errorf("gameVersion порожній")
	}
	if m.Loader == "" {
		return fmt.Errorf("loader порожній")
	}
	return nil
}
