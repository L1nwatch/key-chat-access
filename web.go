package main

import (
	"embed"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

//go:embed web/settings.html web/settings.css web/settings.js
var webAssets embed.FS

const resourcePrefix = "/v0/resource/plugins/" + pluginID + "/"

func managementRegistration() pluginapi.ManagementRegistrationResponse {
	return pluginapi.ManagementRegistrationResponse{Routes: []pluginapi.ManagementRoute{
		{Method: "GET", Path: panelRoute, Description: "Inspect the optional Edit config shortcut."},
		{Method: "POST", Path: panelRoute, Description: "Install or restore the verified management panel shortcut with a backup."},
	}, Resources: []pluginapi.ResourceRoute{
		{Path: "/settings", Menu: "User Access", Description: "Select users to block Chat Completions without calculating identifiers."},
		{Path: "/settings.css"},
		{Path: "/settings.js"},
	}}
}

// Resources contain only the static app. All configuration reads and writes go
// through the host's authenticated Management API, never a public plugin route.
func managementResource(req pluginapi.ManagementRequest) pluginapi.ManagementResponse {
	name := strings.TrimPrefix(req.Path, resourcePrefix)
	contentType := ""
	switch name {
	case "settings":
		name, contentType = "settings.html", "text/html; charset=utf-8"
	case "settings.css":
		contentType = "text/css; charset=utf-8"
	case "settings.js":
		contentType = "text/javascript; charset=utf-8"
	}
	if req.Method != http.MethodGet || !strings.HasPrefix(req.Path, resourcePrefix) || contentType == "" {
		return pluginapi.ManagementResponse{StatusCode: http.StatusNotFound, Body: []byte("Not found")}
	}
	data, err := webAssets.ReadFile("web/" + name)
	if err != nil {
		return pluginapi.ManagementResponse{StatusCode: http.StatusInternalServerError}
	}
	return pluginapi.ManagementResponse{
		StatusCode: http.StatusOK,
		Headers: http.Header{
			"Content-Type": {contentType}, "Cache-Control": {"no-store"},
			"X-Content-Type-Options": {"nosniff"}, "Referrer-Policy": {"no-referrer"},
			"Content-Security-Policy": {"default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'self'"},
		},
		Body: data,
	}
}
