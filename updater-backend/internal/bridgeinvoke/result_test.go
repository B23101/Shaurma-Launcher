package bridgeinvoke

import (
	"errors"
	"reflect"
	"testing"
)

type tester struct{}

func (tester) GetName() (string, error) { return "Shaurma", nil }
func (tester) GetNums() ([]int, error)  { return []int{7, 8}, nil }
func (tester) GetFail() (string, error) { return "", errors.New("boom") }
func (tester) Void()                    {}

func TestExtractResults(t *testing.T) {
	bt := tester{}
	v := reflect.ValueOf(bt)

	cases := []struct {
		method string
		want   any
	}{
		{"GetName", "Shaurma"},
		{"GetNums", []int{7, 8}},
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

	out := v.MethodByName("GetFail").Call(nil)
	if _, err := ExtractResults(out); err == nil {
		t.Error("GetFail: очікували помилку")
	}

	out = v.MethodByName("Void").Call(nil)
	if _, err := ExtractResults(out); err != nil {
		t.Errorf("Void: несподівана помилка: %v", err)
	}
}
