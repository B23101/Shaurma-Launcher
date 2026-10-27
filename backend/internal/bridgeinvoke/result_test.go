package bridgeinvoke

import (
	"errors"
	"reflect"
	"testing"
)

type tester struct{}

func (tester) GetAppDir() (string, error) { return "C:/test/dir", nil }
func (tester) GetNumbers() ([]int, error) { return []int{1, 2, 3}, nil }
func (tester) GetBool() (bool, error)     { return true, nil }
func (tester) GetNilList() ([]string, error) {
	return nil, nil
}
func (tester) GetFail() (string, error) { return "", errors.New("boom") }
func (tester) Void()                    {}
func (tester) OnlyError() error         { return nil }

// TestExtractResults — регресія на баг «(T, error) з nil error повертає
// null»: раніше nil-error через type assertion провалювався в гілку
// `result = v.Interface()` і ЗАТИРАВ справжній результат nil-ом.
func TestExtractResults(t *testing.T) {
	bt := tester{}
	v := reflect.ValueOf(bt)

	cases := []struct {
		method string
		want   any
	}{
		{"GetAppDir", "C:/test/dir"},
		{"GetNumbers", []int{1, 2, 3}},
		{"GetBool", true},
		// Типізований nil-слайс: JSON-маршалінг дасть null (що й очікує
		// фронтенд). Головне — результат НЕ має бути затертий untyped nil.
		{"GetNilList", []string(nil)},
	}
	for _, c := range cases {
		out := v.MethodByName(c.method).Call(nil)
		res, err := ExtractResults(out)
		if err != nil {
			t.Fatalf("%s: несподівана помилка: %v", c.method, err)
		}
		if !reflect.DeepEqual(res, c.want) {
			t.Errorf("%s: result = %#v, хотіли %#v", c.method, res, c.want)
		}
	}

	// Метод з помилкою: errVal має заповнитись, result не важливий.
	out := v.MethodByName("GetFail").Call(nil)
	res, err := ExtractResults(out)
	if err == nil {
		t.Errorf("GetFail: очікували помилку, отримали result=%#v", res)
	}

	// (error) з nil: помилки немає, результату немає — теж без паніки.
	out = v.MethodByName("OnlyError").Call(nil)
	if _, err := ExtractResults(out); err != nil {
		t.Errorf("OnlyError: несподівана помилка: %v", err)
	}

	// () — порожній виклик.
	out = v.MethodByName("Void").Call(nil)
	if _, err := ExtractResults(out); err != nil {
		t.Errorf("Void: несподівана помилка: %v", err)
	}
}
