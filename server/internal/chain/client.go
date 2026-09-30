package chain

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// Client is the HTTP client (plus an optional WS endpoint for block subscriptions).
type Client struct {
	Eth     *ethclient.Client
	RPC     *rpc.Client
	ChainID *big.Int
	WSURL   string
}

// Dial connects to the HTTP RPC endpoint and reads the chain id.
func Dial(ctx context.Context, httpURL, wsURL string) (*Client, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	r, err := rpc.DialContext(ctx, httpURL)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to RPC_HTTP_URL: %w", err)
	}
	c := &Client{Eth: ethclient.NewClient(r), RPC: r, WSURL: wsURL}
	c.ChainID, err = c.Eth.ChainID(ctx)
	if err != nil {
		r.Close()
		return nil, fmt.Errorf("RPC_HTTP_URL did not answer eth_chainId (is the node running?): %w", err)
	}
	return c, nil
}

// ClientVersion returns web3_clientVersion (anvil reports "anvil/vX").
func (c *Client) ClientVersion(ctx context.Context) (string, error) {
	var v string
	err := c.RPC.CallContext(ctx, &v, "web3_clientVersion")
	return v, err
}

// Raw calls any JSON-RPC method (used for the anvil cheat methods).
func (c *Client) Raw(ctx context.Context, result any, method string, args ...any) error {
	return c.RPC.CallContext(ctx, result, method, args...)
}

// CallRaw runs an eth_call and returns the raw bytes.
func (c *Client) CallRaw(ctx context.Context, from *common.Address, to common.Address, data []byte, blk *big.Int) ([]byte, error) {
	msg := ethereum.CallMsg{To: &to, Data: data}
	if from != nil {
		msg.From = *from
	}
	return c.Eth.CallContract(ctx, msg, blk)
}

// Call packs method+args, runs an eth_call at blk (nil = latest) and unpacks the outputs.
func (c *Client) Call(ctx context.Context, a *abi.ABI, to common.Address, blk *big.Int, method string, args ...any) ([]any, error) {
	data, err := a.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	out, err := c.CallRaw(ctx, nil, to, data, blk)
	if err != nil {
		return nil, fmt.Errorf("%s on %s: %w", method, to.Hex(), err)
	}
	res, err := a.Unpack(method, out)
	if err != nil {
		return nil, fmt.Errorf("%s on %s returned unexpected data: %w", method, to.Hex(), err)
	}
	return res, nil
}

// ToBig converts any unsigned/signed integer decoded by go-ethereum's ABI into *big.Int.
func ToBig(v any) (*big.Int, error) {
	switch x := v.(type) {
	case *big.Int:
		return x, nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return new(big.Int).SetUint64(rv.Uint()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return big.NewInt(rv.Int()), nil
	}
	return nil, fmt.Errorf("value %v (%T) is not an integer", v, v)
}

// Uint calls a method whose first output is an integer.
func (c *Client) Uint(ctx context.Context, a *abi.ABI, to common.Address, blk *big.Int, method string, args ...any) (*big.Int, error) {
	res, err := c.Call(ctx, a, to, blk, method, args...)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, fmt.Errorf("%s returned nothing", method)
	}
	return ToBig(res[0])
}

// Addr calls a method whose first output is an address.
func (c *Client) Addr(ctx context.Context, a *abi.ABI, to common.Address, blk *big.Int, method string, args ...any) (common.Address, error) {
	res, err := c.Call(ctx, a, to, blk, method, args...)
	if err != nil {
		return common.Address{}, err
	}
	v, ok := res[0].(common.Address)
	if !ok {
		return common.Address{}, fmt.Errorf("%s did not return an address", method)
	}
	return v, nil
}

// Bool calls a method whose first output is a bool.
func (c *Client) Bool(ctx context.Context, a *abi.ABI, to common.Address, blk *big.Int, method string, args ...any) (bool, error) {
	res, err := c.Call(ctx, a, to, blk, method, args...)
	if err != nil {
		return false, err
	}
	v, ok := res[0].(bool)
	if !ok {
		return false, fmt.Errorf("%s did not return a bool", method)
	}
	return v, nil
}

// BalanceOf reads an ERC-20 balance.
func (c *Client) BalanceOf(ctx context.Context, token, who common.Address, blk *big.Int) (*big.Int, error) {
	return c.Uint(ctx, &ERC20ABI, token, blk, "balanceOf", who)
}

// Price is a Chainlink-style answer with its decimals.
type Price struct {
	Answer   *big.Int
	Decimals int
}

// Float returns the price as a float64.
func (p Price) Float() float64 {
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(p.Answer), new(big.Float).SetInt(pow10(p.Decimals))).Float64()
	return f
}

func pow10(n int) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }

// Pow10 returns 10^n.
func Pow10(n int) *big.Int { return pow10(n) }

// LatestPrice reads a Chainlink-style feed (latestRoundData + decimals).
func (c *Client) LatestPrice(ctx context.Context, feed common.Address, blk *big.Int) (Price, error) {
	res, err := c.Call(ctx, &AggregatorABI, feed, blk, "latestRoundData")
	if err != nil {
		return Price{}, err
	}
	ans, err := ToBig(res[1])
	if err != nil {
		return Price{}, err
	}
	if ans.Sign() <= 0 {
		return Price{}, fmt.Errorf("feed %s returned a non-positive answer", feed.Hex())
	}
	d, err := c.Uint(ctx, &AggregatorABI, feed, blk, "decimals")
	if err != nil {
		return Price{}, err
	}
	return Price{Answer: ans, Decimals: int(d.Int64())}, nil
}

// Reserve holds the parts of Aave's ReserveDataLegacy that Heimdall reads.
type Reserve struct {
	LiquidityIndex *big.Int
	AToken         common.Address
}

// AaveReserve reads getReserveData(asset). Layout: contracts/src/interfaces/IAavePool.sol
// (ReserveDataLegacy): word 0 configuration, word 1 liquidityIndex, word 8 aTokenAddress.
func (c *Client) AaveReserve(ctx context.Context, pool, asset common.Address, blk *big.Int) (Reserve, error) {
	data, err := AavePoolABI.Pack("getReserveData", asset)
	if err != nil {
		return Reserve{}, err
	}
	out, err := c.CallRaw(ctx, nil, pool, data, blk)
	if err != nil {
		return Reserve{}, fmt.Errorf("Aave getReserveData: %w", err)
	}
	if len(out) < 32*15 {
		return Reserve{}, errors.New("Aave getReserveData returned too little data")
	}
	return Reserve{
		LiquidityIndex: new(big.Int).SetBytes(out[32*1 : 32*2]),
		AToken:         common.BytesToAddress(out[32*8 : 32*9]),
	}, nil
}

// HeaderTime converts a header timestamp.
func HeaderTime(h *types.Header) time.Time { return time.Unix(int64(h.Time), 0).UTC() }

// IsRevert reports whether an RPC error looks like an EVM revert.
func IsRevert(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := ethclient.RevertErrorData(err); ok {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "revert")
}
