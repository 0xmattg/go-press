package commerce

// Address is a customer billing or shipping address. Flat and provider-neutral;
// shared by payment and shipping contracts.
type Address struct {
	Name, Company            string
	Line1, Line2, City       string
	State, Country, Postcode string
	Phone, Email             string
}

// KV is a labeled key/value row used by display-type payment instructions
// (e.g. a crypto deposit address, a bank account number).
type KV struct {
	Label, Value string
}
