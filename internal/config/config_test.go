// SPDX-License-Identifier: MPL-2.0

package config

import (
	"reflect"
	"strings"
	"testing"
)

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
			env:  map[string]string{"WHATSAPP_MCP_TOKEN": "t"},
			want: Config{Addr: "127.0.0.1:8080", DatabaseURL: "sqlite://data/whatsapp.db", AuthToken: "t", LogLevel: "info"},
		},
		{
			name: "flags beat env",
			args: []string{"-addr", ":9000", "-read-only=false"},
			env:  map[string]string{"WHATSAPP_MCP_TOKEN": "t", "WHATSAPP_MCP_ADDR": ":1", "WHATSAPP_MCP_READ_ONLY": "true"},
			want: Config{Addr: ":9000", DatabaseURL: "sqlite://data/whatsapp.db", AuthToken: "t", LogLevel: "info"},
		},
		{
			name: "read-only from env",
			env:  map[string]string{"WHATSAPP_MCP_TOKEN": "t", "WHATSAPP_MCP_READ_ONLY": "1"},
			want: Config{Addr: "127.0.0.1:8080", DatabaseURL: "sqlite://data/whatsapp.db", AuthToken: "t", LogLevel: "info", ReadOnly: true},
		},
		{
			name: "allowlist is trimmed and drops empty entries",
			args: []string{"-allow-recipients", " +31 6 1234 5678 , ,123@g.us,"},
			env:  map[string]string{"WHATSAPP_MCP_TOKEN": "t"},
			want: Config{
				Addr: "127.0.0.1:8080", DatabaseURL: "sqlite://data/whatsapp.db", AuthToken: "t", LogLevel: "info",
				AllowRecipients: []string{"+31 6 1234 5678", "123@g.us"},
			},
		},
		{
			name:    "missing token",
			wantErr: "WHATSAPP_MCP_TOKEN",
		},
		{
			name:    "invalid read-only env",
			env:     map[string]string{"WHATSAPP_MCP_TOKEN": "t", "WHATSAPP_MCP_READ_ONLY": "maybe"},
			wantErr: "WHATSAPP_MCP_READ_ONLY",
		},
		{
			name:    "read-only with allowlist",
			args:    []string{"-read-only", "-allow-recipients", "+31612345678"},
			env:     map[string]string{"WHATSAPP_MCP_TOKEN": "t"},
			wantErr: "no effect",
		},
		{
			name:    "stray argument",
			args:    []string{"serve"},
			env:     map[string]string{"WHATSAPP_MCP_TOKEN": "t"},
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
