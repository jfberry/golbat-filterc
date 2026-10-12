package filterc

import "testing"

func TestSmallSide(t *testing.T) {
	cases := []struct {
		f    field
		in   intervalSet
		want intervalSet
		neg  bool
	}{
		{fPokemon, setOf(interval{Lo: 1, Hi: 5}), setOf(interval{Lo: 1, Hi: 5}), false},
		{fPokemon, setOf(interval{Lo: 6, Hi: 32767}), setOf(interval{Lo: 1, Hi: 5}), true},
		{fPokemon, setOf(interval{Lo: 1, Hi: 16383}), setOf(interval{Lo: 1, Hi: 16383}), false},    // 16383 of 32767
		{fPokemon, setOf(interval{Lo: 1, Hi: 16384}), setOf(interval{Lo: 16385, Hi: 32767}), true}, // 16384 of 32767
		{fForm, setOf(interval{Lo: 0, Hi: 16383}), setOf(interval{Lo: 0, Hi: 16383}), false},       // a tie keeps the set
		{fForm, setOf(interval{Lo: 0, Hi: 32767}), setOf(), true},                                  // the whole domain
	}
	for _, c := range cases {
		got, neg := smallSide(c.f, c.in)
		if !got.Equal(c.want) || neg != c.neg {
			t.Errorf("smallSide(%s, %v) = %v, %v; want %v, %v", fieldNames[c.f], c.in, got, neg, c.want, c.neg)
		}
	}
}
