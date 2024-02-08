package util

import "strings"

type HostConfig struct {
	Host   string
	Port   string
	Scheme string
}

func ParseHost(host string) (HostConfig, error) {
	var scheme, port string
	if strings.HasPrefix(host, "http://") {
		scheme = "http"
		port = "800"
	}
	if strings.HasPrefix(host, "https://") {
		scheme = "https"
		port = "443"
	}

	if strings.Contains(host, "localhost") {
		scheme = "http"
		port = "8080"
	}

	if scheme == "" {
		scheme = "https"
		port = "443"
	}

	host = strings.TrimPrefix(host, scheme+"://")
	host = strings.TrimSuffix(host, "/")
	host = strings.TrimSuffix(host, ":")

	// check if port is specified
	if strings.Contains(host, ":") {
		parts := strings.Split(host, ":")
		host = parts[0]
		port = parts[1]
	}

	return HostConfig{
		Host:   host,
		Port:   port,
		Scheme: scheme,
	}, nil
}
