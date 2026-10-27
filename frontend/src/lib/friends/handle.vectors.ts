// Спільні тестові вектори нормалізації handle: [вхід, очікуваний результат ('' = невалідний)].
// ТІ САМІ, що в vps/internal/accounts/handle_test.go, backend/internal/social/handle_test.go
// і worker/tests/test.mjs. Перевірка: node --experimental-strip-types frontend/scripts/check-handle.mjs
export const HANDLE_VECTORS: Array<[string, string]> = [
  ['bob', 'bob'], ['  @Bob_4821 ', 'bob_4821'], ['@@bob', ''], ['ab', ''],
  ['a'.repeat(32), 'a'.repeat(32)], ['a'.repeat(33), ''],
  ['bo..b', ''], ['bo__b', ''], ['.bob', ''], ['bob.', ''], ['_bob', ''], ['bob_', ''],
  ['b.o_b', 'b.o_b'], ['bob-1', ''], ['боб', ''], ['bob smith', ''], ['', ''],
]
