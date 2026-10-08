# Key Chat Access

CLIProxyAPI 插件：按已鉴权的客户端 API Key，禁止指定用户调用 `/v1/chat/completions`。命中规则后直接返回 HTTP 403，不调用上游模型，流式和非流式均生效。

- 可配置多个用户，可随时增删或关闭规则。
- 其他用户及 Responses、Messages、旧版 `/v1/completions`、模型列表正常使用。
- 使用 CPA 认证上下文的 `caller_scope` 匹配身份，不依赖客户端自行填写的用户名或身份 Header。
- 配置中只保存不可逆 caller scope，无需保存原始客户端 API Key。

兼容性：已在 CLIProxyAPI **v8.0.4**（commit `d33f63f8`、ABI 1 / RPC schema 6）上验证。预编译安装包适用于 Linux amd64、glibc 2.34 或更新系统。

## 通过管理页面安装，无需 SSH

1. 在 CPA 管理页的配置面板中找到**插件商店源**（`plugins.store-sources`），添加以下地址并保存，保留已有的源：

   ```text
   https://raw.githubusercontent.com/L1nwatch/key-chat-access/main/registry.json
   ```

2. 确保全局插件开关 `plugins.enabled` 为 `true`。

3. 打开**插件商店**，刷新，搜索 `key-chat-access`，点击安装。CPA 会下载带 SHA-256 校验的安装包并热加载插件。

4. 在**插件管理**中找到 `key-chat-access`，启用插件，并填写 `blocked_caller_scopes`。空列表允许所有用户。

5. 确认插件已加载、已启用；用被限制用户的 Key 调用 `/v1/chat/completions` 应返回 HTTP 403，错误码为 `chat_completions_disabled`。

无需正常情况下的服务重启。若页面提示加载失败或必须重启，不应视为规则已生效。

## 生成用户的 caller scope

在自己的电脑上运行：

```bash
python3 caller_scope.py
```

按提示输入用户的**客户端 API Key**，输入不会回显，也不会作为命令行参数写入 shell 历史。复制输出的 64 位十六进制字符串到插件配置的 `blocked_caller_scopes` 数组。

scope 与该 Key 精确对应；更换 Key 后需要重新生成。同一个 Key 若多人共用，规则会同时作用于这些人。

## 配置示例

将下面的节点合并到现有 CPA 配置，勿用片段替换完整配置文件：

```yaml
plugins:
  enabled: true
  configs:
    key-chat-access:
      enabled: true
      priority: 1000
      blocked_caller_scopes:
        - "替换为 caller_scope.py 生成的 64 位十六进制字符串"
```

- `enabled`：插件开关，关闭后恢复 CPA 原有行为。
- `blocked_caller_scopes`：禁止调用 Chat Completions 的用户列表，支持多个；空数组允许全部。
- `priority`：CPA 的插件优先级。

目前限制的接口固定为 `/v1/chat/completions`。请求在选择上游凭据前终止，使用其他上游模型或改为流式不会绕过规则。插件不阻止用户通过其他保留的协议调用同一个模型。

## Management API

所有管理请求需要 `Authorization: Bearer <MANAGEMENT_KEY>`，使用管理密码，与客户端 API Key 区分。

- `PUT /v8/management/config/plugins/store-sources`：添加自定义源，提交含已有源的完整 URL 数组。
- `PATCH /v8/management/config/plugins/configs/key-chat-access`：提交插件配置对象。
- `POST /v8/management/plugins/store/key-chat-access/install?source=<source-id>`：安装，`source-id` 来自 `GET /v8/management/plugins/store` 中该插件的条目。
- `GET /v8/management/plugins`：确认插件 `registered`、`enabled` 和 `effective_enabled`。
- `PUT /v8/management/config/plugins/configs/key-chat-access/enabled`：开关，正文直接使用 `true` 或 `false`，无需 `{value: ...}` 包装。

主机关闭全局插件、插件未注册或被停用时，此规则不会执行。安装后应确认插件状态及实际 403 行为。

## 构建与测试

依赖 Go 1.26 和 C 编译器，使用官方 v8.0.4 的插件类型：

```bash
go test -race ./...
go build -trimpath -ldflags=-s -buildmode=c-shared -o dist/linux/amd64/key-chat-access.so .
python3 integration_test.py --cpa /path/to/v8.0.4/cli-proxy-api
```

集成测试只使用本地假 Key、模型和上游，包含：通过管理 API 安装及热加载；流式、非流式和 8 MiB 请求体拒绝；Bearer、X-Api-Key、X-Goog-Api-Key、key/auth_token 查询参数的身份一致性；伪造身份字段不能绕过；被拒绝请求的上游调用数为零；其他用户和协议正常；管理 API 开关热更新。

未知配置字段、格式错误或重复的 scope 会被拒绝，插件内部保留原策略。主机是否仍注册并启用插件，仍应通过管理 API 确认。

## 发布自定义源

```bash
python3 package.py --base-url https://github.com/L1nwatch/key-chat-access/releases/download/v0.1.0
```

将生成的 ZIP 发布为 GitHub Release 附件，将 `release/registry.json` 更新到仓库根目录。插件安装包只包含动态库；用户策略和原始 Key 不应放入公开仓库或安装包。

## 协议来源

原生 ABI 声明参考 [CLIProxyAPI v8.0.4 插件示例](https://github.com/router-for-me/CLIProxyAPI/blob/v8.0.4/examples/plugin/simple/go/main.go)。插件通过官方 `Terminate` / `StatusCode` 响应终止请求。项目采用 MIT 许可。
