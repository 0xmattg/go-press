package commerceusdt

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// transferTopic is keccak256("Transfer(address,address,uint256)"), computed once
// so there is no hand-transcribed constant to get wrong.
var transferTopic = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")).Hex()

// evmChain is the generic EVM implementation of Chain, shared by every EVM
// preset (Ethereum now; BSC/Polygon later). Only its resolved constants differ,
// so adding a chain never touches this code.
type evmChain struct {
	id      string
	token   TokenSpec
	chainID int64
	confs   uint64
	rpcURL  string
	xpub    string
	http    *http.Client
}

func (c *evmChain) ID() string                             { return c.id }
func (c *evmChain) Token() TokenSpec                       { return c.token }
func (c *evmChain) Confirmations() uint64                  { return c.confs }
func (c *evmChain) DeriveAddress(i uint32) (string, error) { return deriveEVMAddress(c.xpub, i) }

// PaymentURI builds an EIP-681 ERC-20 transfer request for a QR code.
func (c *evmChain) PaymentURI(addr string, amount *big.Int) string {
	return fmt.Sprintf("ethereum:%s@%d/transfer?address=%s&uint256=%s",
		c.token.Contract, c.chainID, addr, amount.String())
}

// --- JSON-RPC ---

type rpcRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *evmChain) call(ctx context.Context, method string, params []interface{}, out interface{}) error {
	body, _ := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.rpcURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("usdt rpc %s: read: %w", method, err)
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("usdt rpc %s: http %d: %s", method, res.StatusCode, snippet(raw))
	}
	var r rpcResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("usdt rpc %s: decode: %w", method, err)
	}
	if r.JSONRPC != "2.0" || string(r.ID) != "1" {
		return fmt.Errorf("usdt rpc %s: mismatched response envelope", method)
	}
	if r.Error != nil {
		return fmt.Errorf("usdt rpc %s: %d %s", method, r.Error.Code, r.Error.Message)
	}
	if out != nil {
		if len(r.Result) == 0 || bytes.Equal(r.Result, []byte("null")) {
			return fmt.Errorf("usdt rpc %s: missing result", method)
		}
		return json.Unmarshal(r.Result, out)
	}
	return nil
}

// LatestBlock returns the current head height.
func (c *evmChain) LatestBlock(ctx context.Context) (uint64, error) {
	var hexNum string
	if err := c.call(ctx, "eth_blockNumber", []interface{}{}, &hexNum); err != nil {
		return 0, err
	}
	return hexToUint64(hexNum)
}

type rpcLog struct {
	Address     string   `json:"address"`
	Topics      []string `json:"topics"`
	Data        string   `json:"data"`
	BlockNumber string   `json:"blockNumber"`
	TxHash      string   `json:"transactionHash"`
	LogIndex    string   `json:"logIndex"`
	BlockHash   string   `json:"blockHash"`
	Removed     bool     `json:"removed"`
}

type rpcBlock struct {
	Timestamp string `json:"timestamp"`
}

// BlockTimestamp returns the canonical timestamp of a block. Expiry decisions
// use this chain time rather than server wall time, after the block is safely
// confirmed and scanned.
func (c *evmChain) BlockTimestamp(ctx context.Context, block uint64) (time.Time, error) {
	var out rpcBlock
	if err := c.call(ctx, "eth_getBlockByNumber", []interface{}{uint64Hex(block), false}, &out); err != nil {
		return time.Time{}, err
	}
	seconds, err := hexToUint64(out.Timestamp)
	if err != nil || seconds == 0 || seconds > uint64(^uint64(0)>>1) {
		return time.Time{}, fmt.Errorf("usdt: invalid block timestamp %q", out.Timestamp)
	}
	return time.Unix(int64(seconds), 0).UTC(), nil
}

// VerifyConfiguration binds the configured RPC to the expected EVM chain and
// token contract before settings are accepted.
func (c *evmChain) VerifyConfiguration(ctx context.Context) error {
	var chainHex string
	if err := c.call(ctx, "eth_chainId", []interface{}{}, &chainHex); err != nil {
		return err
	}
	chainID, err := hexToUint64(chainHex)
	if err != nil || chainID != uint64(c.chainID) {
		return fmt.Errorf("RPC chain id %d does not match configured chain id %d", chainID, c.chainID)
	}
	var code string
	if err := c.call(ctx, "eth_getCode", []interface{}{c.token.Contract, "latest"}, &code); err != nil {
		return err
	}
	if code == "" || code == "0x" || code == "0x0" {
		return errors.New("configured token address has no contract code")
	}
	var decimalsHex string
	call := map[string]interface{}{"to": c.token.Contract, "data": "0x313ce567"}
	if err := c.call(ctx, "eth_call", []interface{}{call, "latest"}, &decimalsHex); err != nil {
		return err
	}
	decimals, err := hexToUint64(decimalsHex)
	if err != nil || int(decimals) != c.token.Decimals {
		return fmt.Errorf("token decimals %d do not match expected %d", decimals, c.token.Decimals)
	}
	return nil
}

