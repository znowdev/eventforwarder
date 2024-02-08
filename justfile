build:
    @echo "Building..."
    @go build -v .

install:
    @echo "Installing..."
    @go install -v .

installgh:
    @echo "Installing..."
    @go install github.com/znowdev/reqbouncer@v0.0.1