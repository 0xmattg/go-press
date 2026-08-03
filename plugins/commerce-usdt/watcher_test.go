package commerceusdt

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	corecommerce "go-press/core/commerce"
	"go-press/core/hook"
	"go-press/pkg/dbprefix"

	_ "github.com/mattn/go-sqlite3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type captureSettler struct {
	requests []corecommerce.SettleRequest
	err      error
}

func (s *captureSettler) Settle(_ context.Context, req corecommerce.SettleRequest) error {
	s.requests = append(s.requests, req)
	return s.err
}

func usdtTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbprefix.Set(dbprefix.DefaultPrefix)
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared&_foreign_keys=1"
	sqlDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, WithoutReturning: true}), &gorm.Config{
		DisableAutomaticPing: true, SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// SQLite serializes writes itself and does not support SELECT ... FOR UPDATE.
	db.ClauseBuilders["FOR"] = func(clause.Clause, clause.Builder) {}
	for _, statement := range []string{
		`CREATE TABLE ` + tbl("invoices") + ` (
			id INTEGER PRIMARY KEY AUTOINCREMENT, order_ref TEXT UNIQUE, start_key TEXT UNIQUE,
			chain TEXT, network_key TEXT, evm_chain_id INTEGER, hd_index INTEGER, address TEXT,
			token_contract TEXT, token_decimals INTEGER, confirmations INTEGER, expected_token TEXT, received_token TEXT,
			usd_minor INTEGER, currency TEXT, rate_scaled INTEGER, dust_tolerance TEXT, status TEXT,
			created_at DATETIME, expires_at DATETIME, watch_until DATETIME, settled_at DATETIME
		)`,
		`CREATE TABLE ` + tbl("deposits") + ` (
			id INTEGER PRIMARY KEY AUTOINCREMENT, invoice_id INTEGER, chain TEXT, network_key TEXT,
			tx_hash TEXT, log_index INTEGER, from_addr TEXT, token_amount TEXT, block_number INTEGER,
			block_time DATETIME, confirmations INTEGER, seen_at DATETIME,
			UNIQUE(network_key, tx_hash, log_index)
		)`,
		`CREATE TABLE ` + tbl("network_cursors") + ` (
			network_key TEXT PRIMARY KEY, last_scanned_block INTEGER, updated_at DATETIME
		)`,
		`CREATE TABLE ` + tbl("hd_counters") + ` (
			chain TEXT PRIMARY KEY, next_index INTEGER, updated_at DATETIME
		)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
				t.Skip("USDT transaction tests require CGO-backed SQLite")
			}
			t.Fatalf("create schema: %v\n%s", err, statement)
		}
	}
	return db
}

func testInvoice(deadline time.Time) Invoice {
	return Invoice{
		OrderRef: "ORDER-1", StartKey: "start:ORDER-1", Chain: "ethereum",
		NetworkKey: "evm:1:0xdac17f958d2ee523a2206206994597c13d831ec7", EVMChainID: 1,
		Address: canonicalAddr0, TokenContract: "0xdAC17F958D2ee523a2206206994597C13D831ec7",
		TokenDecimals: 6, Confirmations: 24, ExpectedToken: "10000000", ReceivedToken: "0",
		USDMinor: 1000, Currency: "USD", RateScaled: rateScale, DustTolerance: "0",
		Status: invPending, CreatedAt: deadline.Add(-30 * time.Minute), ExpiresAt: deadline,
		WatchUntil: deadline.Add(lateWatchRetention),
	}
}

func testPluginWithSettler(t *testing.T, db *gorm.DB) (*Plugin, *captureSettler) {
	t.Helper()
	bus := hook.New()
	settler := &captureSettler{}
	corecommerce.SetSettler(bus, settler)
	return &Plugin{db: db, hooks: bus}, settler
}

func TestPersistScanBatchDoesNotAdvanceCursorOnDepositFailure(t *testing.T) {
	db := usdtTestDB(t)
	if err := db.Exec(`DROP TABLE ` + tbl("deposits")).Error; err != nil {
		t.Fatal(err)
	}
	p := &Plugin{db: db}
	chain := &evmChain{id: "ethereum", confs: 24}
	deposit := Deposit{
		TxHash: "0x" + strings.Repeat("a", 64), To: canonicalAddr0,
		TokenAmount: big.NewInt(1), BlockNumber: 100, BlockTime: time.Now().UTC(),
	}
	err := p.persistScanBatch("evm:1:token", chain, 120, 120, []observedDeposit{{invoiceID: 1, deposit: deposit}})
	if err == nil {
		t.Fatal("deposit failure did not abort scan batch")
	}
	var count int64
	if err := db.Model(&NetworkCursor{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("cursor rows = %d, err=%v; want 0", count, err)
	}
}

func TestWatchInvoicesHasNoThousandOrderStarvationCap(t *testing.T) {
	db := usdtTestDB(t)
	deadline := time.Now().UTC().Add(time.Hour)
	invoices := make([]Invoice, 1005)
	for i := range invoices {
		invoices[i] = testInvoice(deadline)
		invoices[i].OrderRef = fmt.Sprintf("ORDER-%04d", i)
		invoices[i].StartKey = "start:" + invoices[i].OrderRef
	}
	if err := db.CreateInBatches(&invoices, 200).Error; err != nil {
		t.Fatal(err)
	}
	got, err := (&Plugin{db: db}).watchInvoices(invoices[0].NetworkKey, time.Now().UTC())
	if err != nil || len(got) != len(invoices) {
		t.Fatalf("watch invoices = %d, err=%v; want %d", len(got), err, len(invoices))
	}
}

func TestFinalizeUsesSafeChainTimeAndRecoversConfirmedPayment(t *testing.T) {
	db := usdtTestDB(t)
	p, settler := testPluginWithSettler(t, db)
	deadline := time.Unix(1_800_000_000, 0).UTC()
	inv := testInvoice(deadline)
	if err := db.Create(&inv).Error; err != nil {
		t.Fatal(err)
	}
	cfg := config{ChainID: "ethereum", resolved: true, net: evmNetwork{ChainID: 1}, TokenContract: inv.TokenContract}
	chain := &evmChain{id: "ethereum"}
	if err := p.finalizeExpired(context.Background(), cfg, chain, deadline.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(settler.requests) != 0 {
		t.Fatal("invoice finalized before safely scanned chain time passed deadline")
	}
	row := DepositRow{
		InvoiceID: inv.ID, Chain: inv.Chain, NetworkKey: inv.NetworkKey,
		TxHash: "0x" + strings.Repeat("a", 64), TokenAmount: inv.ExpectedToken,
		BlockNumber: 10, BlockTime: deadline.Add(-time.Second), SeenAt: deadline.Add(time.Minute),
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := p.finalizeExpired(context.Background(), cfg, chain, deadline.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(settler.requests) != 1 || settler.requests[0].Status != corecommerce.SettlePaid {
		t.Fatalf("settlements = %#v, want one paid", settler.requests)
	}
}

func TestFinalizeReportsActualUnderpaidAmount(t *testing.T) {
	db := usdtTestDB(t)
	p, settler := testPluginWithSettler(t, db)
	deadline := time.Now().UTC().Add(-time.Hour)
	inv := testInvoice(deadline)
	if err := db.Create(&inv).Error; err != nil {
		t.Fatal(err)
	}
	row := DepositRow{
		InvoiceID: inv.ID, Chain: inv.Chain, NetworkKey: inv.NetworkKey,
		TxHash: "0x" + strings.Repeat("b", 64), TokenAmount: "5000000",
		BlockNumber: 10, BlockTime: deadline.Add(-time.Second), SeenAt: deadline,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	cfg := config{resolved: true, net: evmNetwork{ChainID: 1}, TokenContract: inv.TokenContract}
	if err := p.finalizeExpired(context.Background(), cfg, &evmChain{id: "ethereum"}, deadline.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(settler.requests) != 1 || settler.requests[0].Status != corecommerce.SettleUnderpaid || settler.requests[0].Amount.Amount != 500 {
		t.Fatalf("underpaid settlement = %#v", settler.requests)
	}
}

func TestFinalizeAbortsOnDepositQueryFailure(t *testing.T) {
	db := usdtTestDB(t)
	p, settler := testPluginWithSettler(t, db)
	deadline := time.Now().UTC().Add(-time.Hour)
	inv := testInvoice(deadline)
	if err := db.Create(&inv).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DROP TABLE ` + tbl("deposits")).Error; err != nil {
		t.Fatal(err)
	}
	cfg := config{resolved: true, net: evmNetwork{ChainID: 1}, TokenContract: inv.TokenContract}
	if err := p.finalizeExpired(context.Background(), cfg, &evmChain{id: "ethereum"}, time.Now().UTC()); err == nil {
		t.Fatal("deposit query failure was treated as zero deposits")
	}
	if len(settler.requests) != 0 {
		t.Fatal("terminal settlement occurred after deposit query failure")
	}
}

