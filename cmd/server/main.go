package main

import (
	"bytes"
	"context"
	"errors"
	"eventforwarder/slogger"
	"fmt"
	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"log/slog"
	"net/http"
	"os"
	"sync/atomic"
)

const (
	requestsTopic = "requests"
)

var connectedClients atomic.Int32

func main() {
	slogger.NewSlogger()
	r := gin.Default()

	pubSub := gochannel.NewGoChannel(
		gochannel.Config{},
		watermill.NewStdLogger(false, false),
	)

	srv := &server{&websocket.Upgrader{}, pubSub}

	r.GET("/", srv.healthHandler)
	r.GET("/health", srv.healthHandler)
	r.GET("/websocket", srv.handleSockets)
	r.NoRoute(srv.forwardRequest)

	defaultPort := "4045"
	port, ok := os.LookupEnv("PORT")
	if !ok {
		port = defaultPort
	}

	slog.Info("starting server", slog.Any("port", port))

	err := r.Run(":" + port)
	if err != nil {
		slog.Error(err.Error(), slog.Any("error", err))
	}
}

type server struct {
	*websocket.Upgrader
	pubSub *gochannel.GoChannel
}

func (s *server) healthHandler(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok"})
}

func (s *server) forwardRequest(c *gin.Context) {
	slog.Info("forwarding request", slog.Any("headers", c.Request.Header))

	buf := new(bytes.Buffer)
	err := c.Request.Write(buf)
	if err != nil {
		handleError(c, err)
		return
	}

	msg := message.NewMessage(watermill.NewUUID(), buf.Bytes())

	slog.Debug("publishing message", slog.Any("message_id", msg.UUID))
	err = s.pubSub.Publish(requestsTopic, msg)
	if err != nil {
		handleError(c, err)
		return
	}

	//messages, err := s.pubSub.Subscribe(c.Request.Context(), msg.UUID)
	//if err != nil {
	//	handleError(c, err)
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
	c.Status(http.StatusOK)
}

func (ws *server) handleSockets(c *gin.Context) {
	connectedClients.Add(1)
	slog.Info(fmt.Sprintf("client connected (total connected clients: %d)", connectedClients.Load()), slog.Any("total_connected_clients", connectedClients.Load()), slog.Any("client", c.Request.RemoteAddr))

	defer func() {
		connectedClients.Add(-1)
		slog.Info(fmt.Sprintf("client disconnected (total connected clients: %d)", connectedClients.Load()), slog.Any("total_connected_clients", connectedClients.Load()), slog.Any("client", c.Request.RemoteAddr))

	}()

	conn, err := ws.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		handleError(c, err)
		return
	}
	defer conn.Close()

	messages, err := ws.pubSub.Subscribe(c.Request.Context(), requestsTopic)
	if err != nil {
		handleError(c, err)
		return
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
			return
		case msg := <-messages:
			slog.Debug("sending message", slog.Any("message_id", msg.UUID))
			err := conn.WriteMessage(websocket.TextMessage, msg.Payload)
			if err != nil {
				handleError(c, err)
				return
			}
			msg.Ack()
		}
	}

	slog.Info("exiting")
}

func handleError(c *gin.Context, err error) {
	slog.Error(err.Error(), slog.Any("error", err))
	c.String(http.StatusInternalServerError, err.Error())
	c.AbortWithError(http.StatusInternalServerError, err)
}
