package lang

import "testing"

// Conjunctions are copied by value but hold slice headers; merging and
// splitting must build new sets, never write into an input's.
func TestMergeAndSplitLeaveInputsAlone(t *testing.T) {
	m := testModel()
	n, err := Lower(m, "(a in 0..5 || a == 9) && b in [1, 2]", nil)
	if err != nil {
		t.Fatal(err)
	}
	conjs, err := ToDNF(m, NNF(m, n, nil), 16)
	if err != nil {
		t.Fatal(err)
	}
	before := make([]string, len(conjs))
	for i, c := range conjs {
		before[i] = c.String()
	}
	a, _ := fromLit(m, &Lit{F: 0, Set: IntervalSet{{0, 10}}})
	b, _ := fromLit(m, &Lit{F: 0, Set: IntervalSet{{3, 4}}})
	if c, ok := merge(a, b); !ok || c.String() != "a[3,4]" || a.String() != "a[0,10]" || b.String() != "a[3,4]" {
		t.Errorf("merge: %s from %s and %s", c, a, b)
	}
	wide, _ := fromLit(m, &Lit{F: 0, Set: IntervalSet{{0, 1}, {5, 6}}})
	parts, err := Split([]Conjunction{wide}, 16)
	if err != nil || len(parts) != 2 || wide.String() != "a[0,1][5,6]" {
		t.Errorf("split: %v %v, input now %s", parts, err, wide)
	}
	for i, c := range conjs {
		if c.String() != before[i] {
			t.Errorf("conjunction %d changed: %s, was %s", i, c, before[i])
		}
	}
	if len(conjs) != 2 || conjs[0].String() != "a[0,5] b[1,2]" || conjs[1].String() != "a[9,9] b[1,2]" {
		t.Errorf("DNF = %v", conjs)
	}
}
