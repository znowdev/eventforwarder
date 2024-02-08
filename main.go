package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/znowdev/reqbouncer/internal/slogger"

	"github.com/urfave/cli/v2"
	"github.com/znowdev/reqbouncer/internal/client"
	"github.com/znowdev/reqbouncer/internal/server"
)

const (
	maxRetries        = 3
	retryPeriod       = 2 * time.Second
	secretTokenEnvKey = "REQBOUNCER_SECRET_TOKEN"
)

var Version string

func main() {
	// Parse command-line flags
	app := &cli.App{
		Name:  "reqbouncer",
		Usage: "hijack and bounce requests to a different server",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "debug",
				Aliases: []string{"d"},
				Value:   false,
				Usage:   "enable debug mode",
			},
		},
		Before: func(c *cli.Context) error {
			slogger.NewSlogger(c.Bool("debug"))
			return nil
		},
		Commands: []*cli.Command{
			{
				Name:  "version",
				Usage: "prints the version",
				Action: func(cCtx *cli.Context) error {
					fmt.Println(version())
					return nil
				},
			},
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
				Name:  "forward",
				Usage: "starts a reqbouncer forwarding client",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "server",
						Aliases: []string{"s"},
						Usage:   "reqbouncer server to connect to",
					},
					&cli.StringFlag{
						Name:  "secret-token",
						Usage: "specify the secret token to connect to the reqbouncer server",
					},
				},
				Action: func(cCtx *cli.Context) error {
					if cCtx.NArg() == 0 {
						return errors.New("you must specify a single argument: a port or address to tunnel to")
					}

					arg := cCtx.Args().Get(0)
					target := arg

					// Check if arg is an integer
					if _, err := strconv.Atoi(arg); err == nil {
						// If arg is an integer, forward to localhost:port
						target = "localhost:" + arg
					}
					c, err := client.NewClient(client.Config{

						Target:      target,
						Server:      parseServer(cCtx),
						Path:        "/_websocket",
						SecretToken: parseToken(cCtx),
					})
					if err != nil {
						return err
					}
					return c.Listen(cCtx.Context)
				},
			},
			{
				Name:  "configure",
				Usage: "configures the local reqbouncer client",
				Action: func(cCtx *cli.Context) error {
					reader := bufio.NewReader(os.Stdin)

					fmt.Print("Enter Server Host: ")
					serverHost, err := reader.ReadString('\n')
					if err != nil {
						return err
					}

					fmt.Print("Enter Secret Token: ")
					secretToken, err := reader.ReadString('\n')
					if err != nil {
						return err
					}

					// Trim newline characters
					serverHost = strings.TrimSpace(serverHost)
					secretToken = strings.TrimSpace(secretToken)

					// Get user home directory
					homeDir, err := os.UserHomeDir()
					if err != nil {
						return err
					}

					// Create .reqbouncer directory if it doesn't exist
					reqBouncerDir := filepath.Join(homeDir, ".reqbouncer")
					if _, err := os.Stat(reqBouncerDir); os.IsNotExist(err) {
						err = os.Mkdir(reqBouncerDir, 0755)
						if err != nil {
							return err
						}
					}

					// Create config file
					configFile := filepath.Join(reqBouncerDir, "config")
					file, err := os.Create(configFile)
					if err != nil {
						return err
					}
					defer file.Close()

					// Write server host and secret token to config file
					_, err = file.WriteString(fmt.Sprintf("server_host=%s\nsecret_token=%s\n", serverHost, secretToken))
					if err != nil {
						return err
					}

					fmt.Println("Configuration saved successfully.")
					return nil
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}

}

func parseConfigKey(key string) (string, error) {
	// Get user home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	// Open config file
	configFile := filepath.Join(homeDir, ".reqbouncer", "config")
	file, err := os.Open(configFile)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// Read config file
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, key+"=") {
			slog.Debug(fmt.Sprintf("found key `%s` in config file", key))
			return strings.TrimPrefix(line, key+"="), nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return "", nil
}

func parseToken(cCtx *cli.Context) string {
	token := cCtx.String("secret-token")
	if token != "" {
		return token
	}

	if val, ok := os.LookupEnv(secretTokenEnvKey); ok {
		return val
	}

	token, err := parseConfigKey("secret_token")
	if err != nil {
		log.Fatal(err)
	}

	return token
}

func parseServer(cCtx *cli.Context) string {
	server := cCtx.String("server")
	if server != "" {
		return server
	}

	server, err := parseConfigKey("server_host")
	if err != nil {
		log.Fatal(err)
	}

	return server
}

func version() string {
	if Version == "" {
		i, ok := debug.ReadBuildInfo()
		if !ok {
			return ""
		}
		Version = i.Main.Version
	}
	return Version
}
