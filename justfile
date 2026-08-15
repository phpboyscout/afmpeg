set dotenv-load

# Run go mod tidy
tidy:
    go mod tidy

# Apply go fix to update deprecated API usage
fix:
    go fix ./...

# Run go generate (mocks, embeds, etc.)
generate:
    go generate ./...

# Build the library (and any future cmd/) — proves CGO_ENABLED=0 compiles (R-AF-1)
[default]
build: tidy generate
    CGO_ENABLED=0 go build ./...

# Cross-compile the library for the launch targets (R-AF-1)
build-cross:
    CGO_ENABLED=0 GOOS=linux  GOARCH=amd64 go build ./...
    CGO_ENABLED=0 GOOS=linux  GOARCH=arm64 go build ./...
    CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build ./...
    CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ./...

# Run golangci-lint
lint:
    golangci-lint run

# Run golangci-lint with auto-fix
lint-fix:
    golangci-lint run --fix

# Run unit tests with coverage
test:
    go test ./... -cover

# Run unit tests with the race detector
test-race:
    go test -race ./...

# Run the integration suite against built ffmpeg-wasi modules (env-gated).
# Pass BOTH profiles: spec 0022 profiles are cumulative but not interchangeable, and
# the intermediate-profile tests need mpegts/libopus/yadif/libass, which a lean build
# does not carry. Omit either and the tests needing it skip, naming what to set.
#   just test-integration ~/m/ffmpeg-wasi-lgpl.wasm ~/m/ffmpeg-wasi-intermediate-lgpl.wasm
#
# The pkg/afmpeg/native tests want *driver* binaries instead, named by profile and
# licence variant (AFMPEG_TEST_NATIVE_DRIVER[_INTERMEDIATE|_FULL][_GPL]). Put those
# in a .env — dotenv-load is on — since one gpl intermediate driver satisfies them all.
test-integration lean="" intermediate="":
    AFMPEG_TEST_FFMPEG_WASI="{{lean}}" \
    AFMPEG_TEST_FFMPEG_WASI_INTERMEDIATE="{{intermediate}}" \
    go test ./pkg/afmpeg/... -run Integration -v

# Generate an HTML coverage report and open it
coverage:
    go test ./... -coverprofile=coverage.out
    go tool cover -html=coverage.out -o coverage.html
    open coverage.html

# Run benchmarks
bench:
    go test -bench=. -benchmem ./...

# Regenerate all mocks
mocks:
    mockery

# Check for vulnerabilities in dependencies
vuln:
    govulncheck ./...

# Run Trivy filesystem scan
trivy:
    trivy fs --severity HIGH,CRITICAL --skip-dirs .claude .

# Run gitleaks secret scan
gitleaks:
    gitleaks detect --source . -v

# Run OSV dependency scanner
osv-scan:
    osv-scanner scan source -L go.mod

# Run all security scans
security: vuln trivy gitleaks osv-scan
    @echo "All security scans passed"

# Report public-API changes vs the latest release tag (advisory, pre-1.0)
apidiff *args:
    ./scripts/apidiff.sh {{args}}

# Advisory per-package ≥90% coverage check (flags sub-90 packages not excluded)
coverage-policy *args:
    ./scripts/coverage-policy.sh {{args}}

# Find unreachable exported symbols
deadcode:
    deadcode ./...

# Run pre-commit checks and documentation linting
check:
    pre-commit run --all-files
    ./scripts/lint-docs-errors.sh

# Serve the documentation locally (zensical; pass ARGS, e.g. `just docs-serve "-a 0.0.0.0:8000"`)
docs-serve ARGS="":
    zensical serve {{ARGS}}

# Run the full local CI suite (mirrors the MR gate)
ci: tidy generate test test-race lint
    @echo "CI suite passed"

# Cleanup build artifacts
[confirm]
cleanup:
    rm -rf bin site dist .cache
    rm -f coverage.out coverage.html
