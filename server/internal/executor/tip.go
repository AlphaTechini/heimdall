package executor

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"sync"
	"time"

	"github.com/AlphaTechini/heimdall/server/internal/chain"
	"github.com/AlphaTechini/heimdall/server/internal/config"
	"github.com/AlphaTechini/heimdall/server/internal/signals"
	"github.com/ethereum/go-ethereum/common"
)

// priceFeed reads the ETH/USD Chainlink-style feed with a short cache. With no feed configured
// there is no price and the priority tip is off: a price is never invented (specs N7).
type priceFeed struct {
	ch   *chain.Client
	addr common.Address
	on   bool

	mu     sync.Mutex
	price  chain.Price
	loaded time.Time
}

func newPriceFeed(ch *chain.Client, a config.AddrSrc) *priceFeed {
	return &priceFeed{ch: ch, addr: a.Addr(), on: a.Set()}
}

func (p *priceFeed) get(ctx context.Context) (chain.Price, error) {
	if !p.on {
		return chain.Price{}, fmt.Errorf("no ETH/USD price feed is configured")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.price.Answer != nil && time.Since(p.loaded) < 5*time.Second {
		return p.price, nil
	}
	pr, err := p.ch.LatestPrice(ctx, p.addr, nil)
	if err != nil {
		return chain.Price{}, fmt.Errorf("cannot read the ETH/USD price feed: %w", err)
	}
	p.price, p.loaded = pr, time.Now()
	return pr, nil
}

// capWei converts a USD cap into wei: usd / (ethUsd) * 1e18.
func capWei(usd float64, eth chain.Price) *big.Int {
	micro := new(big.Int).SetInt64(int64(math.Round(usd * 1e6)))
	num := new(big.Int).Mul(micro, chain.Pow10(18))
	num.Mul(num, chain.Pow10(eth.Decimals))
	den := new(big.Int).Mul(eth.Answer, big.NewInt(1_000_000))
	return num.Quo(num, den)
}

// weiToUSD converts wei back to USD for display.
func weiToUSD(wei *big.Int, eth chain.Price) float64 {
	f := new(big.Float).SetInt(wei)
	f.Mul(f, new(big.Float).SetInt(eth.Answer))
	f.Quo(f, new(big.Float).SetInt(chain.Pow10(18+eth.Decimals)))
	v, _ := f.Float64()
	return v
}

// tipPlan is the priority fee decision for one transaction.
type tipPlan struct {
	tipPerGas *big.Int // maxPriorityFeePerGas
	capPerGas *big.Int // the highest tip per gas the user's cap allows (for replacements)
	eth       chain.Price
	note      string // why the tip is zero, when it is
}

// planTip implements specs W6 and details.md §9:
//
//	tipPerGas = min(capWei, capWei*budgetPct/100) / gasLimit
//
// where capWei is the user's USD cap converted with the ETH/USD feed. Dividing by the gas limit
// (the most gas the transaction can use) guarantees tip*gasLimit <= capWei: the cap is never exceeded.
func planTip(ctx context.Context, feed *priceFeed, sig *config.Signals, severity string, tipCapUSD float64, priority bool, gasLimit uint64) tipPlan {
	zero := big.NewInt(0)
	if !priority {
		return tipPlan{tipPerGas: zero, capPerGas: zero, note: "priority exit is turned off in the policy"}
	}
	eth, err := feed.get(ctx)
	if err != nil {
		return tipPlan{tipPerGas: zero, capPerGas: zero, note: err.Error() + ", so no priority tip is added"}
	}
	c := capWei(tipCapUSD, eth)
	pctBudget := sig.Tip.WarningBudgetPct
	if severity == signals.SevCritical {
		pctBudget = sig.Tip.CriticalBudgetPct
	}
	budgetF := new(big.Float).Mul(new(big.Float).SetInt(c), big.NewFloat(pctBudget/100))
	budget, _ := budgetF.Int(nil)
	if budget.Cmp(c) > 0 {
		budget = c
	}
	gl := new(big.Int).SetUint64(gasLimit)
	return tipPlan{tipPerGas: new(big.Int).Quo(budget, gl), capPerGas: new(big.Int).Quo(c, gl), eth: eth}
}
