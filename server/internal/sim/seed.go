package sim

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/AlphaTechini/heimdall/server/internal/chain"
	"github.com/AlphaTechini/heimdall/server/internal/config"
	"github.com/ethereum/go-ethereum/common"
)

// Seed prepares the Arbitrum One fork for the demo (`heimdalld demo-seed`): Ada and Ben get
// USDC (by impersonating demo.usdcSource), both deposit 10,000 USDC into the ERC-4626 target and
// Ada supplies 5,000 USDC to the real Aave V3 Pool. It refuses to run anywhere except anvil.
func Seed(ctx context.Context, ch *chain.Client, tg *config.Targets, logf func(string, ...any)) error {
	if ch.ChainID.Uint64() != 31337 {
		return fmt.Errorf("demo-seed only runs on the local fork (chain id 31337), not chain %s", ch.ChainID)
	}
	if v, err := ch.ClientVersion(ctx); err != nil || !strings.HasPrefix(strings.ToLower(v), "anvil") {
		return fmt.Errorf("demo-seed only runs on anvil (the node says %q)", v)
	}
	var vault, aave *config.Target
	for i := range tg.Targets {
		t := &tg.Targets[i]
		if t.Type == "ERC4626" && vault == nil {
			vault = t
		}
		if t.Type == "AAVE_V3" && aave == nil {
			aave = t
		}
	}
	var missing []string
	if vault == nil {
		missing = append(missing, "an ERC-4626 target with a real vault address in the targets file (targets[].address for \"morpho-usdc\": pick a vault on Arbitrum One and paste its address and source)")
	}
	if aave == nil {
		missing = append(missing, "an AAVE_V3 target in the targets file")
	}
	if !tg.AaveV3.Pool.Set() {
		missing = append(missing, "aaveV3.pool.address in the targets file")
	}
	if !tg.Demo.USDCSource.Set() {
		missing = append(missing, "demo.usdcSource.address: an account that holds plenty of Arbitrum One USDC (find a large holder on Arbiscan, record the URL in demo.usdcSource.source)")
	}
	if tg.Demo.Ada == "" || tg.Demo.Ben == "" {
		missing = append(missing, "demo.ada and demo.ben wallet addresses")
	}
	if len(missing) > 0 {
		return fmt.Errorf("the demo cannot be seeded yet. Fill in the following in the targets file:\n  - %s", strings.Join(missing, "\n  - "))
	}
	ada, ben := common.HexToAddress(tg.Demo.Ada), common.HexToAddress(tg.Demo.Ben)
	src := tg.Demo.USDCSource.Addr()
	usdc := vault.AssetAddr()
	unit := chain.Pow10(vault.AssetDecimals)
	amt := func(n int64) *big.Int { return new(big.Int).Mul(big.NewInt(n), unit) }

	s := &Sim{ch: ch}
	for _, a := range []common.Address{src, ada, ben} {
		if err := s.impersonate(ctx, a); err != nil {
			return err
		}
	}
	send := func(from, to common.Address, data []byte, what string) error {
		logf("%s", what)
		return s.send(ctx, from, to, data)
	}
	bal, err := ch.BalanceOf(ctx, usdc, src, nil)
	if err != nil {
		return err
	}
	if bal.Cmp(amt(25_000)) < 0 {
		return fmt.Errorf("demo.usdcSource %s holds only %s USDC on this fork; it needs at least 25,000", src.Hex(), new(big.Int).Div(bal, unit))
	}
	for _, f := range []struct {
		to common.Address
		n  int64
	}{{ada, 15_000}, {ben, 10_000}} {
		data, _ := chain.ERC20ABI.Pack("transfer", f.to, amt(f.n))
		if err := send(src, usdc, data, fmt.Sprintf("Sending %d USDC to %s", f.n, f.to.Hex())); err != nil {
			return err
		}
	}
	dep := func(who common.Address, n int64) error {
		ap, _ := chain.ERC20ABI.Pack("approve", vault.Addr(), amt(n))
		if err := send(who, usdc, ap, fmt.Sprintf("%s approves the vault", who.Hex())); err != nil {
			return err
		}
		d, _ := chain.ERC4626ABI.Pack("deposit", amt(n), who)
		return send(who, vault.Addr(), d, fmt.Sprintf("%s deposits %d USDC into %s", who.Hex(), n, vault.Label))
	}
	if err := dep(ada, 10_000); err != nil {
		return err
	}
	if err := dep(ben, 10_000); err != nil {
		return err
	}
	pool := tg.AaveV3.Pool.Addr()
	ap, _ := chain.ERC20ABI.Pack("approve", pool, amt(5_000))
	if err := send(ada, usdc, ap, "Ada approves the Aave V3 Pool"); err != nil {
		return err
	}
	sup, _ := chain.AavePoolABI.Pack("supply", usdc, amt(5_000), ada, uint16(0))
	if err := send(ada, pool, sup, "Ada supplies 5,000 USDC to Aave V3 (the real Pool)"); err != nil {
		return err
	}
	for _, who := range []common.Address{ada, ben} {
		sh, _ := ch.BalanceOf(ctx, vault.Addr(), who, nil)
		wal, _ := ch.BalanceOf(ctx, usdc, who, nil)
		logf("%s: %s vault shares, %s USDC left in wallet", who.Hex(), sh, new(big.Int).Div(wal, unit))
	}
	at, _ := ch.BalanceOf(ctx, aave.PositionToken(), ada, nil)
	logf("Ada's Aave aToken balance: %s", at)
	return nil
}
