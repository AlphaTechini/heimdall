#!/usr/bin/env bash
# Cloud-only toolchain setup for Heimdall build sessions.
# Local development does NOT need this: install Go 1.26, Foundry and pnpm normally.
#
# Why this exists: the cloud sandbox blocks proxy.golang.org, go.dev downloads,
# foundry.paradigm.xyz, GitHub release downloads and binaries.soliditylang.org.
# It allows GitHub git clones and the npm registry. So:
#   - Go 1.26 is built from source (git clone + make.bash, ~4-5 min)
#   - Go modules are fetched with GOPROXY=direct (git), GOSUMDB=off
#   - forge/anvil/cast come from npm (@foundry-rs/*)
#   - solc comes from npm (solc-js) through a small wrapper forge can call
#
# Usage:  bash scripts/cloud-setup.sh && source ~/.heimdall-env
set -euo pipefail

GO_TAG="${GO_TAG:-go1.26.8}"
SOLC_VERSION="${SOLC_VERSION:-0.8.28}"
TOOLS="$HOME/.heimdall-tools"
mkdir -p "$TOOLS" "$HOME/.local/bin"

# ---------- Go 1.26 ----------
if [[ ! -x "$TOOLS/go/bin/go" ]]; then
  echo ">> Building $GO_TAG from source (uses the preinstalled Go as bootstrap)..."
  rm -rf "$TOOLS/go"
  git clone --depth 1 -b "$GO_TAG" https://github.com/golang/go "$TOOLS/go"
  ( cd "$TOOLS/go/src" && GOROOT_BOOTSTRAP="$(dirname "$(dirname "$(readlink -f "$(command -v go)")")")" ./make.bash )
fi

# ---------- Foundry (forge, anvil, cast) via npm ----------
if [[ ! -x "$TOOLS/foundry/node_modules/.bin/forge" ]]; then
  echo ">> Installing Foundry from npm..."
  mkdir -p "$TOOLS/foundry"
  ( cd "$TOOLS/foundry" && [[ -f package.json ]] || echo '{"name":"foundry-tools","private":true}' > package.json
    pnpm add @foundry-rs/forge @foundry-rs/anvil @foundry-rs/cast )
fi
# pnpm's .bin shims resolve relative paths, so use small exec wrappers instead of symlinks.
for b in forge anvil cast; do
  rm -f "$HOME/.local/bin/$b"
  printf '#!/usr/bin/env bash\nexec "%s" "$@"\n' "$TOOLS/foundry/node_modules/.bin/$b" > "$HOME/.local/bin/$b"
  chmod +x "$HOME/.local/bin/$b"
done

# ---------- solc via solc-js + wrapper ----------
if [[ ! -d "$TOOLS/solc/node_modules/solc" ]]; then
  echo ">> Installing solc-js $SOLC_VERSION from npm..."
  mkdir -p "$TOOLS/solc"
  ( cd "$TOOLS/solc" && [[ -f package.json ]] || echo '{"name":"solc-tools","private":true}' > package.json
    pnpm add "solc@$SOLC_VERSION" )
fi
cat > "$TOOLS/solc/solc" <<'EOF'
#!/usr/bin/env bash
# Makes solc-js look like native solc for forge (--version and --standard-json).
DIR="$(cd "$(dirname "$0")" && pwd)"
if [[ "${1:-}" == "--version" ]]; then
  v=$("$DIR/node_modules/.bin/solcjs" --version | sed 's/.Emscripten.clang//')
  echo "solc, the solidity compiler commandline interface"
  echo "Version: ${v}.Linux.g++"
  exit 0
fi
exec node -e '
const solc = require(process.argv[1]);
const fs = require("fs"), path = require("path");
let input = "";
process.stdin.on("data", d => input += d).on("end", () => {
  const findImports = p => {
    const f = path.resolve(process.cwd(), p);
    return fs.existsSync(f) ? { contents: fs.readFileSync(f, "utf8") } : { error: "File not found: " + p };
  };
  process.stdout.write(solc.compile(input, { import: findImports }));
});' "$DIR/node_modules/solc"
EOF
chmod +x "$TOOLS/solc/solc"

# ---------- env file ----------
cat > "$HOME/.heimdall-env" <<EOF
export PATH="$TOOLS/go/bin:\$HOME/go/bin:\$HOME/.local/bin:\$PATH"
export GOTOOLCHAIN=local
export GOPROXY=direct
export GOSUMDB=off
export FOUNDRY_SOLC="$TOOLS/solc/solc"
export FOUNDRY_OFFLINE=true
EOF

# shellcheck disable=SC1091
source "$HOME/.heimdall-env"
echo ">> Done."
go version
forge --version | head -1
echo "Run: source ~/.heimdall-env"
