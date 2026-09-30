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
	tipPerGas *big.Int // maxPriorityFeePerGas for this transaction
	capPerGas *big.Int // the highest tip per gas the remaining budget allows (for replacements)
	eth       chain.Price
	note      string // why the tip is zero or reduced, when it is
}

// planTip implements specs W6 and details.md §9 with the owner's cap as a budget for the WHOLE
// exit job, not per transaction:
//
//	budgetWei    = min(capWei, capWei*budgetPct/100)      (fixed when the job sends its first tx)
//	available    = budgetWei - tips already paid by mined txs (tip x gas used)
//	tipPerGas    = available / gasLimit
//
// A replacement transaction replaces the exposure of the one it supersedes (only one of them can
// be mined), so it draws from the same `available`. tip x gasLimit is therefore always within what
// is left of the cap. When the budget is used up the exit keeps going with tip 0.
func (x *Executor) planTip(j *job, gasLimit uint64) tipPlan {
	zero := big.NewInt(0)
	if !j.pol.PriorityExit {
		return tipPlan{tipPerGas: zero, capPerGas: zero, note: "priority exit is turned off in the policy"}
	}
	eth, err := x.feed.get(x.ctx)
	if err != nil {
		return tipPlan{tipPerGas: zero, capPerGas: zero, note: err.Error() + ", so no priority tip is added"}
	}
	if j.budget == nil {
		c := capWei(j.pol.TipCapUSD, eth)
		pctBudget := x.sig.Tip.WarningBudgetPct
		if j.severity == signals.SevCritical {
			pctBudget = x.sig.Tip.CriticalBudgetPct
		}
		b, _ := new(big.Float).Mul(new(big.Float).SetInt(c), big.NewFloat(pctBudget/100)).Int(nil)
		if b.Cmp(c) > 0 {
			b = c
		}
		j.budget = b
	}
	avail := new(big.Int).Sub(j.budget, j.paid)
	perGas := new(big.Int)
	if avail.Sign() > 0 {
		perGas.Quo(avail, new(big.Int).SetUint64(gasLimit))
	}
	p := tipPlan{tipPerGas: perGas, capPerGas: new(big.Int).Set(perGas), eth: eth}
	if perGas.Sign() == 0 {
		p.note = fmt.Sprintf("the $%.2f priority-tip budget for this exit is used up, so the rest is sent without a tip", j.pol.TipCapUSD)
	}
	return p
}
