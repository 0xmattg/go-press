package commerceusdt

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type testOptionStore map[string]string

func (s testOptionStore) Get(key string) string { return s[key] }
func (s testOptionStore) GetDefault(key, fallback string) string {
	if value := s[key]; value != "" {
		return value
	}
	return fallback
}
func (s testOptionStore) Set(key, value string) error { s[key] = value; return nil }

func validSettings(t *testing.T, rpcURL string) map[string]string {
	t.Helper()
	return map[string]string{
		optEnabled: "1", optChain: "ethereum", optNetwork: "mainnet", optRPCURL: rpcURL,
		optTokenContract: "0xdAC17F958D2ee523a2206206994597C13D831ec7",
		optReceiveXpub:   accountXpub(t), optConfirmations: "24", optWindowMinutes: "30", optUSDRate: "1.00",
	}
}

func verifiedRPCServer(t *testing.T, chainID string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request rpcRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		result := `"` + chainID + `"`
		switch request.Method {
		case "eth_getCode":
			result = `"0x6000"`
		case "eth_call":
			result = `"0x6"`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":` + result + `}`))
	}))
}

func TestValidateSettingsVerifiesRPCAndBounds(t *testing.T) {
	db := usdtTestDB(t)
	rpc := verifiedRPCServer(t, "0x1")
	defer rpc.Close()
	p := &Plugin{db: db, options: testOptionStore{}, httpClient: rpc.Client()}
	settings := validSettings(t, rpc.URL)
	if err := p.ValidateSettings(settings); err != nil {
		t.Fatal(err)
	}

	badRate := validSettings(t, rpc.URL)
	badRate[optUSDRate] = "0.000001"
	if err := p.ValidateSettings(badRate); err == nil || !strings.Contains(err.Error(), "rate") {
		t.Fatalf("unsafe rate error = %v", err)
	}
	badContract := validSettings(t, rpc.URL)
	badContract[optTokenContract] = "not-an-address"
	if err := p.ValidateSettings(badContract); err == nil {
		t.Fatal("invalid contract accepted")
	}
	lookalikeContract := validSettings(t, rpc.URL)
	lookalikeContract[optTokenContract] = "0x1111111111111111111111111111111111111111"
	if err := p.ValidateSettings(lookalikeContract); err == nil || !strings.Contains(err.Error(), "canonical") {
		t.Fatalf("non-canonical mainnet token error = %v", err)
	}
	for name, mutate := range map[string]func(map[string]string){
		"confirmations": func(values map[string]string) { values[optConfirmations] = "twenty-four" },
		"window":        func(values map[string]string) { values[optWindowMinutes] = "thirty" },
		"dust":          func(values map[string]string) { values[optDustTolerance] = "1.5" },
	} {
		t.Run(name, func(t *testing.T) {
			values := validSettings(t, rpc.URL)
			mutate(values)
			if err := p.ValidateSettings(values); err == nil {
				t.Fatalf("malformed %s accepted", name)
			}
		})
	}
}

func TestValidateSettingsRejectsWrongRPCChain(t *testing.T) {
	db := usdtTestDB(t)
	rpc := verifiedRPCServer(t, "0x38")
	defer rpc.Close()
	p := &Plugin{db: db, options: testOptionStore{}, httpClient: rpc.Client()}
	if err := p.ValidateSettings(validSettings(t, rpc.URL)); err == nil || !strings.Contains(err.Error(), "chain id") {
		t.Fatalf("wrong-chain RPC error = %v", err)
	}
}

func TestValidateSettingsProtectsPendingInvoiceIdentity(t *testing.T) {
	db := usdtTestDB(t)
	rpc := verifiedRPCServer(t, "0x1")
	defer rpc.Close()
	current := testOptionStore(validSettings(t, rpc.URL))
	p := &Plugin{db: db, options: current, httpClient: rpc.Client()}
	inv := testInvoice(time.Now().UTC().Add(time.Hour))
	if err := db.Create(&inv).Error; err != nil {
		t.Fatal(err)
	}
	if err := p.ValidateSettings(map[string]string{optEnabled: "0"}); err == nil {
		t.Fatal("gateway disabled while invoice pending")
	}
	if err := p.ValidateSettings(map[string]string{
		optNetwork: "testnet", optTokenContract: "0x1111111111111111111111111111111111111111",
	}); err == nil {
		t.Fatal("settlement identity switched while invoice pending")
	}
}

func TestValidateSettingsProtectsLateWatchWindow(t *testing.T) {
	db := usdtTestDB(t)
	rpc := verifiedRPCServer(t, "0x1")
	defer rpc.Close()
	current := testOptionStore(validSettings(t, rpc.URL))
	p := &Plugin{db: db, options: current, httpClient: rpc.Client()}
	inv := testInvoice(time.Now().UTC().Add(-time.Hour))
	inv.Status = invExpired
	inv.WatchUntil = time.Now().UTC().Add(time.Hour)
	if err := db.Create(&inv).Error; err != nil {
		t.Fatal(err)
	}
	if err := p.ValidateSettings(map[string]string{optEnabled: "0"}); err == nil {
		t.Fatal("gateway disabled during late-payment watch window")
	}
	inv.WatchUntil = time.Now().UTC().Add(-time.Second)
	if err := db.Model(&Invoice{}).Where("id = ?", inv.ID).Update("watch_until", inv.WatchUntil).Error; err != nil {
		t.Fatal(err)
	}
	if err := p.ValidateSettings(map[string]string{optEnabled: "0"}); err != nil {
		t.Fatalf("gateway cannot be disabled after watch retention: %v", err)
	}
}

func TestGatewaySupportsOnlyUSD(t *testing.T) {
	g := usdtGateway{}
	if !g.SupportsCurrency("usd") || g.SupportsCurrency("CNY") {
		t.Fatal("USDT gateway currency contract is incorrect")
	}
}

func TestUSDTSettingsUseDedicatedPermissionResource(t *testing.T) {
	if got := (&Plugin{}).SettingsPermissionResource(); got != "commerce_usdt_settings" {
		t.Fatalf("settings resource = %q", got)
	}
}

func TestLoadConfigFailsClosedOnMalformedStoredNumbers(t *testing.T) {
	for name, key := range map[string]string{
		"rate": optUSDRate, "confirmations": optConfirmations,
		"window": optWindowMinutes, "dust": optDustTolerance,
	} {
		t.Run(name, func(t *testing.T) {
			values := testOptionStore(validSettings(t, "https://rpc.example"))
			values[key] = "invalid"
			p := &Plugin{options: values}
			if p.loadConfig().ready() {
				t.Fatalf("configuration with malformed %s remained available", name)
			}
		})
	}
}
