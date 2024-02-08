package main

import (
	"github.com/urfave/cli/v2"
	"log"
	"os"
	"reqbouncer/internal/client"
	"reqbouncer/internal/server"
	"time"
)

const (
	maxRetries  = 3
	retryPeriod = 2 * time.Second
)

func main() {
	// Parse command-line flags
	app := &cli.App{
		Name:  "reqbouncer",
		Usage: "hijack and bounce requests to a different server",
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
		},
		Commands: []*cli.Command{
			{
				Name:  "serve",
				Usage: "starts a reqbouncer server",
				Action: func(cCtx *cli.Context) error {
					return server.Start()
				},
			},
			{
				Name:  "listen",
				Usage: "starts a reqbouncer client",
				Action: func(cCtx *cli.Context) error {
					return client.Start(client.Config{
						Destination: cCtx.String("dest"),
						Host:        cCtx.String("host"),
						Path:        "/websocket",
					})
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}

}
