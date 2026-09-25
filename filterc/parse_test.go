package filterc

import "testing"

func TestParseLowers(t *testing.T) {
	cases := []struct{ src, want string }{
		{"iv >= 90", "iv{[90,100]}"},
		{"90 <= iv", "iv{[90,100]}"},
		{"cp > 1500", "cp{[1501,32767]}"},
		{"iv != 50", "iv{[-1,49],[51,100]}"},
		{"iv > 100", "iv{}"},
		{"iv in -1..100", "iv{[-1,100]}"},
		{"level not in 1..29", "not(level{[1,29]})"},
		{"gender in [3, 1]", "gender{[1,1],[3,3]}"},
		{"gender in []", "gender{}"},
		{"pokemon in [4, 1, 4]", "pokemon{1,4}"},
		{"pokemon != 710", "!pokemon{710}"},
		{"pokemon not in [1, 4]", "not(pokemon{1,4})"},
		{"pokemon == 1 and form == 0", "and(pokemon{1},form{0})"},
		{"!(iv >= 90) || great <= 100 && cp == 1500", "or(not(iv{[90,100]}),and(great{[1,100]},cp{[1500,1500]}))"},
		{"not (great <= 100)", "not(great{[1,100]})"},
		{"great == 4096", "great{[4096,4096]}"},
	}
	for _, c := range cases {
		n, err := parse(c.src)
		if err != nil {
			t.Errorf("%q: %v", c.src, err)
			continue
		}
		if got := format(n); got != c.want {
			t.Errorf("%q: lowered to %s, want %s", c.src, got, c.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{"iv >= 90 &&", "1:11: unexpected token EOF"},
		{"x == 1", `1:1: unknown field "x"`},
		{"iv == 1.5", "1:7: expected an integer, got 1.5"},
		{"iv.foo == 1", "1:4: expected a field on one side of =="},
		{"iv >= level", "1:7: expected an integer, got level"},
		{"5 == 6", "1:1: expected a field on one side of =="},
		{"iv", "1:1: expected a comparison"},
		{"not great <= 100", "1:1: not binds tighter than <=; write not (…)"},
		{"pokemon == 0", `1:12: pokemon 0 is not a species id; leave pokemon unconstrained for "everything else"`},
		{"pokemon < 5", "1:1: pokemon supports ==, !=, in and not in only"},
		{"pokemon in 1..5", "1:1: pokemon supports a list, not a range"},
		{"form == -1", "1:9: form -1 is out of range 0..32767"},
		{"iv == 99999999", "1:7: integer 99999999 is out of range"},
		{"iv ?? 1", `1:4: unsupported operator "??"`},
		{"pokemon in [1, 0, 5]", `1:16: pokemon 0 is not a species id; leave pokemon unconstrained for "everything else"`},
		{"form in [3, 40000, 7]", "1:13: form 40000 is out of range 0..32767"},
	}
	for _, c := range cases {
		_, err := parse(c.src)
		if err == nil {
			t.Errorf("%q: expected an error", c.src)
			continue
		}
		if err.Error() != c.want {
			t.Errorf("%q: error %q, want %q", c.src, err.Error(), c.want)
		}
	}
}

func TestParseRejectsLongExpression(t *testing.T) {
	src := make([]byte, maxExpressionBytes+1)
	for i := range src {
		src[i] = ' '
	}
	if _, err := parse(string(src)); err == nil {
		t.Error("expected an error for an over-long expression")
	}
}

// Offsets are bytes while columns stay rune-based: the é before each error
// is two bytes but one column.
func TestParseErrorPositionsNonASCII(t *testing.T) {
	cases := []struct {
		src  string
		want Position
	}{
		{"/* é */ x == 1", Position{Line: 1, Column: 9, Offset: 9}},
		{"/* é */ iv == 1.5", Position{Line: 1, Column: 15, Offset: 15}},
		{"/* é */ iv == 1 )", Position{Line: 1, Column: 17, Offset: 17}},
	}
	for _, c := range cases {
		_, err := parse(c.src)
		e, ok := err.(*Error)
		if !ok {
			t.Errorf("%q: error %v, want *Error", c.src, err)
			continue
		}
		if e.Pos != c.want {
			t.Errorf("%q: position %+v, want %+v (%s)", c.src, e.Pos, c.want, e.Msg)
		}
	}
}
