# syntax=docker/dockerfile:1.7
# devspaces portal — Go API + embedded web UI. Build context is the repository
# root so the catalog can be copied in:  docker build -f portal/Dockerfile .
#
# Stage 1 (optional): compile Tailwind. The committed web/tailwind.css is used
# when Node/npm is unavailable (set SKIP_CSS=1), so an air-gapped build works.
FROM node:22-alpine AS css
ARG SKIP_CSS=0
WORKDIR /src
COPY portal/package.json portal/tailwind.config.js ./
COPY portal/web ./web
RUN if [ "$SKIP_CSS" = "1" ]; then echo "using committed tailwind.css"; \
    else npm install --no-audit --no-fund && npm run build:css; fi

# Stage 2: Go binary (stdlib only; CGO off → static).
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY portal/ ./
COPY deploy/platform/catalog.json ./catalog.default.json
COPY api/openapi.yaml ./web/openapi.yaml
COPY --from=css /src/web/tailwind.css ./web/tailwind.css
RUN node_free_catalog() { printf '/* generated from deploy/platform/catalog.json */\nwindow.DEMO_CATALOG = '; cat catalog.default.json; printf ';\n'; }; \
    node_free_catalog > web/catalog.js \
 && go vet ./... && CGO_ENABLED=0 go test ./... \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /portal .

# Stage 3: minimal runtime.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /portal /portal
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/portal"]
