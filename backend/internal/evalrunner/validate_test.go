package evalrunner

import (
	"strings"
	"testing"
)

func TestStrictJSONRejectsInvalidUTF8(t *testing.T) {
	if _, err := decodeJSON([]byte{'"', 0xff, '"'}, "string"); err == nil {
		t.Fatal("invalid UTF-8 accepted as a replacement character")
	}
}

func TestFiniteJSONTypes(t *testing.T) {
	// Raw input is 6002 bytes; canonical HTML escaping expands it to 36002.
	if got, err := decodeJSON([]byte(`"`+strings.Repeat("<", 6000)+`"`), "string"); err == nil {
		t.Errorf("canonical JSON escaped the IO ceiling: %d bytes", len(got))
	}
	for _, tc := range []struct{ typ, input, want string }{
		{"bool", "true", "true"}, {"string", `"quoted"`, `"quoted"`}, {"int", "-9223372036854775808", "-9223372036854775808"},
		{"int64", "9223372036854775807", "9223372036854775807"}, {"float64", "1.5", "1.5"},
		{"[]bool", "[true,false]", "[true,false]"}, {"[]string", `["a","b"]`, `["a","b"]`},
		{"[]int", "[0,-1]", "[0,-1]"}, {"[]int64", "[9223372036854775807]", "[9223372036854775807]"}, {"[]float64", "[]", "[]"},
	} {
		t.Run(tc.typ, func(t *testing.T) {
			got, err := decodeJSON([]byte(tc.input), tc.typ)
			if err != nil || string(got) != tc.want {
				t.Fatalf("got %s %v; want %s", got, err, tc.want)
			}
		})
	}
	for _, tc := range []struct{ typ, input string }{{"[]int", "[1,null]"}, {"[]string", "[1]"}, {"bool", "0"}, {"int", "1e0"}, {"float64", "1e999"}, {"string", `"a" garbage`}} {
		if _, err := decodeJSON([]byte(tc.input), tc.typ); err == nil {
			t.Errorf("accepted %s as %s", tc.input, tc.typ)
		}
	}
}
