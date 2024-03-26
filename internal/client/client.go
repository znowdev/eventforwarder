package client

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/znowdev/reqbouncer/internal/wire"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type HostPost struct {
	Host string
	Port string
}

func (h *HostPost) String() string {
	return fmt.Sprintf("%s:%s", h.Host, h.Port)
}

func (h *HostPost) HttpScheme() string {
	if h.Port == "443" {
		return "https"
	}
	return "http"
}

type Client struct {
	conn        *websocket.Conn
	connMutex   sync.Mutex
	path        string
	target      HostPost
	server      HostPost
	secretToken string
	clientId    string
}

type Config struct {
	ClientId    string
	Target      string
	Server      string
	Path        string
	SecretToken string
}

const (
	maxRetries  = 3
	retryPeriod = 2 * time.Second
)

func splitHostPort(hostPort string) (HostPost, error) {
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		return HostPost{}, err
	}

	//if port == "" {
	//	return HostPost{}, fmt.Errorf("missing port in url: %s", hostPort)
	//}

	return HostPost{
		Host: host,
		Port: port,
	}, nil
}

func NewClient(cfg Config) (*Client, error) {
	if cfg.Target == "" {
		return nil, fmt.Errorf("missing target to tunnel to")
	}
	if cfg.Server == "" {
		return nil, fmt.Errorf("missing server to connect to")
	}
	target, err := splitHostPort(cfg.Target)
	if err != nil {
		return nil, err
	}

	server, err := splitHostPort(cfg.Server)
	if err != nil {
		return nil, err
	}

	return &Client{
		clientId:    cfg.ClientId,
		path:        cfg.Path,
		target:      target,
		server:      server,
		secretToken: cfg.SecretToken,
	}, nil

}

func (c *Client) connect(ctx context.Context) error {
	c.connMutex.Lock()
	defer c.connMutex.Unlock()

	var scheme = "ws"
	if c.server.Port == "443" {
		scheme = "wss"
	}

	if c.server.Host == "localhost" {
		c.server.Host += ":" + c.server.Port
	}

	u := url.URL{Scheme: scheme, Host: c.server.Host, Path: c.path}

	var conn *websocket.Conn
	var resp *http.Response
	var err error
	for i := 0; i < maxRetries; i++ {
		slog.Debug(fmt.Sprintf("dialing %s", u.String()))
		conn, resp, err = websocket.DefaultDialer.Dial(u.String(), map[string][]string{
			"Authorization":        {"Bearer " + c.secretToken},
			"reqbouncer-client-id": {c.clientId},
		})

		if err != nil {
			slog.Debug(fmt.Sprintf("failed to dial, retrying in %s", retryPeriod), slog.Any("error", err))
			time.Sleep(retryPeriod)
			continue
		}

		if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
			slog.Info("received close message")
			return nil
		}
		if resp.StatusCode == http.StatusPreconditionFailed {
			return fmt.Errorf("precondition failed: %d", resp.StatusCode)
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("unauthorized: %d", resp.StatusCode)
		}
		if resp.StatusCode == http.StatusForbidden {
			return fmt.Errorf("forbidden: %d", resp.StatusCode)
		}

		if err == nil {
			//conn.SetCloseHandler(func(code int, text string) error {
			//	slog.Info(fmt.Sprintf("received close message: %d %s", code, text))
			//	return errors.New(text)
			//})
			c.conn = conn
			break
		}
	}

	return err
}

func (c *Client) Listen(ctx context.Context) error {
	target := c.target
	server := c.server

	// Prepare the WebSocket scheme and host

	// Prepare the URL

	slog.Info(fmt.Sprintf("connecting to %s", server.Host))
	if c.clientId != "" {
		slog.Info(fmt.Sprintf("using client_id %s", c.clientId))
	}
	// Connect to the server
	err := c.connect(ctx)
	if err != nil {
		return fmt.Errorf("failed to dial after %d attempts: %s", maxRetries, err)
	}
	defer c.conn.Close()

	// Handle graceful shutdown
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, os.Kill)
	go handleShutdown(ch, c.conn)

	slog.Info(fmt.Sprintf("forwarding all requests to %s", target.String()))

	// Main loop: read messages and forward requests
	slog.Info("successfully connected, waiting for messages...")
	for {
		msgType, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				if strings.Contains(err.Error(), "client already connected") {
					return err
				}
			}
			slog.Info(fmt.Sprintf("read: %s", err))
			slog.Info("reconnecting")
			err = c.connect(ctx)
			if err != nil {
				return fmt.Errorf("failed to reconnect after %d attempts: %w", maxRetries, err)
			}
			return nil
		}
		if msgType == websocket.CloseMessage {
			slog.Info("received close message", slog.Any("msg", string(message)))
			return errors.New(string(message))
		}

		if msgType == websocket.BinaryMessage {
			if err := c.readAndForwardMessage(ctx, message); err != nil {
				slog.Error("failed to read and forward message", slog.Any("error", err))
			}
		}
	}
}

func handleShutdown(c chan os.Signal, conn *websocket.Conn) {
	<-c
	defer conn.Close()
	slog.Info("received interrupt signal")
	err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	if err != nil {
		slog.Error("failed to write close message", slog.Any("error", err))
	}
	os.Exit(0)
}

func (c *Client) readAndForwardMessage(ctx context.Context, socketPayload []byte) error {

	var wireMessage wire.WireMessage
	if err := wireMessage.Deserialize(socketPayload); err != nil {
		slog.Error("failed to deserialize message", slog.Any("error", err))
		return err
	}

	buf := bufio.NewReader(bytes.NewReader(wireMessage.Payload))
	req, err := http.ReadRequest(buf)
	if err != nil {
		slog.Error("failed to read request", slog.Any("error", err))
		return err
	}

	req.RequestURI = ""
	req.URL.Scheme = c.target.HttpScheme()
	req.URL.Host = c.target.String()

	slog.Info(fmt.Sprintf("forwarding request to %s: %s %s", c.target.String(), req.Method, req.URL.Path))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("failed to send request", slog.Any("error", err))
		return err
	}
	if resp.StatusCode != http.StatusOK {
		slog.Error("received bad response", slog.Any("response", resp.StatusCode), slog.String("destination", c.target.String()), slog.Any("request", req.URL.String()),
			slog.Any("response", resp.StatusCode), slog.Any("body", printBody(resp)))
		return fmt.Errorf("bad response: %d", resp.StatusCode)
	}

	respbytes, err := httputil.DumpResponse(resp, true)
	if err != nil {
		slog.Error("failed to dump response", slog.Any("error", err))
		return err
	}

	responseWireMessage := wire.WireMessage{
		ID:      wireMessage.ID,
		Payload: respbytes,
	}

	wirePayload, err := responseWireMessage.Serialize()
	if err != nil {
		slog.Error("failed to serialize response", slog.Any("error", err))
		return err
	}

	return c.conn.WriteMessage(websocket.BinaryMessage, wirePayload)
}

func printBody(resp *http.Response) string {
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	return buf.String()
}
