// ТІ САМІ вектори, що в Go: backend/internal/social/bio_test.go і vps/internal/accounts/profile_test.go.
// [назва, вхід, очікуваний вихід, ok]
const rep = (s: string, n: number) => s.repeat(n)
export const BIO_VECTORS: [string, string, string, boolean][] = [
  ['звичайний', 'Люблю шаурму', 'Люблю шаурму', true],
  ['пробіли по краях', '  привіт  ', 'привіт', true],
  ['переноси → пробіл', 'рядок1\nрядок2\r\n\r\nрядок3', 'рядок1 рядок2 рядок3', true],
  ['табуляції й повтори', 'a\t\t b    c', 'a b c', true],
  ['порожнє', '', '', true],
  ['лише пробіли', '   \n\t ', '', true],
  ['bidi-override вирізається', 'abc\u202edef', 'abcdef', true],
  ['нульова ширина вирізається', 'a\u200bb\u200cc\ufeffd', 'abcd', true],
  ['керуючі символи', 'a\x00b\x07c\x1bd', 'abcd', true],
  ['лише невидиме → порожнє', '\u200b\u200d\u200d\u202e', '', true],
  ['ZWJ-емодзі зберігається', '👨\u200d👩\u200d👧', '👨\u200d👩\u200d👧', true],
  ['рівно 120 символів', rep('я', 120), rep('я', 120), true],
  ['121 символ', rep('я', 121), '', false],
  ['120 емодзі (по code point)', rep('😀', 120), rep('😀', 120), true],
  ['121 емодзі', rep('😀', 121), '', false],
  ['зальго обрізається до 2 міток', 'a' + rep('\u0301', 10), 'a\u0301\u0301', true],
  ['пробіли не рахуються до ліміту після очищення', rep(' ', 500) + 'ok', 'ok', true],
]
