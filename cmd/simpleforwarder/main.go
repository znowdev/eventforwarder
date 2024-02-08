package main

import (
	"bufio"
	"bytes"
	"eventforwarder/slogger"
	"github.com/gin-gonic/gin"
	"io"
	"log/slog"
	"net/http"
)

func main() {
	slogger.NewSlogger()
	r := gin.Default()

	srv := &server{}

	r.GET("/health", srv.healthHandler)
	r.NoRoute(srv.forwardRequest)

	err := r.Run(":4045")
	if err != nil {
		slog.Error(err.Error(), slog.Any("error", err))
	}
}

type server struct {
}

func (s *server) healthHandler(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok"})
}

func (s *server) ok(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok"})
}

func (s *server) forwardRequest(c *gin.Context) {
	slog.Info("forwarding request", slog.Any("headers", c.Request.Header))

	buf := new(bytes.Buffer)
	err := c.Request.Write(buf)
	if err != nil {
		slog.Error(err.Error(), slog.String("reason", "could not write request"), slog.Any("error", err))
		handleError(c, err)
		return
	}
	bufr := bufio.NewReader(buf)
	req, err := http.ReadRequest(bufr)
	if err != nil {
		slog.Error(err.Error(), slog.String("reason", "could not read request from bufio"), slog.Any("error", err))

		handleError(c, err)
		return
	}
	req.RequestURI = ""
	req.URL.Scheme = "http"
	req.URL.Host = "localhost:4000"
	req.Method = c.Request.Method
	slog.Info("received request", slog.String("method", req.Method), slog.Any("request", req.URL.String()))

	//time.Sleep(1 * time.Second)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error(err.Error(), slog.String("reason", "could not send request to local forwarder"), slog.Any("error", err))

		handleError(c, err)
		return
	}

	slog.Info("received response", slog.Any("response", resp.StatusCode))

	io.Copy(c.Writer, resp.Body)

	c.Status(resp.StatusCode)
}

func handleError(c *gin.Context, err error) {
	slog.Error(err.Error(), slog.Any("error", err))
	c.String(http.StatusInternalServerError, err.Error())
	c.AbortWithError(http.StatusInternalServerError, err)
}
