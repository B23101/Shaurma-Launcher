package trust

import (
	"regexp"
	"strconv"
	"strings"
)

var versionRe = regexp.MustCompile(`^[0-9]{1,6}(\.[0-9]{1,6}){1,3}$`)

// ValidVersion — версія лаунчера у формі X.Y[.Z[.W]] без префікса й суфіксів.
func ValidVersion(v string) bool { return versionRe.MatchString(v) }

// CompareVersions — семверне порівняння всіх сегментів (той самий контракт, що
// manifest.CompareVersions в оновлювачі): «2.1.5.0» == «2.1.5».
func CompareVersions(v1, v2 string) int {
	p1, p2 := splitVersion(v1), splitVersion(v2)
	n := len(p1)
	if len(p2) > n {
		n = len(p2)
	}
	for i := 0; i < n; i++ {
		a, b := 0, 0
		if i < len(p1) {
			a = p1[i]
		}
		if i < len(p2) {
			b = p2[i]
		}
		if a != b {
			if a > b {
				return 1
			}
			return -1
		}
	}
	return 0
}

func splitVersion(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v = strings.SplitN(v, "-", 2)[0]
	var out []int
	for _, s := range strings.Split(v, ".") {
		n, err := strconv.Atoi(s)
		if err != nil {
			n = 0
		}
		out = append(out, n)
	}
	return out
}
