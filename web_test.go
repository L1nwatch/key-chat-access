package main

import (
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestManagementResources(t *testing.T) {
	for _, resource := range managementRegistration().Resources {
		resp := managementResource(pluginapi.ManagementRequest{Method: "GET", Path: resourcePrefix + strings.TrimPrefix(resource.Path, "/")})
		if resp.StatusCode != 200 || len(resp.Body) == 0 || resp.Headers.Get("Content-Type") == "" {
			t.Fatalf("resource %s: invalid response", resource.Path)
		}
		if !strings.Contains(resp.Headers.Get("Content-Security-Policy"), "connect-src 'self'") {
			t.Fatal("resources must restrict connections to the host origin")
		}
	}
	for _, req := range []pluginapi.ManagementRequest{
		{Method: "POST", Path: resourcePrefix + "settings"},
		{Method: "GET", Path: resourcePrefix + "../main.go"},
		{Method: "GET", Path: "/settings"},
	} {
		if managementResource(req).StatusCode != 404 {
			t.Fatalf("unexpected resource served for %+v", req)
		}
	}
}
