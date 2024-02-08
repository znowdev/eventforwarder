# Stage 1: Build the Go application
FROM golang:1.21 AS builder

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download all dependencies. Dependencies will be cached if the go.mod and go.sum files are not changed
RUN go mod download

# Copy the source from the current directory to the Working Directory inside the container
COPY . .

# Build the Go app
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o reqbouncer ./cmd/reqbouncer/main.go

# Stage 2: Start from a smaller image and copy the Go binary into it
FROM alpine:latest  

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/reqbouncer .

# Expose port 8080 to the outside world
EXPOSE 8080

# Run the binary
CMD ["./listener"]