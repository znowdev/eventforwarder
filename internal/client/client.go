package client

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/lxzan/gws"
	"github.com/mscno/zerrors"
	"github.com/znowdev/reqbouncer/internal/wire"
	"io"
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
	conn           *gws.Conn
	connMutex      sync.Mutex
	path           string
	target         HostPost
	server         HostPost
	accessToken    string
	clientId       string
	closeErr       chan error
	reconnectDelay time.Duration
	maxConnAge     time.Duration
}

type Config struct {
	Target      string
	Server      string
	Path        string
	AccessToken string
}

const (
	maxRetries = 5
)

var retryPeriod = 1 * time.Second

func splitHostPort(hostPort string) (HostPost, error) {
	hostPort = strings.TrimPrefix(hostPort, "http://")
	hostPort = strings.TrimPrefix(hostPort, "https://")
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

	if cfg.AccessToken == "" {
		return nil, fmt.Errorf("missing access token")
	}

	slog.Debug(fmt.Sprintf("connecting to %s:%s", server.Host, server.Port))

	return &Client{
		path:        cfg.Path,
		target:      target,
		server:      server,
		accessToken: cfg.AccessToken,
		closeErr:    make(chan error),
	}, nil

}

func (c *Client) connect(ctx context.Context) error {
	c.connMutex.Lock()
	defer c.connMutex.Unlock()

	var scheme = "ws"
	if c.server.Port == "443" {
		scheme = "wss"
	}

	if c.server.Host == "localhost" || strings.HasSuffix(c.server.Host, ".internal") {
		c.server.Host += ":" + c.server.Port
	}

	u := url.URL{Scheme: scheme, Host: c.server.Host, Path: c.path}

	var conn *gws.Conn
	var err error
	var resp *http.Response
	for i := 0; i < maxRetries; i++ {
		slog.Debug(fmt.Sprintf("dialing %s", u.String()))
		conn, resp, err = gws.NewClient(c, &gws.ClientOption{
			Addr: u.String(),
			RequestHeader: map[string][]string{
				"Authorization":        {"Bearer " + c.accessToken},
				"reqbouncer-client-id": {c.clientId},
			},
			PermessageDeflate: gws.PermessageDeflate{
				Enabled:               true,
				ServerContextTakeover: true,
				ClientContextTakeover: true,
			},
		})

		if resp != nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusNotFound {
				return fmt.Errorf("server not found: " + c.server.Host)
			}
			if resp.StatusCode == http.StatusConflict {
				return fmt.Errorf("client already connected for host: " + c.server.Host)
			}
			if resp.StatusCode == http.StatusUnauthorized {
				body, _ := io.ReadAll(resp.Body)
				return zerrors.Unauthenticated(fmt.Sprintf("unauthorized: %s", body))
			}
			if resp.StatusCode == http.StatusForbidden {
				return zerrors.PermissionDenied("permission denied")
			}
			if resp.StatusCode != http.StatusSwitchingProtocols {
				slog.Debug(fmt.Sprintf("unexpected response: %s", resp.Status))
				return fmt.Errorf("unexpected response: %s", resp.Status)
			}
		}

		if err != nil {
			slog.Debug(fmt.Sprintf("failed to dial, retrying in %s", retryPeriod), slog.Any("error", err))
			time.Sleep(retryPeriod)
			retryPeriod = retryPeriod * 2
			continue
		}

		if err == nil {
			slog.Info(fmt.Sprintf("successfully connected to %s", c.server.Host))
			c.conn = conn
			c.conn.SetDeadline(time.Now().Add(30 * time.Second))
			break
		}
	}

	return err
}

type WebSocket struct {
}

func (c *Client) OnClose(socket *gws.Conn, err error) {
	// Start a goroutine to reconnect after ReconnectDelay
	slog.Debug("connection closed", slog.Any("error", err))

	if strings.Contains(err.Error(), "client already connected") {
		fmt.Printf("onerror: err=%s\n", err.Error())
		c.conn.NetConn().Close()
	}
	//go func() {
	//	defer close(c.closeErr)
	//	c.closeErr <- err
	//}()
}

