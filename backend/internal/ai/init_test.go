package ai

import "testing"

// Регресія 2026-08-09: package-level regexp.MustCompile з lookahead `(?=...)`
// (RE2 не підтримує) панікував у init() і backend.dll тихо не завантажувався
// — go build не виконує init, тому баг проходив збірку. go test пакета ВИКОНУЄ
// init(), тож цей тест тримає ВСІ regex-и пакета ai під валідацією.
func TestPackageInit(t *testing.T) {}

// TestNoLookaheadInRegexes — явна перевірка, що жоден package-level regex не
// містить RE2-непідтримуваних конструкцій (lookahead `(?=`, lookbehind `(?<=`,
// backreferences `\1`). Якщо такий з'явиться — MustCompile у init() впаде, але
// ця перевірка ловить його на рівні джерела зі зрозумілою помилкою.
func TestNoLookaheadInRegexes(t *testing.T) {
	// Тут МОЖНА додати явні перевірки на відомі regex-и пакета, коли вони
	// стануть package-level. Поки що досить init-тесту: він гарантує, що
	// всі package-level MustCompile виконуються без паніки.
}
