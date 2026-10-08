package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct { void* ptr; size_t len; } cliproxy_buffer;
typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);
typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	cliproxy_host_call_fn call;
	cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;
typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);
typedef struct {
	uint32_t abi_version;
	cliproxy_plugin_call_fn call;
	cliproxy_plugin_free_fn free_buffer;
	cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;
extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"unsafe"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type lifecycleRequest struct {
	ConfigYAML []byte `json:"config_yaml"`
}

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if host == nil || plugin == nil || host.abi_version != C.uint32_t(pluginabi.ABIVersion) {
		return 1
	}
	plugin.abi_version = C.uint32_t(pluginabi.ABIVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response == nil {
		return 1
	}
	response.ptr = nil
	response.len = 0
	if method == nil || requestLen > 64*1024*1024 || (request == nil && requestLen != 0) {
		writeResponse(response, errorResponse("invalid_request", "invalid plugin RPC request"))
		return 1
	}
	var raw []byte
	if requestLen > 0 {
		raw = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}
	result, err := handleMethod(C.GoString(method), raw)
	if err != nil {
		writeResponse(response, errorResponse("plugin_error", err.Error()))
		return 1
	}
	data, err := json.Marshal(result)
	if err != nil {
		writeResponse(response, errorResponse("encode_error", "cannot encode plugin response"))
		return 1
	}
	envelope, err := json.Marshal(pluginabi.Envelope{OK: true, Result: data})
	if err != nil {
		writeResponse(response, errorResponse("encode_error", "cannot encode plugin response"))
		return 1
	}
	writeResponse(response, envelope)
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, length C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {}

func errorResponse(code, message string) []byte {
	data, err := pluginabi.NewErrorEnvelope(code, message)
	if err != nil {
		data = []byte(`{"ok":false,"error":{"code":"encode_error","message":"cannot encode plugin response"}}`)
	}
	return data
}

func writeResponse(response *C.cliproxy_buffer, data []byte) {
	response.ptr = C.CBytes(data)
	response.len = C.size_t(len(data))
}

func handleMethod(method string, raw []byte) (any, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		var req lifecycleRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, fmt.Errorf("invalid lifecycle envelope")
		}
		p, err := parsePolicy(req.ConfigYAML)
		if err != nil {
			return nil, err
		}
		activePolicy.Store(p)
		return map[string]any{
			"schema_version": pluginabi.SchemaVersion,
			"metadata": pluginapi.Metadata{
				Name: pluginID, Version: "0.2.0", Author: "Local administration",
				GitHubRepository: "https://github.com/L1nwatch/key-chat-access",
				ConfigFields: []pluginapi.ConfigField{{
					Name: "blocked_caller_scopes", Type: pluginapi.ConfigFieldTypeArray,
					Description: "Client caller scopes blocked from /v1/chat/completions. Use User Access to select users; an empty list allows everyone.",
				}},
			},
			"capabilities": map[string]bool{"request_interceptor": true, "management_api": true},
		}, nil
	case pluginabi.MethodManagementRegister:
		return managementRegistration(), nil
	case pluginabi.MethodManagementHandle:
		var req pluginapi.ManagementRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, fmt.Errorf("invalid management envelope")
		}
		return managementResource(req), nil
	case pluginabi.MethodRequestInterceptBefore, pluginabi.MethodRequestInterceptAfter:
		// Decode only the fields needed by the policy. In particular, don't
		// base64-decode or parse potentially large client request bodies.
		var req struct{ Metadata map[string]any }
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, fmt.Errorf("invalid request interceptor envelope")
		}
		return activePolicy.Load().intercept(pluginapi.RequestInterceptRequest{Metadata: req.Metadata}), nil
	case pluginabi.MethodPluginQuiesce, pluginabi.MethodPluginShutdown:
		return struct{}{}, nil
	default:
		return nil, fmt.Errorf("unsupported method")
	}
}
