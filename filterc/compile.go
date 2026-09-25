package filterc

const (
	DefaultMaxConjunctions = 512
	DefaultMaxClauses      = 10000
	DefaultMaxKeys         = 10000
	DefaultMaxIds          = 100000
)

type options struct {
	maxConjunctions, maxClauses, maxKeys, maxIds int
}

// Option adjusts Compile's limits.
type Option func(*options)

// WithMaxConjunctions caps the number of conjunctions the expression may
// expand to (default DefaultMaxConjunctions).
func WithMaxConjunctions(n int) Option { return func(o *options) { o.maxConjunctions = n } }

// WithMaxClauses caps the number of emitted clauses (default DefaultMaxClauses).
func WithMaxClauses(n int) Option { return func(o *options) { o.maxClauses = n } }

// WithMaxKeys caps the number of distinguished (species, form) keys the
// expression names (default DefaultMaxKeys).
func WithMaxKeys(n int) Option { return func(o *options) { o.maxKeys = n } }

// WithMaxIds caps the total number of pokemon entries across all emitted
// clauses, block clauses included (default DefaultMaxIds).
func WithMaxIds(n int) Option { return func(o *options) { o.maxIds = n } }

// Compile turns an expression into v3 filter clauses. Errors are *Error
// with a position.
func Compile(expression string, opts ...Option) (*Compiled, error) {
	o := options{maxConjunctions: DefaultMaxConjunctions, maxClauses: DefaultMaxClauses, maxKeys: DefaultMaxKeys, maxIds: DefaultMaxIds}
	for _, opt := range opts {
		opt(&o)
	}
	w := &warnings{}
	n, err := lower(expression, w)
	if err != nil {
		return nil, err
	}
	conjs, err := toDNF(nnf(n, w), o.maxConjunctions)
	if err != nil {
		return nil, err
	}
	if conjs, err = split(conjs, o.maxConjunctions); err != nil {
		return nil, err
	}
	if err := validate(conjs); err != nil {
		return nil, err
	}
	clauses, err := dispatchCapped(conjs, o.maxClauses, o.maxKeys, o.maxIds)
	if err != nil {
		return nil, err
	}
	c := &Compiled{Filters: clauses, Warnings: w.list()}
	if len(clauses) == 0 {
		msg := "the expression can never hold; the request matches nothing"
		if w.pvpNegated {
			msg += "; note that negated PvP conditions never match pokemon without PvP data"
		}
		c.Warnings = append(c.Warnings, msg)
	}
	return c, nil
}
