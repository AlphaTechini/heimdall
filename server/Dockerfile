# heimdalld container image.
#
# Build (from the repository root or from server/):
#   docker build -t heimdalld server/
#
# The binary reads two config files at runtime that live OUTSIDE server/ in the repository:
#   ../config/targets.<network>.json   (TARGETS_FILE)
#   ../config/signals.json             (SIGNALS_FILE)
# They are deliberately not baked into the image. Mount the repository's config/ folder, e.g.:
#   docker run --rm -p 8080:8080 \
#     -v "$PWD/config:/config:ro" -v "$PWD/contracts/deployments:/deployments:ro" \
#     -e TARGETS_FILE=/config/targets.arbitrum-one.json -e SIGNALS_FILE=/config/signals.json \
#     -e DEPLOYMENT_FILE=/deployments/42161.json \
#     -e DATABASE_URL=... -e RPC_HTTP_URL=... -e KEEPER_PRIVATE_KEY=... -e AUTH_SECRET=... heimdalld
# (or set FACTORY_ADDRESS instead of DEPLOYMENT_FILE). See server/.env.example for every variable.

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/heimdalld ./cmd/heimdalld

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/heimdalld /heimdalld
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/heimdalld"]
