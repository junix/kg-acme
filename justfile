# kg-acme — capability hub for the KG toolchain

default: test

build:
    go build ./...
    go build -o kg ./cmd/kg

test:
    go test ./...

vet:
    go vet ./...

check: vet test build

clean:
    rm -f kg
