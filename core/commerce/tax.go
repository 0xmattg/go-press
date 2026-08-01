package commerce

import "github.com/gin-gonic/gin"

// TaxCalculator computes tax for a set of taxable amounts. The default
// implementation (in the commerce engine) reads the tax_rate table; a satellite
// can replace it (VAT MOSS, Avalara, …) by registering its own.
type TaxCalculator interface {
	Calculate(c *gin.Context, set TaxableSet) (TaxResult, error)
}

// TaxableSet is the input to tax calculation.
type TaxableSet struct {
	Dest  Address
	Lines []TaxableLine
}

// TaxableLine is one taxable amount with its tax class.
type TaxableLine struct {
	Ref      string
	Amount   Money
	TaxClass string
}

// TaxResult is the computed tax: total plus a per-line breakdown.
type TaxResult struct {
	Total Money
	Lines []TaxLineResult
}

// TaxLineResult is the tax computed for one TaxableLine.
type TaxLineResult struct {
	Ref string
	Tax Money
}