// ScanTransfers fetches ERC-20 Transfer logs to any of addrs within [from,to].
func (c *evmChain) ScanTransfers(ctx context.Context, addrs []string, from, to uint64) ([]Deposit, error) {
	if len(addrs) == 0 || to < from {
		return nil, nil
	}
	toTopics := make([]string, 0, len(addrs))
	for _, a := range addrs {
		toTopics = append(toTopics, addressTopic(a))
	}
	filter := map[string]interface{}{
		"fromBlock": uint64Hex(from),
		"toBlock":   uint64Hex(to),
		"address":   c.token.Contract,
		"topics":    []interface{}{transferTopic, nil, toTopics},
	}
	var logs []rpcLog
	if err := c.call(ctx, "eth_getLogs", []interface{}{filter}, &logs); err != nil {
		return nil, err
	}
	out := make([]Deposit, 0, len(logs))
	watched := make(map[string]struct{}, len(addrs))
	for _, addr := range addrs {
		watched[strings.ToLower(common.HexToAddress(addr).Hex())] = struct{}{}
	}
	for _, l := range logs {
		if l.Removed {
			continue
		}
		if !strings.EqualFold(l.Address, c.token.Contract) || len(l.Topics) != 3 ||
			!strings.EqualFold(l.Topics[0], transferTopic) || !validTopic(l.Topics[1]) || !validTopic(l.Topics[2]) {
			return nil, errors.New("usdt: RPC returned a log outside the requested token/Transfer filter")
		}
		bn, err := hexToUint64(l.BlockNumber)
		if err != nil || bn < from || bn > to {
			return nil, fmt.Errorf("usdt: RPC returned invalid block %q", l.BlockNumber)
		}
		li, err := hexToUint64(l.LogIndex)
		if err != nil {
			return nil, fmt.Errorf("usdt: RPC returned invalid log index %q", l.LogIndex)
		}
		if !validHash(l.TxHash) || !validHash(l.BlockHash) {
			return nil, errors.New("usdt: RPC returned an invalid transaction or block hash")
		}
		if len(l.Data) != 66 || !strings.HasPrefix(l.Data, "0x") {
			return nil, errors.New("usdt: RPC returned an invalid ERC-20 amount")
		}
		val, ok := new(big.Int).SetString(strings.TrimPrefix(l.Data, "0x"), 16)
		if !ok {
			return nil, errors.New("usdt: RPC returned an invalid ERC-20 amount")
		}
		// ERC-20 requires zero-value transfers to emit Transfer too. They carry no
		// payment value and must be ignored rather than stalling the scan cursor.
		if val.Sign() == 0 {
			continue
		}
		toAddr := topicToAddress(l.Topics[2])
		if _, ok := watched[strings.ToLower(toAddr)]; !ok {
			return nil, errors.New("usdt: RPC returned a transfer to an unwatched address")
		}
		out = append(out, Deposit{
			TxHash: strings.ToLower(l.TxHash), LogIndex: uint(li),
			From: topicToAddress(l.Topics[1]), To: toAddr,
			TokenAmount: val, BlockNumber: bn,
		})
	}
	return out, nil
}

func validHash(value string) bool {
	if len(value) != 66 || !strings.HasPrefix(value, "0x") {
		return false
	}
	_, err := hex.DecodeString(value[2:])
	return err == nil
}

func validTopic(value string) bool {
	return validHash(value)
}

// --- hex / topic helpers ---

func addressTopic(addr string) string {
	h := strings.ToLower(strings.TrimPrefix(addr, "0x"))
	if len(h) > 64 {
		h = h[len(h)-64:]
	}
	return "0x" + strings.Repeat("0", 64-len(h)) + h
}

func topicToAddress(topic string) string {
	h := strings.TrimPrefix(topic, "0x")
	if len(h) < 40 {
		return ""
	}
	return common.HexToAddress("0x" + h[len(h)-40:]).Hex()
}

func uint64Hex(n uint64) string { return fmt.Sprintf("0x%x", n) }

func hexToUint64(s string) (uint64, error) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	if s == "" {
		return 0, nil
	}
	v, ok := new(big.Int).SetString(s, 16)
	if !ok || !v.IsUint64() {
		return 0, fmt.Errorf("usdt: bad hex %q", s)
	}
	return v.Uint64(), nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[:300]
	}
	return s
}

var _ Chain = (*evmChain)(nil)
