package commerceusdt

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEVMLatestBlockAndScan(t *testing.T) {
	toAddr := "0x9858EfFD232B4033E47d90003D41EC34EcaEda94"
	fromAddr := "0x1111111111111111111111111111111111111111"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "eth_blockNumber":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x100"}`))
		case "eth_getLogs":
			// value = 10 USDT (6dp) = 10_000000 = 0x989680, left-padded to 32 bytes.
			data := "0x" + strings.Repeat("0", 64-6) + "989680"
			logObj := map[string]interface{}{
				"topics":          []string{transferTopic, addressTopic(fromAddr), addressTopic(toAddr)},
				"data":            data,
				"blockNumber":     "0xf0",
				"transactionHash": "0xabc",
				"logIndex":        "0x2",
				"removed":         false,
			}
			resp, _ := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "id": 1, "result": []interface{}{logObj}})
			_, _ = w.Write(resp)
		default:
			http.Error(w, "unexpected method", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	c := &evmChain{
		id: "ethereum", chainID: 1,
		token: TokenSpec{Symbol: "USDT", Contract: "0xdAC17F958D2ee523a2206206994597C13D831ec7", Decimals: 6},
		confs: 24, rpcURL: srv.URL, http: srv.Client(),
	}
	ctx := context.Background()

	if bn, err := c.LatestBlock(ctx); err != nil || bn != 0x100 {
		t.Fatalf("LatestBlock = %d, err = %v", bn, err)
	}

	deps, err := c.ScanTransfers(ctx, []string{toAddr}, 0x10, 0xf0)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 {
		t.Fatalf("want 1 deposit, got %d", len(deps))
	}
	d := deps[0]
	if d.TokenAmount.String() != "10000000" {
		t.Errorf("amount = %s, want 10000000", d.TokenAmount)
	}
	if d.BlockNumber != 0xf0 {
		t.Errorf("block = %d, want 240", d.BlockNumber)
	}
	if d.LogIndex != 2 {
		t.Errorf("logIndex = %d, want 2", d.LogIndex)
	}
	if !strings.EqualFold(d.To, toAddr) {
		t.Errorf("to = %s, want %s", d.To, toAddr)
	}
	if !strings.EqualFold(d.From, fromAddr) {
		t.Errorf("from = %s, want %s", d.From, fromAddr)
	}
}

func TestPaymentURI(t *testing.T) {
	c := &evmChain{chainID: 1, token: TokenSpec{Contract: "0xdac", Decimals: 6}}
	got := c.PaymentURI("0xabc", big.NewInt(10000000))
	want := "ethereum:0xdac@1/transfer?address=0xabc&uint256=10000000"
	if got != want {
		t.Errorf("PaymentURI = %q, want %q", got, want)
	}
}

func TestScanRange(t *testing.T) {
	// First-ever scan with a deep head: bounded lookback window.
	from, to, ok := scanRange(0, 100000)
	if !ok || from != 100000-initialLookback || to != 100000 {
		t.Errorf("scanRange(0,100000) = %d,%d,%v", from, to, ok)
	}
	// First-ever scan with a shallow head: start at 1.
	from, to, ok = scanRange(0, 100)
	if !ok || from != 1 || to != 100 {
		t.Errorf("scanRange(0,100) = %d,%d,%v", from, to, ok)
	}
	// Nothing new.
	if _, _, ok := scanRange(500, 500); ok {
		t.Error("scanRange(500,500) should report nothing new")
	}
	// Large gap clamped to maxScanSpan.
	from, to, ok = scanRange(1000, 10000)
	if !ok || from != 1001 || to != 1001+maxScanSpan {
		t.Errorf("scanRange(1000,10000) = %d,%d,%v", from, to, ok)
	}
}

func TestAddressTopicRoundTrip(t *testing.T) {
	addr := "0x9858EfFD232B4033E47d90003D41EC34EcaEda94"
	if got := topicToAddress(addressTopic(addr)); !strings.EqualFold(got, addr) {
		t.Errorf("round-trip = %s, want %s", got, addr)
	}
}