func TestReconcileRetriesDurableDepositWithoutNewScan(t *testing.T) {
	db := usdtTestDB(t)
	p, settler := testPluginWithSettler(t, db)
	deadline := time.Now().UTC().Add(time.Hour)
	inv := testInvoice(deadline)
	if err := db.Create(&inv).Error; err != nil {
		t.Fatal(err)
	}
	row := DepositRow{
		InvoiceID: inv.ID, Chain: inv.Chain, NetworkKey: inv.NetworkKey,
		TxHash: "0x" + strings.Repeat("c", 64), TokenAmount: inv.ExpectedToken,
		BlockNumber: 10, BlockTime: deadline.Add(-time.Minute), SeenAt: time.Now().UTC(),
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := p.reconcileInvoices(context.Background(), &evmChain{id: "ethereum"}, []Invoice{inv}); err != nil {
		t.Fatal(err)
	}
	if len(settler.requests) != 1 || settler.requests[0].Status != corecommerce.SettlePaid {
		t.Fatalf("recovered settlements = %#v", settler.requests)
	}
	var saved Invoice
	if err := db.First(&saved, inv.ID).Error; err != nil || saved.Status != invPaid {
		t.Fatalf("invoice status = %q, err=%v", saved.Status, err)
	}
}

func TestReconcileReportsLatePartialDepositForExpiredInvoice(t *testing.T) {
	db := usdtTestDB(t)
	p, settler := testPluginWithSettler(t, db)
	deadline := time.Now().UTC().Add(-time.Hour)
	inv := testInvoice(deadline)
	inv.Status = invExpired
	if err := db.Create(&inv).Error; err != nil {
		t.Fatal(err)
	}
	row := DepositRow{
		InvoiceID: inv.ID, Chain: inv.Chain, NetworkKey: inv.NetworkKey,
		TxHash: "0x" + strings.Repeat("d", 64), TokenAmount: "5000000",
		BlockNumber: 10, BlockTime: deadline.Add(time.Minute), SeenAt: time.Now().UTC(),
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}

	if err := p.reconcileInvoices(context.Background(), &evmChain{id: "ethereum"}, []Invoice{inv}); err != nil {
		t.Fatal(err)
	}
	if len(settler.requests) != 1 || settler.requests[0].Status != corecommerce.SettleUnderpaid || settler.requests[0].Amount.Amount != 500 {
		t.Fatalf("late settlement = %#v, want one underpaid 500", settler.requests)
	}
	var saved Invoice
	if err := db.First(&saved, inv.ID).Error; err != nil || saved.Status != invLate {
		t.Fatalf("invoice status = %q, err=%v; want %q", saved.Status, err, invLate)
	}
}

func TestSettleFailureLeavesInvoiceRetryable(t *testing.T) {
	db := usdtTestDB(t)
	p, settler := testPluginWithSettler(t, db)
	settler.err = errors.New("temporary commerce failure")
	deadline := time.Now().UTC().Add(time.Hour)
	inv := testInvoice(deadline)
	if err := db.Create(&inv).Error; err != nil {
		t.Fatal(err)
	}
	err := p.settle(context.Background(), &inv, big.NewInt(10_000_000), 1000, corecommerce.SettlePaid, invPaid, "tx")
	if err == nil {
		t.Fatal("settlement failure ignored")
	}
	var saved Invoice
	if err := db.First(&saved, inv.ID).Error; err != nil || saved.Status != invPending {
		t.Fatalf("invoice status after failure = %q, err=%v", saved.Status, err)
	}
}

func TestSettlementClassifiesOverpaymentUsingActualUSD(t *testing.T) {
	inv := testInvoice(time.Now().UTC())
	status, amount, ok := settlementForReceived(&inv, big.NewInt(12_000_000))
	if !ok || status != corecommerce.SettleOverpaid || amount != 1200 {
		t.Fatalf("overpayment = (%s,%d,%v), want overpaid 1200", status, amount, ok)
	}
}
