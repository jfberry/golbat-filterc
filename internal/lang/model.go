package lang

import "fmt"

// FieldID identifies a field of a Model: an index into its field table.
type FieldID uint8

// MaxFields bounds a model's field table, so a Conjunction is a value
// (fixed arrays) and copying one never shares state.
const MaxFields = 16

// Kind says how a field's set reaches the wire, which decides whether
// split must break it into one interval per conjunction.
type Kind uint8

const (
	Range Kind = iota // one {min, max} per clause: split into one conjunction per interval
	List              // a list of values: not split
	Key               // selects a scan group (pokemon, form): not split
)

// NullGroup describes fields that are NULL (absent) for some rows: every
// condition on such a field is UNKNOWN for them, negated or not.
type NullGroup struct {
	// Whole is the verdict for a literal covering the whole domain, which
	// is not a no-op: it excludes the rows where the field is NULL.
	Whole string
	// Negated is appended to the warning on a complemented literal.
	Negated string
	// Note is appended to "can never hold" once such a literal was warned.
	Note string
}

// Field is one comparable field of a model.
type Field struct {
	Name   string
	Kind   Kind
	Domain IntervalSet // the values the field can hold; complements are taken within it
	Null   *NullGroup  // nil when the field always has a value
	// IDs makes explicit values (==, !=, list members) outside the domain
	// an error at the value instead of a warning; ranges and ordered
	// comparisons are still clipped.
	IDs bool
	// BadID, with IDs, may give the message for a value outside the
	// domain; "" falls back to "<name> <v> is out of range <lo>..<hi>".
	BadID func(v int) string
	// Below, when set, is how a range that starts below the domain and
	// reaches into it is reported ("species ids start at 1"), instead of
	// as a clipped range.
	Below string
}

// Model is one filter language's field table.
type Model struct {
	Name   string
	fields []Field
	byName map[string]FieldID
}

// NewModel builds a model; it panics on a table that cannot be right
// (more than MaxFields fields, a repeated name, an empty domain), since
// models are package-level tables checked by their tests.
func NewModel(name string, fields []Field) *Model {
	if len(fields) > MaxFields {
		panic(fmt.Sprintf("lang: model %s has %d fields, more than %d", name, len(fields), MaxFields))
	}
	m := &Model{Name: name, fields: fields, byName: make(map[string]FieldID, len(fields))}
	for i, f := range fields {
		if _, dup := m.byName[f.Name]; dup {
			panic(fmt.Sprintf("lang: model %s repeats field %q", name, f.Name))
		}
		if len(f.Domain) == 0 {
			panic(fmt.Sprintf("lang: model %s field %q has an empty domain", name, f.Name))
		}
		m.byName[f.Name] = FieldID(i)
	}
	return m
}

// Field returns the field with id f.
func (m *Model) Field(f FieldID) *Field { return &m.fields[f] }

// Lookup finds a field by name.
func (m *Model) Lookup(name string) (FieldID, bool) {
	f, ok := m.byName[name]
	return f, ok
}

// Len is the number of fields.
func (m *Model) Len() int { return len(m.fields) }
