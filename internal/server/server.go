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

	srv := &server{&websocket.Upgrader{}, pubSub}

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
	pubSub *gochannel.GoChannel
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

	clientId := c.Request().Header.Get("reqbouncer-client-id")
	var clientMessages <-chan *message.Message
	if clientId != "" {
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

			err = conn.WriteMessage(websocket.TextMessage, bytes)
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

			err = conn.WriteMessage(websocket.TextMessage, bytes)
			if err != nil {
				return err
			}
			msg.Ack()
		}
	}
}
