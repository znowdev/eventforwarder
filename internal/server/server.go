package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync/atomic"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

const (
	requestsTopic = "requests"
)

var connectedClients atomic.Int32

type Config struct {
	SecretToken string
}

func Start(cfg Config) error {
	e := echo.New()
	pubSub := gochannel.NewGoChannel(
		gochannel.Config{},
		watermill.NewStdLogger(false, false),
	)

	srv := &server{&websocket.Upgrader{}, pubSub}

	authMw := newAuthMiddleware(cfg.SecretToken)

	e.GET("/", srv.healthHandler)
	e.GET("/health", srv.healthHandler)
	e.GET("/_websocket", srv.handleSockets, authMw)
	e.RouteNotFound("/*", srv.forwardRequest)

	defaultPort := "4045"
	port, ok := os.LookupEnv("PORT")
	if !ok {
		port = defaultPort
	}

	slog.Info("starting server", slog.Any("port", port))

	err := e.Start(":" + port)
	if err != nil {
		return err
	}

	return nil

}

type server struct {
	*websocket.Upgrader
	pubSub *gochannel.GoChannel
}

func (s *server) healthHandler(c echo.Context) error {
	return c.JSON(200, echo.Map{"status": "ok"})
}

func (s *server) forwardRequest(c echo.Context) error {
	slog.Info("forwarding request", slog.Any("headers", c.Request().Header))

	buf := new(bytes.Buffer)
	err := c.Request().Write(buf)
	if err != nil {
		return err
	}

	msg := message.NewMessage(watermill.NewUUID(), buf.Bytes())

	slog.Debug("publishing message", slog.Any("message_id", msg.UUID))
	err = s.pubSub.Publish(requestsTopic, msg)
	if err != nil {
		return err
	}

	//messages, err := s.pubSub.Subscribe(c.Request.Context(), msg.UUID)
	//if err != nil {
	//	return err
	//	return
	//}
	//
	//ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	//defer cancel()
	//
	//select {
	//case <-ctx.Done():
	//	handleError(c, ctx.Err())
	//case msg := <-messages:
	//	slog.Debug("received response", slog.Any("message_id", msg.UUID))
	//	buf := bufio.NewReader(bytes.NewReader(msg.Payload))
	//	req, err := http.ReadR(buf)
	//	if err != nil {
	//		slog.Error("failed to read request", slog.Any("error", err))
	//	}
	//
	//
	//}
	//resp := <-messages
	//c.Data(http.StatusOK, "application/json", resp.Payload)
	return c.String(http.StatusOK, "ok")
}

func (ws *server) handleSockets(c echo.Context) error {
	connectedClients.Add(1)
	slog.Info(fmt.Sprintf("client connected (total connected clients: %d)", connectedClients.Load()), slog.Any("total_connected_clients", connectedClients.Load()), slog.Any("client", c.Request().RemoteAddr))

	defer func() {
		connectedClients.Add(-1)
		slog.Info(fmt.Sprintf("client disconnected (total connected clients: %d)", connectedClients.Load()), slog.Any("total_connected_clients", connectedClients.Load()), slog.Any("client", c.Request().RemoteAddr))

	}()

	conn, err := ws.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	messages, err := ws.pubSub.Subscribe(c.Request().Context(), requestsTopic)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		for {
			msgType, content, err := conn.ReadMessage()
			if msgType == websocket.CloseMessage {
				slog.Info("received close message")
				cancel()
				return
			}
			var closeError *websocket.CloseError
			if errors.As(err, &closeError) {
				slog.Debug("received close message", slog.Any("code", closeError.Code), slog.Any("text", closeError.Text))
				cancel()
				return
			}
			if err != nil {
				slog.Error(err.Error(), slog.Any("error", err))
				return
			}
			slog.Debug("received message", slog.Any("type", msgType), slog.Any("content", string(content)))
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case msg := <-messages:
			slog.Debug("sending message", slog.Any("message_id", msg.UUID))
			err := conn.WriteMessage(websocket.TextMessage, msg.Payload)
			if err != nil {
				return err
			}
			msg.Ack()
		}
	}
}
