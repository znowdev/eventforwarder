build:
    @echo "Building..."
    @go build -v .

install:
    @echo "Installing..."
    @go install -v .

installgh:
    @echo "Installing from gh..."
    go install github.com/znowdev/reqbouncer@latest