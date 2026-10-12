package lang

import (
	"strings"
	"testing"
)

func testModel() *Model {
	return NewModel("test", []Field{
		{Name: "a", Kind: Range, Domain: IntervalSet{{0, 10}}},
		{Name: "b", Kind: List, Domain: IntervalSet{{1, 3}}},
	})
}

func TestModelLookup(t *testing.T) {
	m := testModel()
	if f, ok := m.Lookup("b"); !ok || f != 1 || m.Field(f).Name != "b" {
		t.Errorf("Lookup(b) = %d, %v", f, ok)
	}
	if _, ok := m.Lookup("c"); ok {
		t.Error("Lookup(c) must fail")
	}
	if m.Len() != 2 || m.Name != "test" {
		t.Errorf("Len, Name = %d, %q", m.Len(), m.Name)
	}
}

func TestNewModelRejects(t *testing.T) {
	many := make([]Field, MaxFields+1)
	for i := range many {
		many[i] = Field{Name: strings.Repeat("x", i+1), Domain: IntervalSet{{0, 1}}}
	}
	cases := map[string][]Field{
		"duplicate":      {{Name: "a", Domain: IntervalSet{{0, 1}}}, {Name: "a", Domain: IntervalSet{{0, 1}}}},
		"empty domain":   {{Name: "a"}},
		"too many":       many,
		"not normalised": {{Name: "a", Domain: IntervalSet{{0, 0}, {1, 1}}}},
	}
	for name, fields := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: NewModel did not panic", name)
				}
			}()
			NewModel("bad", fields)
		}()
	}
}

func TestNewModelCopiesFields(t *testing.T) {
	fields := []Field{{Name: "a", Domain: IntervalSet{{0, 1}}}}
	m := NewModel("copy", fields)
	fields[0].Name = "z"
	if got := m.Field(0).Name; got != "a" {
		t.Errorf("model field changed through the caller's slice: %q", got)
	}
}
