package main

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

//go:embed panel/source.json
var panelSourceJSON []byte

//go:embed panel/redirect.js
var panelRedirect []byte

const panelRoute = "/v0/management/plugins/" + pluginID + "/panel-integration"
const panelMarker = "/*key-chat-access:edit-config:v1*/"
const maxPanelSize = 50 * 1024 * 1024

var panelMu sync.Mutex
var errPanelConflict = errors.New("panel changed or does not match the supported upstream release")

type panelSource struct {
	SHA256 string `json:"sha256"`
	Anchor string `json:"anchor"`
}

type panelState struct {
	SHA256     string `json:"sha256"`
	Installed  bool   `json:"installed"`
	Compatible bool   `json:"compatible"`
}

func digest(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func knownPanelSource() panelSource {
	var source panelSource
	_ = json.Unmarshal(panelSourceJSON, &source)
	return source
}

func patchPanel(data []byte) ([]byte, error) {
	source := knownPanelSource()
	anchor := []byte(source.Anchor)
	if digest(data) != source.SHA256 || len(anchor) == 0 || bytes.Count(data, anchor) != 1 {
		return nil, errPanelConflict
	}
	replacement := append(bytes.Clone(anchor), bytes.TrimSpace(panelRedirect)...)
	return bytes.Replace(data, anchor, replacement, 1), nil
}

// Resolve exactly the standard host management asset location. The API never
// accepts a client-supplied file path or arbitrary replacement bytes.
func managementPanelPath() (string, error) {
	if override := strings.TrimSpace(os.Getenv("MANAGEMENT_STATIC_PATH")); override != "" {
		if strings.EqualFold(filepath.Base(override), "management.html") {
			return filepath.Abs(filepath.Clean(override))
		}
		return filepath.Abs(filepath.Join(override, "management.html"))
	}
	for _, name := range []string{"WRITABLE_PATH", "writable_path"} {
		if writable := strings.TrimSpace(os.Getenv(name)); writable != "" {
			return filepath.Abs(filepath.Join(writable, "static", "management.html"))
		}
	}
	args := os.Args
	// A c-shared Go runtime may have a different os.Args snapshot from the
	// host runtime. Linux procfs supplies the actual process command line.
	if raw, err := os.ReadFile("/proc/self/cmdline"); err == nil && len(raw) != 0 {
		args = strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	}
	configPath := "config.yaml"
	for i, arg := range args {
		if (arg == "-config" || arg == "--config") && i+1 < len(args) {
			configPath = args[i+1]
		}
		if strings.HasPrefix(arg, "-config=") || strings.HasPrefix(arg, "--config=") {
			configPath = strings.SplitN(arg, "=", 2)[1]
		}
	}
	return filepath.Abs(filepath.Join(filepath.Dir(configPath), "static", "management.html"))
}

func readPanel(filename string) ([]byte, os.FileMode, error) {
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxPanelSize {
		return nil, 0, errors.New("management panel file is unavailable or is not a regular supported file")
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, 0, errors.New("cannot read the management panel file")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxPanelSize+1))
	if err != nil || len(data) > maxPanelSize {
		return nil, 0, errors.New("cannot read the management panel file")
	}
	return data, info.Mode().Perm(), nil
}

func inspectPanel(data []byte) panelState {
	source := knownPanelSource()
	installed := bytes.Contains(data, []byte(panelMarker))
	return panelState{SHA256: digest(data), Installed: installed, Compatible: installed || digest(data) == source.SHA256}
}

func backupPanel(filename string, data []byte) error {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if os.IsExist(err) {
		existing, _, readErr := readPanel(filename)
		if readErr == nil && bytes.Equal(existing, data) {
			return nil
		}
		return errors.New("existing panel backup differs; it was not overwritten")
	}
	if err != nil {
		return errors.New("cannot create the management panel backup")
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(filename)
		return errors.New("cannot save the management panel backup")
	}
	return nil
}

func replacePanel(filename string, data []byte, mode os.FileMode, expected string) error {
	file, err := os.CreateTemp(filepath.Dir(filename), ".key-chat-access-panel-*")
	if err != nil {
		return errors.New("cannot create the updated management panel file")
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(mode); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return errors.New("cannot save the updated management panel file")
	}
	current, _, err := readPanel(filename)
	if err != nil || digest(current) != expected {
		return errPanelConflict
	}
	if err = os.Rename(file.Name(), filename); err != nil {
		return errors.New("cannot atomically replace the management panel file")
	}
	return nil
}

func updatePanel(filename, action, expected string) (panelState, error) {
	panelMu.Lock()
	defer panelMu.Unlock()
	data, mode, err := readPanel(filename)
	if err != nil {
		return panelState{}, err
	}
	state := inspectPanel(data)
	if len(expected) != 64 || expected != state.SHA256 {
		return panelState{}, errPanelConflict
	}
	backup := filepath.Join(filepath.Dir(filename), ".key-chat-access-panel-backup-"+knownPanelSource().SHA256+".html")
	var next []byte
	switch action {
	case "install":
		if state.Installed {
			return state, nil
		}
		next, err = patchPanel(data)
		if err == nil {
			err = backupPanel(backup, data)
		}
	case "restore":
		if state.SHA256 == knownPanelSource().SHA256 {
			return state, nil
		}
		next, _, err = readPanel(backup)
		if err == nil && digest(next) != knownPanelSource().SHA256 {
			err = errors.New("management panel backup failed verification")
		}
	default:
		err = errors.New("action must be install or restore")
	}
	if err != nil {
		return panelState{}, err
	}
	if err = replacePanel(filename, next, mode, expected); err != nil {
		return panelState{}, err
	}
	return inspectPanel(next), nil
}

func panelManagement(req pluginapi.ManagementRequest) pluginapi.ManagementResponse {
	respond := func(status int, value any) pluginapi.ManagementResponse {
		data, _ := json.Marshal(value)
		return pluginapi.ManagementResponse{StatusCode: status, Headers: http.Header{
			"Content-Type": {"application/json"}, "Cache-Control": {"no-store"},
		}, Body: data}
	}
	filename, err := managementPanelPath()
	var state panelState
	if err == nil && req.Method == http.MethodGet {
		var data []byte
		data, _, err = readPanel(filename)
		state = inspectPanel(data)
	} else if err == nil && req.Method == http.MethodPost {
		var input struct {
			Action   string `json:"action"`
			Expected string `json:"expected_sha256"`
		}
		decoder := json.NewDecoder(bytes.NewReader(req.Body))
		decoder.DisallowUnknownFields()
		if len(req.Body) > 4096 || decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
			return respond(http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		}
		state, err = updatePanel(filename, input.Action, input.Expected)
	} else if err == nil {
		return respond(http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errPanelConflict) {
			status = http.StatusConflict
		}
		return respond(status, map[string]string{"error": "panel_integration_failed", "message": fmt.Sprint(err)})
	}
	return respond(http.StatusOK, state)
}
