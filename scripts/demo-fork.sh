#!/usr/bin/env bash
# The demo chain: a local anvil FORK of Arbitrum One with real protocol contracts (specs N4).
# Needs your own Arbitrum One RPC URL (the cloud build sandbox cannot reach one).
#
#   export ARBITRUM_ONE_RPC_URL=https://...
#   bash scripts/demo-fork.sh
#
# What it does: starts `anvil --fork-url ... --chain-id 31337 --block-time 1`, deploys
# HeimdallGuardFactory (Deploy.s.sol) with anvil's default keys, and runs `heimdalld demo-seed`
# (funds Ada and Ben, deposits into the ERC-4626 target and Aave V3). Before this works, fill the
# PLACEHOLDERS in config/targets.arbitrum-one.json (a real Morpho USDC vault address, the
# USDC source account, feeds); demo-seed lists what is missing.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
if [[ -f "$HOME/.heimdall-env" ]]; then source "$HOME/.heimdall-env"; fi
if [[ -z "${ARBITRUM_ONE_RPC_URL:-}" ]]; then
  echo "ARBITRUM_ONE_RPC_URL is not set. Export your Arbitrum One RPC URL first (for example from Alchemy or QuickNode)." >&2
  exit 1
fi
RPC="${RPC_URL:-http://127.0.0.1:8545}"
PORT="${RPC##*:}"; PORT="${PORT%%/*}"
TARGETS="$ROOT/config/targets.arbitrum-one.json"

# PUBLIC TEST KEYS: anvil's well-known default accounts. Fork only; they hold nothing real.
ANVIL_KEY_0="0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"  # deployer / factory admin
ANVIL_KEY_9="0x2a871d0798f97d79848a013d4936a73bf4cc922c825d33c1cf7073dff6d409c6"  # keeper
KEEPER_ADDRESS="$(cast wallet address --private-key "$ANVIL_KEY_9")"

# Some npm-wrapped foundry builds exit 0 even when the node is unreachable, so check the output.
up() { [[ "$(cast chain-id --rpc-url "$RPC" 2>/dev/null)" =~ ^[0-9]+$ ]]; }
if up; then
  echo "Something already answers at $RPC. Stop it first (pkill anvil) so the fork starts clean." >&2
  exit 1
fi
mkdir -p "$ROOT/.local"
echo ">> starting the Arbitrum One fork"
nohup anvil --fork-url "$ARBITRUM_ONE_RPC_URL" --chain-id 31337 --block-time 1 --port "$PORT" >"$ROOT/.local/anvil-fork.log" 2>&1 &
for _ in $(seq 1 100); do up && break; sleep 0.3; done
up || { echo "the fork did not start; see .local/anvil-fork.log" >&2; exit 1; }

AAVE_POOL="$(jq -r '.aaveV3.pool.address' "$TARGETS")"
echo ">> deploying HeimdallGuardFactory (Aave pool $AAVE_POOL, keeper $KEEPER_ADDRESS)"
rm -f "$ROOT/contracts/deployments/31337.json"
( cd "$ROOT/contracts" && PRIVATE_KEY="$ANVIL_KEY_0" KEEPER_ADDRESS="$KEEPER_ADDRESS" AAVE_V3_POOL="$AAVE_POOL" \
    forge script script/Deploy.s.sol --rpc-url "$RPC" --broadcast )

[[ -f "$ROOT/contracts/deployments/31337.json" ]] || { echo "the factory deployment failed" >&2; exit 1; }
echo ">> seeding Ada and Ben"
( cd "$ROOT/server" && RPC_HTTP_URL="$RPC" TARGETS_FILE="$TARGETS" go run ./cmd/heimdalld demo-seed )

cat <<OUT

Fork is ready. Run the server (from server/) with the lines below. They match what server/.env
needs for the fork demo; if your server/.env already has these values, just run the last line.
DATABASE_URL, AUTH_SECRET and the alert settings always come from server/.env.

  export RPC_HTTP_URL=$RPC
  export TARGETS_FILE=../config/targets.arbitrum-one.json
  export SIGNALS_FILE=../config/signals.json
  export DEPLOYMENT_FILE=../contracts/deployments/31337.json
  export KEEPER_PRIVATE_KEY=$ANVIL_KEY_9   # public anvil test key
  export DEMO_MODE=true
  go run ./cmd/heimdalld
OUT
