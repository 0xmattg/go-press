package commerceusdt

import (
	"math/big"
	"testing"

	corecommerce "github.com/0xmattg/go-press/core/commerce"
)

func testReadyConfig(t *testing.T) config {
	t.Helper()
	preset := chainPresets["ethereum"]
	network := preset.Networks["mainnet"]
	return config{
		Enabled: true, ChainID: "ethereum", Network: "mainnet", RPCURL: "https://rpc.example",
		TokenContract: network.Contract, Xpub: accountXpub(t), Confirmations: 24,
		WindowMinutes: 30, RateScaled: rateScale, DustTolerance: big.NewInt(0),
		preset: preset, net: network, decimals: preset.Decimals, resolved: true,
	}
}

func TestInvoiceForSnapshotsImmutablePaymentTerms(t *testing.T) {
	db := usdtTestDB(t)
	p := &Plugin{db: db}
	cfg := testReadyConfig(t)
	chain := p.buildChain(cfg)
	req := corecommerce.PaymentRequest{
		OrderRef: "INV-1", IdempotencyKey: "start:INV-1", Amount: corecommerce.New(1999, "USD"),
	}
	inv, err := p.invoiceFor(cfg, chain, req)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Currency != "USD" || inv.NetworkKey != cfg.networkKey() || inv.Confirmations != 24 ||
		inv.ExpectedToken != "19990000" || inv.DustTolerance != "0" || inv.WatchUntil.IsZero() {
		t.Fatalf("invoice snapshot = %#v", inv)
	}
	reused, err := p.invoiceFor(cfg, chain, req)
	if err != nil || reused.ID != inv.ID {
		t.Fatalf("invoice reuse = id %d, err=%v; want %d", reused.ID, err, inv.ID)
	}
	conflict := req
	conflict.Amount = corecommerce.New(2000, "USD")
	if _, err := p.invoiceFor(cfg, chain, conflict); err == nil {
		t.Fatal("idempotency payload conflict accepted")
	}
}

func TestInvoiceForRejectsZeroTokenQuote(t *testing.T) {
	db := usdtTestDB(t)
	p := &Plugin{db: db}
	cfg := testReadyConfig(t)
	cfg.RateScaled = 1
	if _, err := p.invoiceFor(cfg, p.buildChain(cfg), corecommerce.PaymentRequest{
		OrderRef: "ZERO", IdempotencyKey: "start:ZERO", Amount: corecommerce.New(1, "USD"),
	}); err == nil {
		t.Fatal("zero-token quote accepted")
	}
}

func TestStartPaymentRejectsNonUSDDefinitively(t *testing.T) {
	_, err := (usdtGateway{}).StartPayment(nil, corecommerce.PaymentRequest{Amount: corecommerce.New(100, "CNY")})
	if !corecommerce.IsDefinitiveStartFailure(err) {
		t.Fatalf("non-USD error = %v, want definitive", err)
	}
}
