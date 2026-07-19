# kazi — Justfile
# Run `just` or `just --list` to see all recipes.

# List available recipes
default:
    just --list

# Build the kazi binary into the repo root
build:
    go build -o kazi ./cmd/kazi

# Run all unit tests
test:
    go test ./...

# Run go vet
vet:
    go vet ./...

# Format all Go source files
fmt:
    gofmt -l -w .

# Format, vet, and test (full pre-commit check)
check: fmt vet test

# Build a local release snapshot with GoReleaser (no publish; requires goreleaser)
release-snapshot:
    goreleaser release --snapshot --clean

# Print the version metadata baked into a locally built binary
version: build
    ./kazi --version

# Run integration tests (requires a running Docker daemon)
test-integration:
    go test -tags integration ./internal/engine/ -v

# Install kazi to $GOPATH/bin
install:
    go install ./cmd/kazi

# Run kazi directly (pass extra args after --, e.g. `just run -- ls`)
run *ARGS:
    go run ./cmd/kazi {{ARGS}}

# Build kazi, then launch the interactive TUI dashboard
tui *ARGS: build
    ./kazi tui {{ARGS}}

# Remove the built binary
clean:
    rm -f kazi

# --- Docs site (Astro Starlight, in site/, powered by bun) ---

# Install docs-site dependencies
docs-install:
    cd site && just install

# Run the docs dev server with hot reload
docs-dev:
    cd site && just dev

# Build the static docs site into site/dist
docs-build:
    cd site && just build

# Preview the built docs site locally
docs-preview:
    cd site && just preview
