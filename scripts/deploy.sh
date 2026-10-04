#!/usr/bin/env bash
# Deploy and verify HeimdallGuardFactory (+ HeimdallGuard implementation) on an Arbitrum chain.
# Run on the builder's machine (cloud sessions cannot reach Arbitrum RPC).
#
#   bash scripts/deploy.sh wallets   # once: creates a deployer and a keeper wallet in .env, prints their addresses
#   bash scripts/deploy.sh sepolia   # fund the deployer first (~0.01 Arbitrum Sepolia ETH)
#   bash scripts/deploy.sh one       # Arbitrum One mainnet: experimental and unaudited (specs X6)
#   bash scripts/deploy.sh sepolia verify   # retry Arbiscan verification of the last deployment only
#
# Values come from the repo-root .env (git-ignored), then ~/.heimdall-env; variables already exported
# in the shell win. Needed: PRIVATE_KEY (deployer), KEEPER_ADDRESS, ARBISCAN_API_KEY (Etherscan V2 key,
# works for Arbiscan) and ARBITRUM_SEPOLIA_RPC_URL / ARBITRUM_ONE_RPC_URL.
# Writes contracts/deployments/<chainId>.json and prints the addresses with Arbiscan links.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="$ROOT/.env"
WANTED="PRIVATE_KEY KEEPER_ADDRESS KEEPER_PRIVATE_KEY ARBISCAN_API_KEY ARBITRUM_SEPOLIA_RPC_URL ARBITRUM_ONE_RPC_URL AAVE_V3_POOL"

# Read KEY=VALUE lines without executing the file (values may contain spaces or <>), only for the
# keys above, never overriding what the shell already exports.
load_env() {
  local file="$1" k v
  [[ -f "$file" ]] || return 0
  while IFS='=' read -r k v || [[ -n "$k" ]]; do
    k="${k%$'\r'}"; v="${v%$'\r'}"
    k="${k#export }"; k="${k//[[:space:]]/}"
    [[ -z "$k" || "$k" == \#* ]] && continue
    [[ " $WANTED " == *" $k "* ]] || continue
    v="${v#\"}"; v="${v%\"}"; v="${v#\'}"; v="${v%\'}"
    [[ -n "$v" && -z "${!k:-}" ]] && export "$k=$v"
  done < "$file"
}
load_env "$ENV_FILE"
if [[ -f "$HOME/.heimdall-env" ]]; then source "$HOME/.heimdall-env"; fi

NET="${1:-}"

if [[ "$NET" == wallets ]]; then
  # Creates fresh keys locally with cast and appends them to the git-ignored .env. Keys are never printed.
  git -C "$ROOT" check-ignore -q "$ENV_FILE" || { echo ".env is not git-ignored; refusing to write keys to it." >&2; exit 1; }
  new_wallet() { cast wallet new | tr -d '\r'; }
  field() { grep -i "^$1" | head -1 | awk '{print $NF}'; }
  touch "$ENV_FILE"
  if [[ -z "${PRIVATE_KEY:-}" ]]; then
    w="$(new_wallet)"; printf '\n# Deployer (created by scripts/deploy.sh wallets)\nPRIVATE_KEY=%s\n' "$(field 'private key' <<<"$w")" >> "$ENV_FILE"
    echo "Deployer created: $(field address <<<"$w")  <- fund this with ~0.01 Arbitrum Sepolia ETH"
  else
    echo "Deployer already in .env: $(cast wallet address --private-key "$PRIVATE_KEY")"
  fi
  if [[ -z "${KEEPER_ADDRESS:-}" ]]; then
    w="$(new_wallet)"
    printf '\n# Keeper (created by scripts/deploy.sh wallets; needs ETH only if a server runs on this chain)\nKEEPER_PRIVATE_KEY=%s\nKEEPER_ADDRESS=%s\n' \
      "$(field 'private key' <<<"$w")" "$(field address <<<"$w")" >> "$ENV_FILE"
    echo "Keeper created:   $(field address <<<"$w")"
  else
    echo "Keeper already in .env: $KEEPER_ADDRESS"
  fi
  echo "Saved in $ENV_FILE (git-ignored). Next: fund the deployer, then: bash scripts/deploy.sh sepolia"
  exit 0
fi

case "$NET" in
  sepolia) RPC_ALIAS=arbitrum_sepolia; RPC_VAR=ARBITRUM_SEPOLIA_RPC_URL; EXPLORER=https://sepolia.arbiscan.io
           POOL_DEFAULT=0xBfC91D59fdAA134A4ED45f7B584cAf96D7792Eff ;; # bgd-labs/aave-address-book AaveV3ArbitrumSepolia.POOL
  one)     RPC_ALIAS=arbitrum_one; RPC_VAR=ARBITRUM_ONE_RPC_URL; EXPLORER=https://arbiscan.io
           POOL_DEFAULT=0x794a61358D6845594F94dc1DB02A252b5b4814aD ;; # bgd-labs/aave-address-book AaveV3Arbitrum.POOL
  *) echo "usage: bash scripts/deploy.sh wallets|sepolia|one" >&2; exit 1 ;;
