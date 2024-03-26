j: test build install

test:
    @echo "Testing..."
    @go test ./...

build:
    @echo "Building..."
    @go build ./...

install:
    @echo "Installing..."
    @go install -v .

installgh:
    @echo "Installing from gh..."
    go install github.com/znowdev/reqbouncer@latest

