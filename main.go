package main

import (
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/urfave/cli/v2"
	"github.com/znowdev/reqbouncer/internal/client"
	"github.com/znowdev/reqbouncer/internal/server"
)

const (
	maxRetries        = 3
	retryPeriod       = 2 * time.Second
	secretTokenEnvKey = "REQBOUNCER_SECRET_TOKEN"
)

func main() {
	// Parse command-line flags
	app := &cli.App{
		Name:  "reqbouncer",
		Usage: "hijack and bounce requests to a different server",
		Flags: []cli.Flag{},
		Commands: []*cli.Command{
			{
				Name:  "serve",
				Usage: "starts a reqbouncer server",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "secret-token",
						Usage: "sets the secret token that protects the /_websocket endpoint",
					},
				},
				Action: func(cCtx *cli.Context) error {
					token := parseToken(cCtx)
					if token == "" {
						slog.Warn("secret token not provided, the server will be unprotected")
					}
					return server.Start(server.Config{
						SecretToken: token,
					})
				},
			},
			{
				Name:  "listen",
				Usage: "starts a reqbouncer client",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "host",
						Value: "localhost:4045",
						Usage: "reqbouncer host",
					},
					&cli.StringFlag{
						Name:  "dest",
						Value: "localhost:4000",
						Usage: "destination to forward requests to",
					},
					&cli.StringFlag{
						Name:  "secret-token",
						Usage: "specify the secret token to connect to the reqbouncer server",
					},
				},
				Action: func(cCtx *cli.Context) error {
					return client.Start(client.Config{
						Destination: cCtx.String("dest"),
						Host:        cCtx.String("host"),
						Path:        "/_websocket",
						SecretToken: parseToken(cCtx),
					})
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}

}

func parseToken(cCtx *cli.Context) string {
	token := cCtx.String("secret-token")
	if val, ok := os.LookupEnv(secretTokenEnvKey); ok {
		token = val
	}
	return token
}
