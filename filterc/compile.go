package filterc

const (
	DefaultMaxConjunctions = 512
	DefaultMaxClauses      = 10000
)

type options struct {
	maxConjunctions, maxClauses int
}

// Option adjusts Compile's limits.
type Option func(*options)

// WithMaxConjunctions caps the number of conjunctions the expression may
// expand to (default DefaultMaxConjunctions).
func WithMaxConjunctions(n int) Option { return func(o *options) { o.maxConjunctions = n } }

// WithMaxClauses caps the number of emitted clauses (default DefaultMaxClauses).
func WithMaxClauses(n int) Option { return func(o *options) { o.maxClauses = n } }

// Compile turns an expression into v3 filter clauses. Errors are *Error
// with a position.
func Compile(expression string, opts ...Option) (*Compiled, error) {
	o := options{maxConjunctions: DefaultMaxConjunctions, maxClauses: DefaultMaxClauses}
	for _, opt := range opts {
		opt(&o)
	}
	n, err := parse(expression)
	if err != nil {
		return nil, err
	}
	conjs, err := toDNF(nnf(n), o.maxConjunctions)
	if err != nil {
		return nil, err
	}
	if conjs, err = split(conjs, o.maxConjunctions); err != nil {
		return nil, err
	}
	if err := validate(conjs); err != nil {
		return nil, err
	}
	clauses, err := dispatch(conjs, o.maxClauses)
	if err != nil {
		return nil, err
	}
	c := &Compiled{Filters: clauses}
	if len(clauses) == 0 {
		c.Warnings = append(c.Warnings, "the expression can never hold; the request matches nothing")
	}
	return c, nil
}
