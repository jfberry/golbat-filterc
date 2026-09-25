package filterc

// MinMax is an inclusive integer range as the v3 API takes it. Both bounds
// are always set by the compiler.
type MinMax struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// PokemonId is one entry of a clause's pokemon list: a species id and an
// optional form. A nil Form means any form of the species.
type PokemonId struct {
	Id   int  `json:"id"`
	Form *int `json:"form,omitempty"`
}

// Clause is one v3 filter clause. A clause with no Pokemon list is filed
// under "everything else".
type Clause struct {
	Pokemon []PokemonId `json:"pokemon,omitempty"`
	Iv      *MinMax     `json:"iv,omitempty"`
	AtkIv   *MinMax     `json:"atk_iv,omitempty"`
	DefIv   *MinMax     `json:"def_iv,omitempty"`
	StaIv   *MinMax     `json:"sta_iv,omitempty"`
	Level   *MinMax     `json:"level,omitempty"`
	Cp      *MinMax     `json:"cp,omitempty"`
	Gender  []int       `json:"gender,omitempty"`
	Size    *MinMax     `json:"size,omitempty"`
	Little  *MinMax     `json:"pvp_little,omitempty"`
	Great   *MinMax     `json:"pvp_great,omitempty"`
	Ultra   *MinMax     `json:"pvp_ultra,omitempty"`
}

func (cl *Clause) setRange(f field, mm *MinMax) {
	switch f {
	case fIv:
		cl.Iv = mm
	case fAtk:
		cl.AtkIv = mm
	case fDef:
		cl.DefIv = mm
	case fSta:
		cl.StaIv = mm
	case fLevel:
		cl.Level = mm
	case fCp:
		cl.Cp = mm
	case fSize:
		cl.Size = mm
	case fLittle:
		cl.Little = mm
	case fGreat:
		cl.Great = mm
	case fUltra:
		cl.Ultra = mm
	}
}

// LatLon is a coordinate as the scan API takes it.
type LatLon struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// Bounds is the scan's bounding box.
type Bounds struct {
	Min LatLon
	Max LatLon
}

// ScanRequest is the body of POST /api/pokemon/v3/scan.
type ScanRequest struct {
	Min     LatLon   `json:"min"`
	Max     LatLon   `json:"max"`
	Limit   int      `json:"limit,omitempty"`
	Filters []Clause `json:"filters"`
}

// Compiled is the result of compiling an expression.
type Compiled struct {
	Filters  []Clause // never nil, so it marshals as []
	Warnings []string
}

// Request wraps the filters into a full scan request body.
func (c *Compiled) Request(b Bounds, limit int) ScanRequest {
	return ScanRequest{Min: b.Min, Max: b.Max, Limit: limit, Filters: c.Filters}
}
