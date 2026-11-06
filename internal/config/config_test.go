// SPDX-License-Identifier: MPL-2.0

package config

import (
	"reflect"
	"strings"
	"testing"
)

// testToken is a token of the minimum length.
var testToken = strings.Repeat("t", MinTokenLength)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		want    Config
		wantErr string
	}{
		{
			name: "defaults",
			env:  map[string]string{"WHATSAPP_MCP_TOKEN": testToken},
			want: Config{Addr: "127.0.0.1:8080", DatabaseURL: "sqlite://data/whatsapp.db", AuthToken: testToken, LogLevel: "info"},
		},
		{
			name: "flags beat env",
			args: []string{"-addr", ":9000", "-read-only=false"},
			env:  map[string]string{"WHATSAPP_MCP_TOKEN": testToken, "WHATSAPP_MCP_ADDR": ":1", "WHATSAPP_MCP_READ_ONLY": "true"},
			want: Config{Addr: ":9000", DatabaseURL: "sqlite://data/whatsapp.db", AuthToken: testToken, LogLevel: "info"},
		},
		{
			name: "read-only from env",
			env:  map[string]string{"WHATSAPP_MCP_TOKEN": testToken, "WHATSAPP_MCP_READ_ONLY": "1"},
			want: Config{Addr: "127.0.0.1:8080", DatabaseURL: "sqlite://data/whatsapp.db", AuthToken: testToken, LogLevel: "info", ReadOnly: true},
		},
		{
			name: "expire disappearing from env",
			env:  map[string]string{"WHATSAPP_MCP_TOKEN": testToken, "WHATSAPP_MCP_EXPIRE_DISAPPEARING": "true"},
			want: Config{Addr: "127.0.0.1:8080", DatabaseURL: "sqlite://data/whatsapp.db", AuthToken: testToken, LogLevel: "info", ExpireDisappearing: true},
		},
		{
			name: "expire disappearing off by flag",
			args: []string{"-expire-disappearing=false"},
			env:  map[string]string{"WHATSAPP_MCP_TOKEN": testToken, "WHATSAPP_MCP_EXPIRE_DISAPPEARING": "true"},
			want: Config{Addr: "127.0.0.1:8080", DatabaseURL: "sqlite://data/whatsapp.db", AuthToken: testToken, LogLevel: "info"},
		},
		{
			name: "group admin and invite links from env",
			env:  map[string]string{"WHATSAPP_MCP_TOKEN": testToken, "WHATSAPP_MCP_ENABLE_GROUP_ADMIN": "true", "WHATSAPP_MCP_ALLOW_INVITE_LINKS": "1"},
			want: Config{Addr: "127.0.0.1:8080", DatabaseURL: "sqlite://data/whatsapp.db", AuthToken: testToken, LogLevel: "info", EnableGroupAdmin: true, AllowInviteLinks: true},
		},
		{
			name: "group admin by flag",
			args: []string{"-enable-group-admin"},
			env:  map[string]string{"WHATSAPP_MCP_TOKEN": testToken},
			want: Config{Addr: "127.0.0.1:8080", DatabaseURL: "sqlite://data/whatsapp.db", AuthToken: testToken, LogLevel: "info", EnableGroupAdmin: true},
		},
		{
			name: "allow status from env",
			env:  map[string]string{"WHATSAPP_MCP_TOKEN": testToken, "WHATSAPP_MCP_ALLOW_STATUS": "true"},
			want: Config{Addr: "127.0.0.1:8080", DatabaseURL: "sqlite://data/whatsapp.db", AuthToken: testToken, LogLevel: "info", AllowStatus: true},
		},
		{
			name: "allowlist is trimmed and drops empty entries",
			args: []string{"-allow-recipients", " +31 6 1234 5678 , ,123@g.us,"},
			env:  map[string]string{"WHATSAPP_MCP_TOKEN": testToken},
			want: Config{
				Addr: "127.0.0.1:8080", DatabaseURL: "sqlite://data/whatsapp.db", AuthToken: testToken, LogLevel: "info",
				AllowRecipients: []string{"+31 6 1234 5678", "123@g.us"},
			},
		},
		{
			name:    "missing token",
			wantErr: "WHATSAPP_MCP_TOKEN must be set",
		},
		{
			name:    "token one character too short",
			env:     map[string]string{"WHATSAPP_MCP_TOKEN": testToken[1:]},
			wantErr: "at least 32 characters",
		},
		{
			name:    "trivial token",
			env:     map[string]string{"WHATSAPP_MCP_TOKEN": "x"},
			wantErr: "openssl rand -hex 32",
		},
		{
			name:    "invalid read-only env",
			env:     map[string]string{"WHATSAPP_MCP_TOKEN": testToken, "WHATSAPP_MCP_READ_ONLY": "maybe"},
			wantErr: "WHATSAPP_MCP_READ_ONLY",
		},
		{
			name:    "invalid expire disappearing env",
			env:     map[string]string{"WHATSAPP_MCP_TOKEN": testToken, "WHATSAPP_MCP_EXPIRE_DISAPPEARING": "sometimes"},
			wantErr: "WHATSAPP_MCP_EXPIRE_DISAPPEARING",
		},
		{
			name:    "invalid group admin env",
			env:     map[string]string{"WHATSAPP_MCP_TOKEN": testToken, "WHATSAPP_MCP_ENABLE_GROUP_ADMIN": "perhaps"},
			wantErr: "WHATSAPP_MCP_ENABLE_GROUP_ADMIN",
		},
		{
			name:    "invalid invite links env",
			env:     map[string]string{"WHATSAPP_MCP_TOKEN": testToken, "WHATSAPP_MCP_ALLOW_INVITE_LINKS": "perhaps"},
			wantErr: "WHATSAPP_MCP_ALLOW_INVITE_LINKS",
		},
		{
			name:    "read-only with group admin",
			args:    []string{"-read-only", "-enable-group-admin"},
			env:     map[string]string{"WHATSAPP_MCP_TOKEN": testToken},
			wantErr: "-enable-group-admin has no effect with -read-only",
		},
		{
			name:    "invalid allow status env",
			env:     map[string]string{"WHATSAPP_MCP_TOKEN": testToken, "WHATSAPP_MCP_ALLOW_STATUS": "kinda"},
			wantErr: "WHATSAPP_MCP_ALLOW_STATUS",
		},
		{
			name:    "read-only with allowlist",
			args:    []string{"-read-only", "-allow-recipients", "+31612345678"},
			env:     map[string]string{"WHATSAPP_MCP_TOKEN": testToken},
			wantErr: "no effect",
		},
		{
			name:    "media dir must exist",
			args:    []string{"-media-dir", "/nonexistent/whatsapp-mcp-media"},
			env:     map[string]string{"WHATSAPP_MCP_TOKEN": testToken},
			wantErr: "-media-dir",
		},
		{
			name:    "media dir must be a directory",
			args:    []string{"-media-dir", "config_test.go"},
			env:     map[string]string{"WHATSAPP_MCP_TOKEN": testToken},
			wantErr: "not a directory",
		},
		{
			name: "media dir",
			args: []string{"-media-dir", "."},
			env:  map[string]string{"WHATSAPP_MCP_TOKEN": testToken},
			want: Config{Addr: "127.0.0.1:8080", DatabaseURL: "sqlite://data/whatsapp.db", AuthToken: testToken, LogLevel: "info", MediaDir: "."},
		},
		{
			name: "public url is normalised",
			args: []string{"-public-url", "HTTPS://WA.Example.com/"},
			env:  map[string]string{"WHATSAPP_MCP_TOKEN": testToken},
			want: Config{Addr: "127.0.0.1:8080", DatabaseURL: "sqlite://data/whatsapp.db", AuthToken: testToken, LogLevel: "info", PublicURL: "https://wa.example.com"},
		},
		{
			name: "public url from env, with a port",
			env:  map[string]string{"WHATSAPP_MCP_TOKEN": testToken, "WHATSAPP_MCP_PUBLIC_URL": "https://wa.example.com:8443"},
			want: Config{Addr: "127.0.0.1:8080", DatabaseURL: "sqlite://data/whatsapp.db", AuthToken: testToken, LogLevel: "info", PublicURL: "https://wa.example.com:8443"},
		},
		{
			name: "plain http is allowed on loopback",
			args: []string{"-public-url", "http://127.0.0.1:9000"},
			env:  map[string]string{"WHATSAPP_MCP_TOKEN": testToken},
			want: Config{Addr: "127.0.0.1:8080", DatabaseURL: "sqlite://data/whatsapp.db", AuthToken: testToken, LogLevel: "info", PublicURL: "http://127.0.0.1:9000"},
		},
		{
			name: "plain http is allowed on localhost, any case",
			args: []string{"-public-url", "http://LocalHost:9000"},
			env:  map[string]string{"WHATSAPP_MCP_TOKEN": testToken},
			want: Config{Addr: "127.0.0.1:8080", DatabaseURL: "sqlite://data/whatsapp.db", AuthToken: testToken, LogLevel: "info", PublicURL: "http://localhost:9000"},
		},
		{
			name: "plain http is allowed on IPv6 loopback",
			args: []string{"-public-url", "http://[::1]:9000"},
			env:  map[string]string{"WHATSAPP_MCP_TOKEN": testToken},
			want: Config{Addr: "127.0.0.1:8080", DatabaseURL: "sqlite://data/whatsapp.db", AuthToken: testToken, LogLevel: "info", PublicURL: "http://[::1]:9000"},
		},
		{name: "public url over plain http on all interfaces", args: []string{"-public-url", "http://0.0.0.0:9000"}, env: map[string]string{"WHATSAPP_MCP_TOKEN": testToken}, wantErr: "TLS reverse proxy"},
		{name: "public url over plain http on a localhost lookalike", args: []string{"-public-url", "http://localhost.example.com"}, env: map[string]string{"WHATSAPP_MCP_TOKEN": testToken}, wantErr: "TLS reverse proxy"},
		{name: "public url over plain http", args: []string{"-public-url", "http://wa.example.com"}, env: map[string]string{"WHATSAPP_MCP_TOKEN": testToken}, wantErr: "TLS reverse proxy"},
		{name: "public url with a path", args: []string{"-public-url", "https://example.com/wa"}, env: map[string]string{"WHATSAPP_MCP_TOKEN": testToken}, wantErr: "has a path"},
		{name: "public url with a query", args: []string{"-public-url", "https://wa.example.com/?x=1"}, env: map[string]string{"WHATSAPP_MCP_TOKEN": testToken}, wantErr: "query"},
		{name: "public url with credentials", args: []string{"-public-url", "https://me:pw@wa.example.com"}, env: map[string]string{"WHATSAPP_MCP_TOKEN": testToken}, wantErr: "credentials"},
		{name: "public url without a scheme", args: []string{"-public-url", "wa.example.com"}, env: map[string]string{"WHATSAPP_MCP_TOKEN": testToken}, wantErr: "absolute"},
		{name: "public url with another scheme", args: []string{"-public-url", "ftp://wa.example.com"}, env: map[string]string{"WHATSAPP_MCP_TOKEN": testToken}, wantErr: "absolute"},
		{
			name:    "stray argument",
			args:    []string{"serve"},
			env:     map[string]string{"WHATSAPP_MCP_TOKEN": testToken},
			wantErr: "unexpected arguments",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Load(tt.args, func(k string) string { return tt.env[k] })
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Load = %+v\nwant   %+v", got, tt.want)
			}
		})
	}
}
