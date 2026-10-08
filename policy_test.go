package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestPolicyIsolation(t *testing.T) {
	scope := strings.Repeat("a", 64)
	p, err := parsePolicy([]byte("enabled: true\nblocked_caller_scopes: [" + scope + "]\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, caller, route string
		block               bool
	}{
		{"target chat", scope, "/v1/chat/completions", true},
		{"normalized chat path", scope, "/v1//chat/completions/", true},
		{"other user chat", strings.Repeat("b", 64), "/v1/chat/completions", false},
		{"responses", scope, "/v1/responses", false},
		{"messages", scope, "/v1/messages", false},
		{"legacy completions", scope, "/v1/completions", false},
		{"models", scope, "/v1/models", false},
		{"missing scope", "", "/v1/chat/completions", false},
		{"missing path", scope, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := p.intercept(pluginapi.RequestInterceptRequest{Metadata: map[string]any{"caller_scope": tc.caller, "request_path": tc.route}})
			if r.Terminate != tc.block {
				t.Fatalf("terminate=%v, want %v", r.Terminate, tc.block)
			}
			if tc.block {
				if r.StatusCode != 403 || !json.Valid(r.ResponseBody) {
					t.Fatalf("invalid rejection: %+v", r)
				}
			} else if len(r.Headers) != 0 || len(r.Body) != 0 || len(r.ClearHeaders) != 0 {
				t.Fatal("allowed request was modified")
			}
		})
	}
}

func TestConfigRejectsMistakes(t *testing.T) {
	for _, raw := range []string{
		"blocked_caller_scope: []", "blocked_caller_scopes: [username]",
		"blocked_caller_scopes: [" + strings.Repeat("g", 64) + "]",
		"blocked_caller_scopes: [" + strings.Repeat("a", 64) + ", " + strings.Repeat("a", 64) + "]",
		"enabled: true\n---\nenabled: false", "enabled: false\nenabled: true",
	} {
		if _, err := parsePolicy([]byte(raw)); err == nil {
			t.Errorf("accepted invalid policy %q", raw)
		}
	}
}

func TestDisabledAndEmptyPolicies(t *testing.T) {
	for _, raw := range []string{"", "enabled: true", "enabled: false\nblocked_caller_scopes: [" + strings.Repeat("a", 64) + "]"} {
		p, err := parsePolicy([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		r := p.intercept(pluginapi.RequestInterceptRequest{Metadata: map[string]any{"caller_scope": strings.Repeat("a", 64), "request_path": "/v1/chat/completions"}})
		if r.Terminate {
			t.Fatal("disabled/empty policy blocked request")
		}
	}
}

func TestInvalidReconfigureKeepsPreviousPolicy(t *testing.T) {
	scope := strings.Repeat("c", 64)
	register, _ := json.Marshal(lifecycleRequest{ConfigYAML: []byte("blocked_caller_scopes: [" + scope + "]")})
	if _, err := handleMethod("plugin.register", register); err != nil {
		t.Fatal(err)
	}
	bad, _ := json.Marshal(lifecycleRequest{ConfigYAML: []byte("blocked_caller_scopes: [invalid]")})
	if _, err := handleMethod("plugin.reconfigure", bad); err == nil {
		t.Fatal("bad policy was accepted")
	}
	for _, stage := range []string{"request.intercept_before", "request.intercept_after"} {
		request, _ := json.Marshal(map[string]any{"Stream": true, "Metadata": map[string]any{"caller_scope": scope, "request_path": "/v1/chat/completions"}})
		r, err := handleMethod(stage, request)
		if err != nil {
			t.Fatal(err)
		}
		if !r.(pluginapi.RequestInterceptResponse).Terminate {
			t.Fatal("previous policy was lost")
		}
	}
}