const (
	PingInterval = 5 * time.Second
	PingWait     = 10 * time.Second
)

func (c *Client) OnPong(socket *gws.Conn, payload []byte) {
	//slog.Debug("received pong")
	_ = socket.SetDeadline(time.Now().Add(PingInterval + PingWait))
}

func (c *Client) OnOpen(socket *gws.Conn) {
	go func() {
		tick := time.Tick(PingInterval)
		for {
			select {
			case <-tick:
				//slog.Debug("sending ping")
				if err := socket.WritePing(nil); err != nil {
					if errors.Is(err, gws.ErrConnClosed) {
						return
					}
					slog.Error("failed to write ping", slog.Any("error", err))
				}
			}

		}
	}()
}

func (c *Client) OnPing(socket *gws.Conn, payload []byte) {
	slog.Debug("received ping")
	_ = socket.SetDeadline(time.Now().Add(PingInterval + PingWait))
	_ = socket.WritePong(nil)
	_ = socket.WritePong(payload)
}

func (c *Client) OnMessage(socket *gws.Conn, wsmsg *gws.Message) {
	defer wsmsg.Close()
	if wsmsg.Opcode == gws.OpcodeBinary {
		if err := c.readAndForwardMessage(wsmsg.Bytes()); err != nil {
			slog.Error("failed to read and forward message", slog.Any("error", err))
		}
	}
}

func (c *Client) Listen(ctx context.Context) error {
	target := c.target
	server := c.server

	slog.Info(fmt.Sprintf("connecting to %s", server.Host))
	if c.clientId != "" {
		slog.Info(fmt.Sprintf("using client_id %s", c.clientId))
	}
	// Connect to the server
	err := c.connect(ctx)
	if err != nil {
		return fmt.Errorf("failed to dial: %w", err)
	}
	defer c.conn.NetConn().Close()

	// Handle graceful shutdown
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, os.Kill)
	go handleShutdown(ch, c.conn)

	slog.Info(fmt.Sprintf("forwarding all requests to %s", target.String()))

	// Main loop: read messages and forward requests
	for {
		c.conn.ReadLoop()
		slog.Info("connection lost, trying to reconnect...")
		err = c.connect(ctx)
		if err != nil {
			return zerrors.ToInternal(err, "failed to reconnect")
		}
		continue
		//slog.Debug("connection closed, waiting for close error")
		//err = <-c.closeErr
		//if err != nil {
		//	if strings.Contains(err.Error(), "client already connected") {
		//		break
		//	}
		//	c.closeErr = make(chan error)
		//	slog.Error("connection closed", slog.Any("error", err))
		//	slog.Info("reconnecting...")
		//	continue
		//}
		//break
	}

	return err
}

func handleShutdown(c chan os.Signal, conn *gws.Conn) {
	<-c
	defer conn.NetConn().Close()
	slog.Info("received interrupt signal")
	err := conn.WriteMessage(gws.OpcodeCloseConnection, []byte("received interrupt signal"))
	if err != nil {
		slog.Error("failed to write close message", slog.Any("error", err))
	}
	os.Exit(0)
}

func (c *Client) readAndForwardMessage(socketPayload []byte) error {

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
	if resp.StatusCode == http.StatusSwitchingProtocols {
		slog.Info("received switching protocols response")
		resp = internalErrorHttpResp(errors.New("switching protocols not supported"))
	}
	//if resp.StatusCode >= 400 {
	//	slog.Error("received bad response", slog.Any("response", resp.StatusCode), slog.String("destination", c.target.String()), slog.Any("request", req.URL.String()),
	//		slog.Any("response", resp.StatusCode), slog.Any("body", printBody(resp)))
	//	return fmt.Errorf("bad response: %d", resp.StatusCode)
	//}

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

	return c.conn.WriteMessage(gws.OpcodeBinary, wirePayload)
}

func internalErrorHttpResp(err error) *http.Response {
	return &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(err.Error())),
	}
}

func printBody(resp *http.Response) string {
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	return buf.String()
}
