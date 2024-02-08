package util

import (
	"reflect"
	"testing"
)

func TestParseHost(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		want    HostConfig
		wantErr bool
	}{
		{
			name: "http host",
			host: "http://example.com",
			want: HostConfig{
				Host:   "example.com",
				Port:   "800",
				Scheme: "http",
			},
			wantErr: false,
		},
		{
			name: "https host",
			host: "https://example.com",
			want: HostConfig{
				Host:   "example.com",
				Port:   "443",
				Scheme: "https",
			},
			wantErr: false,
		},
		{
			name: "localhost",
			host: "localhost",
			want: HostConfig{
				Host:   "localhost",
				Port:   "8080",
				Scheme: "http",
			},
			wantErr: false,
		},
		{
			name: "no scheme",
			host: "example.com",
			want: HostConfig{
				Host:   "example.com",
				Port:   "443",
				Scheme: "https",
			},
			wantErr: false,
		},
		{
			name: "host with port",
			host: "example.com:8080",
			want: HostConfig{
				Host:   "example.com",
				Port:   "8080",
				Scheme: "https",
			},
			wantErr: false,
		},
		{
			name: "host with port and scheme",
			host: "http://example.com:8080",
			want: HostConfig{
				Host:   "example.com",
				Port:   "8080",
				Scheme: "http",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseHost(tt.host)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseHost() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseHost() = %v, want %v", got, tt.want)
			}
		})
	}
}
