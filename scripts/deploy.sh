#!/usr/bin/env bash
# Deploy and verify HeimdallGuardFactory (+ HeimdallGuard implementation) on an Arbitrum chain.
# Run on the builder's machine (cloud sessions cannot reach Arbitrum RPC).
#
#   export PRIVATE_KEY=0x...            # deployer, funded with a little ETH on the target chain
#   export KEEPER_ADDRESS=0x...         # address of the server's KEEPER_PRIVATE_KEY
#   export ARBISCAN_API_KEY=...         # Etherscan V2 key (works for Arbiscan)
#   export ARBITRUM_SEPOLIA_RPC_URL=... # or ARBITRUM_ONE_RPC_URL
#   bash scripts/deploy.sh sepolia      # or: bash scripts/deploy.sh one
#
# Writes contracts/deployments/<chainId>.json. Arbitrum One deployments are experimental and
# unaudited (specs X6): label them that way everywhere.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
if [[ -f "$HOME/.heimdall-env" ]]; then source "$HOME/.heimdall-env"; fi

NET="${1:-}"
case "$NET" in
  sepolia) RPC_ALIAS=arbitrum_sepolia; RPC_VAR=ARBITRUM_SEPOLIA_RPC_URL
           POOL_DEFAULT=0xBfC91D59fdAA134A4ED45f7B584cAf96D7792Eff ;; # bgd-labs/aave-address-book AaveV3ArbitrumSepolia.POOL
  one)     RPC_ALIAS=arbitrum_one; RPC_VAR=ARBITRUM_ONE_RPC_URL
           POOL_DEFAULT=0x794a61358D6845594F94dc1DB02A252b5b4814aD ;; # bgd-labs/aave-address-book AaveV3Arbitrum.POOL
  *) echo "usage: bash scripts/deploy.sh sepolia|one" >&2; exit 1 ;;
esac

missing=()
for v in PRIVATE_KEY KEEPER_ADDRESS ARBISCAN_API_KEY "$RPC_VAR"; do
  [[ -n "${!v:-}" ]] || missing+=("$v")
done
if (( ${#missing[@]} )); then echo "Missing env: ${missing[*]}" >&2; exit 1; fi

if [[ "$NET" == one ]]; then
  echo "Deploying to Arbitrum One MAINNET. This code is experimental and unaudited."
  read -r -p "Type 'deploy' to continue: " ok
  [[ "$ok" == deploy ]] || { echo "Aborted."; exit 1; }
fi

export AAVE_V3_POOL="${AAVE_V3_POOL:-$POOL_DEFAULT}"
# Real deployments need real solc; the cloud-only offline flag must not leak in.
unset FOUNDRY_OFFLINE
cd "$ROOT/contracts"
forge script script/Deploy.s.sol --rpc-url "$RPC_ALIAS" --broadcast --verify -vvv
echo "Deployment written to contracts/deployments/. Add the addresses to README.md (Status and Deployments)."
