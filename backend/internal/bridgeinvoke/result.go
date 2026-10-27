// Package bridgeinvoke містить чисту (без cgo) логіку розпакування
// результатів reflect-виклику методу — щоб її можна було юніт-тестувати
// незалежно від cgo-обгортки backend.dll.
package bridgeinvoke

import "reflect"

// ExtractResults розпаковує повернені значення методу (як повертає
// reflect.Value.Call) у пару (result, errVal).
//
// Конвенція підтримуваних сигнатур: (T) | (T, error) | (error) | ().
//
// ВАЖЛИВО (виправлення «всі методи повертають null»): error-значення
// розпізнається за reflect-типом (v.Type().Implements(errorType)), а НЕ
// через type assertion v.Interface().(error). Для nil-error v.Interface()
// повертає ГОЛИЙ nil interface{}, assertion nil.(error) невдалий, і код
// падав у гілку `result = v.Interface()`, перезаписуючи справжній
// результат (рядок/слайс/булеан) значенням nil. Це робило КОЖЕН метод з
// сигнатурою (T, error) безпомилковим «повертачем null»: StartMSLogin →
// null (OpenExternal(null) → Провідник C:), GetPackAsset → null (жодних
// картинок), GetCustomPackMCVersions → null (версії гри не вантажаться),
// BrowserSearch → null (браузер контенту порожній). Підтверджено
// стендалон-тестом: до виправлення GetAppDir повертало nil, після — "C:/dir".
func ExtractResults(out []reflect.Value) (result any, errVal error) {
	errType := reflect.TypeOf((*error)(nil)).Elem()
	for _, v := range out {
		if v.Type().Implements(errType) {
			// IsNil дозволений лише для nilable-видів (interface/pointer/
			// slice/map/chan/func). Контракт використовує error-інтерфейс,
			// тож тут завжди Interface, але guard не дає паніці статися,
			// якщо метод колись поверне конкретний тип, що реалізує error.
			switch v.Kind() {
			case reflect.Interface, reflect.Pointer, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func:
				if !v.IsNil() {
					errVal = v.Interface().(error)
				}
			default:
				errVal = v.Interface().(error)
			}
			continue
		}
		result = v.Interface()
	}
	return result, errVal
}
