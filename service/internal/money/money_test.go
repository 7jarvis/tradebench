package money

import "testing"

func TestParse(t *testing.T) {
	valid := map[string]Cents{
		"0":       0,
		"1":       100,
		"1.5":     150,
		"1.05":    105,
		"1234.50": 123450,
	}
	for in, want := range valid {
		got, err := Parse(in)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %v, %v; want %v", in, got, err, want)
		}
	}

	for _, in := range []string{"", "-1", "+1", "1.", ".5", "1.234", "1e3", "abc", "1,50", " 1"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) expected error", in)
		}
	}
}

func TestString(t *testing.T) {
	cases := map[Cents]string{0: "0.00", 5: "0.05", 150: "1.50", -150: "-1.50", 123456: "1234.56"}
	for in, want := range cases {
		if got := in.String(); got != want {
			t.Errorf("%d.String() = %q; want %q", in, got, want)
		}
	}
}
