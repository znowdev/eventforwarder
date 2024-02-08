package main

import (
	"bufio"
	"bytes"
	"eventforwarder/slogger"
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

const (
	maxRetries  = 3
	retryPeriod = 3 * time.Second
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

		time.Sleep(retryPeriod)
	}

	return err
}

func main() {
	flag.StringVar(&destination, "dest", destination, "destination to forward requests to")
	flag.StringVar(&host, "host", host, "host to connect to")
	flag.Parse()

	wsScheme := "ws"
	if strings.HasPrefix(host, "https") {
		wsScheme = "wss"
	}

	slogger.NewSlogger()
	u := url.URL{Scheme: wsScheme, Host: host, Path: path}

	err := connect(u)
	if err != nil {
		log.Fatalln("Failed to dial after", maxRetries, "attempts:", err)
		return
	}

	defer conn.Close()
	// graceful shutdown notify
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill)

	go func() {
		<-c
		defer conn.Close()
		slog.Info("received interrupt signal")
		err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		if err != nil {
			slog.Error("failed to write close message", slog.Any("error", err))
		}
		os.Exit(0)
	}()

	slog.Info("successfully connected, waiting for messages...")

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			slog.Info(fmt.Sprintf("read: %s", err))
			slog.Info("reconnecting")
			err = connect(u)
			if err != nil {
				log.Fatalln("Failed to reconnect after", maxRetries, "attempts:", err)
				return
			}
			continue
		}

		buf := bufio.NewReader(bytes.NewReader(message))
		req, err := http.ReadRequest(buf)
		if err != nil {
			slog.Error("failed to read request", slog.Any("error", err))
		}

		req.RequestURI = ""
		req.URL.Scheme = "http"
		req.URL.Host = "localhost:4045"
		slog.Info("forwarding request", slog.String("destination", destination), slog.Any("request", req.URL.String()))

		time.Sleep(100 * time.Millisecond)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			slog.Error("failed to send request", slog.Any("error", err))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			slog.Error("received bad response", slog.Any("response", resp.StatusCode), slog.String("destination", destination), slog.Any("request", req.URL.String()),
				slog.Any("response", resp.StatusCode), slog.Any("body", printBody(resp)))
		}
	}

}

func printBody(resp *http.Response) string {
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	return buf.String()
}
