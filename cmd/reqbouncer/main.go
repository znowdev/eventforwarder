package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"github.com/gorilla/websocket"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"
)

var destination = "localhost:4000"
var host = "localhost:4045"
var path = "/websocket"
var hostPort = "443"

const (
	maxRetries  = 3
	retryPeriod = 2 * time.Second
)

var conn *websocket.Conn
var connMutex sync.Mutex

func connect(u url.URL) error {
	connMutex.Lock()
	defer connMutex.Unlock()

	var err error
	for i := 0; i < maxRetries; i++ {
		conn, _, err = websocket.DefaultDialer.Dial(u.String(), map[string][]string{})
		if err == nil {
			break
		}

		slog.Debug(fmt.Sprintf("failed to dial, retrying in %s", retryPeriod), slog.Any("error", err))
		time.Sleep(retryPeriod)
	}

	return err
}

func main() {
	// Parse command-line flags
	flag.StringVar(&destination, "dest", destination, "destination to forward requests to")
	flag.StringVar(&host, "host", host, "host to connect to")
	flag.Parse()

	slog.Info(fmt.Sprintf("forwarding requests to %s", destination))
	// Prepare the WebSocket scheme and host
	wsScheme := "ws"
	if strings.HasPrefix(host, "https") {
		wsScheme = "wss"
	}
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimSuffix(host, "/")

	//// Initialize the logger
	//slogger.NewSlogger()

	// Prepare the URL
	u := url.URL{Scheme: wsScheme, Host: host, Path: path}

	// Connect to the server
	err := connect(u)
	if err != nil {
		log.Fatalln("Failed to dial after", maxRetries, "attempts:", err)
		return
	}
	defer conn.Close()

	// Handle graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill)
	go handleShutdown(c)

	// Main loop: read messages and forward requests
	slog.Info("successfully connected, waiting for messages...")
	for {
		if err := readAndForwardMessage(u); err != nil {
			slog.Error("failed to read and forward message", slog.Any("error", err))
		}
	}
}

func handleShutdown(c chan os.Signal) {
	<-c
	defer conn.Close()
	slog.Info("received interrupt signal")
	err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	if err != nil {
		slog.Error("failed to write close message", slog.Any("error", err))
	}
	os.Exit(0)
}

func readAndForwardMessage(u url.URL) error {
	_, message, err := conn.ReadMessage()
	if err != nil {
		slog.Info(fmt.Sprintf("read: %s", err))
		slog.Info("reconnecting")
		err = connect(u)
		if err != nil {
			log.Fatalln("Failed to reconnect after", maxRetries, "attempts:", err)
			return err
		}
		return nil
	}

	buf := bufio.NewReader(bytes.NewReader(message))
	req, err := http.ReadRequest(buf)
	if err != nil {
		slog.Error("failed to read request", slog.Any("error", err))
		return err
	}

	req.RequestURI = ""
	req.URL.Scheme = "http"
	req.URL.Host = destination

	slog.Info(fmt.Sprintf("forwarding request to %s: %s %s", destination, req.Method, req.URL.Path))

	//time.Sleep(100 * time.Millisecond)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("failed to send request", slog.Any("error", err))
		return err
	}
	if resp.StatusCode != http.StatusOK {
		slog.Error("received bad response", slog.Any("response", resp.StatusCode), slog.String("destination", destination), slog.Any("request", req.URL.String()),
			slog.Any("response", resp.StatusCode), slog.Any("body", printBody(resp)))
		return fmt.Errorf("bad response: %d", resp.StatusCode)
	}

	return nil
}

func printBody(resp *http.Response) string {
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	return buf.String()
}
