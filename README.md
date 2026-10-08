# Key Chat Access

A CLIProxyAPI plugin that blocks selected users from `/v1/chat/completions` with HTTP 403 before calling an upstream model. Streaming and non-streaming requests are covered. Other users and the Responses, Messages, legacy Completions, and Models endpoints remain available.

## Manage users without calculating identifiers

Open **User Access** from the plugin's menu, or visit this path on your CPA server:

```text
/v0/resource/plugins/key-chat-access/settings
```

1. Connect using your management panel password. A saved session from the same management panel is reused automatically.
2. Search for the user by their API key label or visible key suffix.
3. Check **Block** and click **Save settings**. Uncheck and save to restore access.

The page calculates the caller scope automatically. It reads existing CPA API keys and saves only the selected scopes to the plugin configuration. It never creates, removes, or changes client keys. Existing rules, plugin priority, and installation metadata are preserved.

User names come from API key labels saved by the management panel in your current browser. If a key has no label, add one in the management panel and click **Reload** here. Browser labels are local; they do not change authentication. Long keys show the first 15 and last 12 characters; shorter keys show fewer characters to keep part of the key concealed. Users sharing a key share the same restriction. After rotating a key, select the new key; unmatched existing rules remain visible until explicitly unchecked.

The page must use HTTPS (localhost also works). It does not save management passwords. If your management panel session was not remembered, enter the same management password when prompted.

## Open the picker from Edit config

The optional management panel integration redirects **Edit config** for an enabled `key-chat-access` plugin directly to the User Access picker inside the existing management panel. Other plugins retain their standard configuration forms. When this plugin has no active User Access menu, its standard configuration form remains available.

This integration supports the exact upstream management panel **v1.25.6** recorded in `panel/source.json`. It verifies the panel hash, creates a private backup beside the original file, and replaces the file atomically. Installing the plugin alone does not modify the panel.

Use the authenticated management API:

1. `GET /v0/management/plugins/key-chat-access/panel-integration` returns the current `sha256`, `compatible`, and `installed` status.
2. `POST` to the same path with `{"action":"install","expected_sha256":"<current sha256>"}` installs the shortcut. Unsupported versions and stale hashes are rejected.
3. Set `management.panel-github-repository` to `https://github.com/L1nwatch/key-chat-access` to keep automatic panel updates on this customized release. Releases include a `management.html` asset derived from the verified upstream version.
4. Refresh the management panel, then click **Edit config** on `key-chat-access`.

To undo the integration, read the current hash and POST `{"action":"restore","expected_sha256":"<current sha256>"}`. This restores the original panel byte for byte. Restore `management.panel-github-repository` to `https://github.com/router-for-me/Cli-Proxy-API-Management-Center` to resume upstream panel updates. No client keys or access policies are changed by the panel installer.

## Install or update through the management panel

Tested with CLIProxyAPI **v8.0.4**, commit `d33f63f8`, ABI 1 / RPC schema 6. Prebuilt packages target Linux amd64 with glibc 2.34 or newer.

1. Add this URL to **Plugin store sources** (`plugins.store-sources`), preserving existing sources:

   ```text
   https://raw.githubusercontent.com/L1nwatch/key-chat-access/main/registry.json
   ```

2. Enable the global plugin switch (`plugins.enabled`).
3. Refresh the plugin store, find `key-chat-access`, and install or update it.
4. Enable `key-chat-access` under plugin management, then open **User Access**.

Installation and upgrading from v0.1.0 to v0.2.0 were verified using only the management API, without SSH or a server restart. The existing block list is retained. Verify that the plugin is registered and effectively enabled after installation.

## Advanced configuration

The UI manages this configuration for you. Merge this example into an existing CPA configuration; it is not a complete configuration file:

```yaml
plugins:
  enabled: true
  configs:
    key-chat-access:
      enabled: true
      priority: 1000
      blocked_caller_scopes: []
```

An empty list allows all users. Disabling the plugin or the global plugin switch also removes restrictions. The User Access page is served by the enabled plugin; re-enable it in the main management panel if you disabled it there.

For scripts or manual configuration, `python3 caller_scope.py` prompts for a client API key without echoing it or putting it in shell history. Paste the resulting scope into `blocked_caller_scopes`. Scope identity follows the host's `CallerScope` algorithm, including its domain prefix; it is not a plain hash of the key.

The interceptor matches the authenticated host-provided `caller_scope` and `request_path`. Client-supplied usernames and identity headers cannot override them. The restricted endpoint is fixed to `/v1/chat/completions`; other protocols may still invoke the same underlying models.

## Management API

Management requests require `Authorization: Bearer <MANAGEMENT_KEY>`, using the management password, not a client API key.

- `GET /v8/management/config/access/api-keys`: read existing client keys.
- `PATCH /v8/management/config/plugins/configs/key-chat-access`: update plugin policy fields.
- `POST /v8/management/plugins/store/key-chat-access/install?source=<source-id>`: install from the source ID returned by `GET /v8/management/plugins/store`.
- `GET /v8/management/plugins`: verify `registered`, `enabled`, and `effective_enabled`, and locate the User Access menu.
- `PUT /v8/management/config/plugins/configs/key-chat-access/enabled`: set the switch using a bare JSON `true` or `false` body.

Only static assets are served by the public plugin resource routes. Data reads and policy changes use the host's authenticated management API. The page escapes user labels and detects stale policy changes before saving. It patches only `blocked_caller_scopes`.

## Build and test

Requires Go 1.26 and a C compiler. UI helper tests require Node.js 22 or newer.

```bash
go test -race ./...
node --test web_test.mjs
go build -trimpath -ldflags=-s -buildmode=c-shared -o dist/linux/amd64/key-chat-access.so .
python3 integration_test.py --cpa /path/to/v8.0.4/cli-proxy-api
```

The integration test uses a local mock upstream and fixture keys. It verifies API-only installation, all supported authentication carriers, streaming and non-streaming blocks, 8 MiB bodies, identity spoofing resistance, zero upstream calls for blocked requests, unaffected users/protocols, and management switch hot reload.

To also test upgrading and the actual browser UI, provide the previous release archive and a Playwright module with Chromium installed:

```bash
python3 integration_test.py \
  --cpa /path/to/v8.0.4/cli-proxy-api \
  --upgrade-from /path/to/key-chat-access_0.1.0_linux_amd64.zip \
  --playwright-module /path/to/node_modules/playwright/index.mjs
```

Browser checks cover login, remembered management sessions, key labels, search, adding/removing restrictions with actual 403/200 enforcement, preserving hidden rules and installation metadata, concurrent edits, escaped labels, and mobile layout.

## Release

```bash
python3 package.py --base-url https://github.com/L1nwatch/key-chat-access/releases/download/v0.3.0
python3 panel_patch.py
```

Publish the ZIP, checksum file, registry, patched `management.html`, and `panel/LICENSE` as release assets, then copy `release/registry.json` to the repository root. Packages contain only the native library with its embedded static page and panel patch instructions. Never publish real API keys or personal policies. The custom panel is built from a SHA-256-pinned upstream release; its original MIT license is in `panel/LICENSE`.

## License and protocol

MIT. Native ABI declarations follow the [official CLIProxyAPI v8.0.4 plugin example](https://github.com/router-for-me/CLIProxyAPI/blob/v8.0.4/examples/plugin/simple/go/main.go). Requests are stopped using the official `Terminate` / `StatusCode` interceptor response.