esac

missing=()
for v in PRIVATE_KEY KEEPER_ADDRESS ARBISCAN_API_KEY "$RPC_VAR"; do
  [[ -n "${!v:-}" ]] || missing+=("$v")
done
if (( ${#missing[@]} )); then
  echo "Missing: ${missing[*]}. Add them to $ENV_FILE (or run: bash scripts/deploy.sh wallets)." >&2; exit 1
fi

MODE="${2:-deploy}"
if [[ "$MODE" == verify ]]; then
  # Re-uses the saved broadcast (contracts/broadcast/...): verifies, sends nothing new.
  unset FOUNDRY_OFFLINE
  export AAVE_V3_POOL="${AAVE_V3_POOL:-$POOL_DEFAULT}"
  cd "$ROOT/contracts"
  exec forge script script/Deploy.s.sol --rpc-url "$RPC_ALIAS" --resume --verify -vvv
fi

DEPLOYER="$(cast wallet address --private-key "$PRIVATE_KEY")"
BAL="$(cast balance "$DEPLOYER" --rpc-url "${!RPC_VAR}" --ether)"
echo "Network:  $NET   Deployer: $DEPLOYER ($BAL ETH)   Keeper: $KEEPER_ADDRESS"
if [[ "$(awk -v b="$BAL" 'BEGIN{print (b+0 < 0.001) ? "low" : "ok"}')" == low ]]; then
  echo "The deployer has under 0.001 ETH on $NET. Fund $DEPLOYER first." >&2; exit 1
fi

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

CHAIN_ID="$(cast chain-id --rpc-url "${!RPC_VAR}")"
OUT="$ROOT/contracts/deployments/$CHAIN_ID.json"
[[ -f "$OUT" ]] || { echo "Deployment file $OUT was not written; see the forge output above." >&2; exit 1; }
FACTORY="$(jq -r .factory "$OUT")"; IMPL="$(jq -r .implementation "$OUT")"
cat <<DONE

Deployed on $NET (chain $CHAIN_ID). Saved to contracts/deployments/$CHAIN_ID.json
  HeimdallGuardFactory          $FACTORY
    $EXPLORER/address/$FACTORY#code
  HeimdallGuard implementation  $IMPL
    $EXPLORER/address/$IMPL#code
  Keeper                        $KEEPER_ADDRESS
  Aave V3 Pool                  $AAVE_V3_POOL

README Deployments row:
| Arbitrum $( [[ $NET == one ]] && echo "One (experimental, unaudited)" || echo Sepolia ) | [\`$FACTORY\`]($EXPLORER/address/$FACTORY#code) | [\`$IMPL\`]($EXPLORER/address/$IMPL#code) |

Open the links: the Contract tab should show a green check (verified). If verification failed (Arbiscan
sometimes needs a minute), retry verification only, without deploying again:
  bash scripts/deploy.sh $NET verify
DONE
