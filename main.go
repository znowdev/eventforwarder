package main

import (
	"bufio"
	"errors"
	"fmt"
	"github.com/mscno/zerrors"
	"github.com/znowdev/reqbouncer/internal/client/auth"
	"github.com/znowdev/reqbouncer/internal/config"
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
	defaultServer     = "reqbouncer.znow.dev:443"
)

var Version string

func main() {
	// Parse command-line flags
	var cfg *config.Config
	var err error
	var logger *slog.Logger
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
			logger, err = slogger.NewSlogger(c.Bool("debug"))
			return err
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
				Name:  "login",
				Usage: "logs in to the reqbouncer server",
				Action: func(cCtx *cli.Context) error {

					// Trim newline characters

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

					githubClientId, err := auth.GetGithubConfig(defaultServer)
					if err != nil {
						return err
					}

					token, err := auth.Login(cCtx.Context, githubClientId)
					if err != nil {
						return err
					}

					githubUser, err := auth.GetGitHubUser(token.AccessToken)
					if err != nil {
						return err
					}
					serverHost := fmt.Sprintf("%s.%s", githubUser.Login, defaultServer)

					// Create config file
					configFile := filepath.Join(reqBouncerDir, "config")
					file, err := os.Create(configFile)
					if err != nil {
						return err
					}
					defer file.Close()

					// Write server host and secret token to config file
					_, err = file.WriteString(fmt.Sprintf("server_host=%s\naccess_token=%s", serverHost, token.AccessToken))
					if err != nil {
						return err
					}

					fmt.Println("Login successful.")
					return nil
				},
			},
			{
				Name:    "server",
				Aliases: []string{"serve"},
				Usage:   "starts a reqbouncer server",
				Before: func(context *cli.Context) error {
					var err error
					cfg, err = config.Init()
					return zerrors.ToInternal(err, "failed to initialize config")
				},
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "port",
						Aliases: []string{"p"},
						Usage:   "sets the port to listen on",
					},
				},
				Action: func(cCtx *cli.Context) error {
					port := cCtx.String("port")
					if port == "" {
						port = "8080"
					}

					if val, ok := os.LookupEnv("PORT"); ok {
						port = val
					}

					return server.Start(logger, server.Config{
						GithubClientid:     cfg.GithubClientId,
						GithubUserProvider: auth.GetGitHubUser,
						Port:               port,
						Debug:              cCtx.Bool("debug"),
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
						AccessToken: parseToken(cCtx),
					})
					if err != nil {
						return err
					}
					return c.Listen(cCtx.Context)
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		slog.Error(err.Error(), "error", err)
		os.Exit(1)
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
		if os.IsNotExist(err) {
			return "", nil
		}
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
	token := cCtx.String("access-token")
	if token != "" {
		return token
	}

	if val, ok := os.LookupEnv(secretTokenEnvKey); ok {
		return val
	}

	token, err := parseConfigKey("access_token")
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

func parseClientId(cCtx *cli.Context) string {
	server := cCtx.String("client-id")
	if server != "" {
		return server
	}

	server, err := parseConfigKey("client_id")
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
