package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/znowdev/reqbouncer/internal/wire"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

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
	Port        string
}

func Start(cfg Config) error {
	e := echo.New()
	pubSub := gochannel.NewGoChannel(
		gochannel.Config{},
		watermill.NewStdLogger(false, false),
	)

	srv := &server{&websocket.Upgrader{}, pubSub, &clientMap{clients: make(map[string]struct{})}}

	authMw := newAuthMiddleware(cfg.SecretToken)

	e.GET("/_health", srv.healthHandler)
	e.GET("/_websocket", srv.handleSockets, authMw)
	e.RouteNotFound("/*", srv.forwardRequest)

	err := e.Start(":" + cfg.Port)
	if err != nil {
		return err
	}

	return nil

}

type server struct {
	*websocket.Upgrader
	pubSub    *gochannel.GoChannel
	clientMap *clientMap
}

type clientMap struct {
	clients map[string]struct{}
	mux     sync.Mutex
}

func (cm *clientMap) AddClient(clientId string) {
	cm.mux.Lock()
	defer cm.mux.Unlock()
	cm.clients[clientId] = struct{}{}
}

func (cm *clientMap) HasClient(clientId string) bool {
	cm.mux.Lock()
	defer cm.mux.Unlock()
	_, ok := cm.clients[clientId]
	return ok

}

func (cm *clientMap) RemoveClient(clientId string) {
	cm.mux.Lock()
	defer cm.mux.Unlock()
	delete(cm.clients, clientId)
}

func (cm *clientMap) Clients() []string {
	cm.mux.Lock()
	defer cm.mux.Unlock()
	clients := make([]string, 0, len(cm.clients))
	for client := range cm.clients {
		clients = append(clients, client)
	}
	return clients
}

func (cm *clientMap) ConnectedClientsNo() int {
	cm.mux.Lock()
	defer cm.mux.Unlock()
	return len(cm.clients)
}

func (s *server) healthHandler(c echo.Context) error {
	return c.JSON(200, echo.Map{"status": "ok"})
}

func (s *server) forwardRequest(c echo.Context) error {
	slog.Info("forwarding request", slog.Any("headers", c.Request().Header))

	requestId := uuid.NewString()

	buf := new(bytes.Buffer)
	err := c.Request().Write(buf)
	if err != nil {
		return err
	}

	msg := message.NewMessage(requestId, buf.Bytes())

	topic := requestsTopic
	clientId := c.Request().Header.Get("reqbouncer-client-id")
	if clientId != "" {
		if !s.clientMap.HasClient(clientId) {
			return c.String(http.StatusBadRequest, "no connected clients for this client id: "+clientId)
		}
		topic = clientId
	}

	slog.Debug("publishing message", slog.Any("message_id", msg.UUID))
	err = s.pubSub.Publish(topic, msg)
	if err != nil {
		return err
	}

	msgs, err := s.pubSub.Subscribe(c.Request().Context(), requestId)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 60*time.Second)
	defer cancel()

	for {
		select {
		case <-c.Request().Context().Done():
			return c.Request().Context().Err()
		case msg := <-msgs:
			slog.Debug("received response", slog.Any("message_id", msg.UUID))

			hj, ok := c.Response().Writer.(http.Hijacker)
			if !ok {
				return errors.New("webserver does not support hijacking")
			}
			conn, bufrw, err := hj.Hijack()
			if err != nil {
				return err
			}
			defer conn.Close()
			fmt.Fprintf(bufrw, "%s", msg.Payload)
			bufrw.Flush()
			conn.Close()
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

}

func (ws *server) handleSockets(c echo.Context) error {
	var anonClient bool
	clientId := c.Request().Header.Get("reqbouncer-client-id")
	if clientId == "" {
		anonClient = true
		clientId = uuid.NewString()
	}

	if ws.clientMap.HasClient(clientId) {
		slog.Debug("client already connected", slog.Any("client", clientId))
		conn, err := ws.Upgrade(c.Response(), c.Request(), nil)
		if err != nil {
			return err
		}
		defer conn.Close()

		err = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "client already connected"), time.Now().Add(time.Second))
		if err != nil {
			slog.Error("failed to write close message", slog.Any("error", err))
			return err
		}
		return nil
	}

	ws.clientMap.AddClient(clientId)
	slog.Info(fmt.Sprintf("client connected (total connected clients: %d)", ws.clientMap.ConnectedClientsNo()), slog.Any("total_connected_clients", ws.clientMap.ConnectedClientsNo()), slog.Any("client", c.Request().RemoteAddr))

	defer func() {
		ws.clientMap.RemoveClient(clientId)
		slog.Info(fmt.Sprintf("client disconnected (total connected clients: %d)", ws.clientMap.ConnectedClientsNo()), slog.Any("total_connected_clients", ws.clientMap.ConnectedClientsNo()), slog.Any("client", c.Request().RemoteAddr))
	}()

	conn, err := ws.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	var clientMessages <-chan *message.Message
	if !anonClient {
		clientMessages, err = ws.pubSub.Subscribe(c.Request().Context(), clientId)
		if err != nil {
			return err
		}
	}

	globalMessages, err := ws.pubSub.Subscribe(c.Request().Context(), requestsTopic)
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
			if msgType == websocket.BinaryMessage {
				slog.Debug("received binary message", slog.Any("content", string(content)))
				var wireMsg wire.WireMessage
				if err := wireMsg.Deserialize(content); err != nil {
					slog.Error("failed to deserialize message", slog.Any("error", err))
					continue
				}
				msg := message.NewMessage(wireMsg.ID, wireMsg.Payload)
				slog.Debug("publishing message", slog.Any("message_id", msg.UUID))
				err := ws.pubSub.Publish(wireMsg.ID, msg)
				if err != nil {
					slog.Error("failed to publish message", slog.Any("error", err))
					continue
				}
				continue
			}

			slog.Debug("received message", slog.Any("type", msgType), slog.Any("content", string(content)))
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case msg := <-clientMessages:
			slog.Debug("sending client message", slog.Any("message_id", msg.UUID))

			wireMsg := wire.WireMessage{
				ID:      msg.UUID,
				Payload: msg.Payload,
			}

			bytes, err := wireMsg.Serialize()
			if err != nil {
				return err
			}

			err = conn.WriteMessage(websocket.BinaryMessage, bytes)
			if err != nil {
				return err
			}
			msg.Ack()
		case msg := <-globalMessages:
			slog.Debug("sending global message", slog.Any("message_id", msg.UUID))

			wireMsg := wire.WireMessage{
				ID:      msg.UUID,
				Payload: msg.Payload,
			}

			bytes, err := wireMsg.Serialize()
			if err != nil {
				return err
			}

			err = conn.WriteMessage(websocket.BinaryMessage, bytes)
			if err != nil {
				return err
			}
			msg.Ack()
		}
	}
}
