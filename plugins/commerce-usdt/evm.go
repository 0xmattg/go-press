package commerceusdt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"

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
	Result json.RawMessage `json:"result"`
	Error  *struct {
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
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("usdt rpc %s: http %d: %s", method, res.StatusCode, snippet(raw))
	}
	var r rpcResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("usdt rpc %s: decode: %w", method, err)
	}
	if r.Error != nil {
		return fmt.Errorf("usdt rpc %s: %d %s", method, r.Error.Code, r.Error.Message)
	}
	if out != nil {
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
	Topics      []string `json:"topics"`
	Data        string   `json:"data"`
	BlockNumber string   `json:"blockNumber"`
	TxHash      string   `json:"transactionHash"`
	LogIndex    string   `json:"logIndex"`
	Removed     bool     `json:"removed"`
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
	for _, l := range logs {
		if l.Removed || len(l.Topics) < 3 {
			continue
		}
		bn, _ := hexToUint64(l.BlockNumber)
		li, _ := hexToUint64(l.LogIndex)
		val, ok := new(big.Int).SetString(strings.TrimPrefix(l.Data, "0x"), 16)
		if !ok {
			continue
		}
		out = append(out, Deposit{
			TxHash: l.TxHash, LogIndex: uint(li),
			From: topicToAddress(l.Topics[1]), To: topicToAddress(l.Topics[2]),
			TokenAmount: val, BlockNumber: bn,
		})
	}
	return out, nil
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
	if !ok {
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
