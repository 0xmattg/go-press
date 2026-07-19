# 前台用户注册与身份登录

GoPress 把前台用户体系设计为“核心账号与 Session + 插件提供登录协议”。Core 不识别 Google、MetaMask 或任何主题：身份插件完成协议验证后，只向 core 提交统一的 `user.VerifiedIdentity`；主题只读取请求上下文中的安全用户视图和模板 helper。

## 架构边界

```text
浏览器
  -> 身份插件 start/callback 路由
  -> 插件验证具体协议（OIDC、钱包签名等）
  -> core PublicAuth.LoginVerifiedIdentityWithOptions
  -> IdentityBroker 执行策略判断与账号事务
  -> core 创建可撤销 Session
  -> 主题读取安全的当前用户视图
```

职责划分如下：

- **Core**：负责本地用户、外部身份绑定、注册策略、账号关联、Session、Cookie 和统一登录页。
- **身份插件**：负责跳转、challenge、签名或 Token 验证、Provider 设置，以及到 `VerifiedIdentity` 的映射。
- **主题**：只负责表现，通过 core 模板 helper 使用登录状态，不 import 或探测任何身份插件。

因此 Google OIDC 与 MetaMask 钱包登录可以并存，core 不需要加入 Provider 名称或协议特判。

## 核心数据模型

### User

`user.User` 是站内账号。`Email` 允许为空，`PasswordHash` 也可以为空，因此可以表示纯外部身份账号和纯钱包账号；后台创建的密码用户仍复用同一张表。

### UserIdentity

`user.UserIdentity` 将外部身份绑定到本地用户，稳定唯一键是：

```text
(provider, issuer, subject)
```

Core 将 `subject` 视为不透明值。OIDC 插件应使用验证过的 ID Token `sub`；钱包插件可使用规范化的链和地址标识。Email 只是资料字段，不是外部身份主键。GoPress 不会仅因为邮箱相同就静默绑定到已有账号。

### UserSession

`user.UserSession` 是可撤销的前台 Session。浏览器 Cookie `gopress_user_session` 保存高熵随机 bearer token，数据库只保存它的 SHA-256 hash，同时记录过期、撤销、最近访问、IP、User-Agent 和本次登录使用的 identity。

Cookie 使用 `HttpOnly`、`SameSite=Lax`；站点 URL 为 HTTPS 时自动启用 `Secure`。已登录请求和认证端点不会进入共享页面缓存。

## 注册策略设置

后台 **系统设置 > 账号设置** 提供以下 core 策略：

| Option key | 默认值 | 作用 |
|---|---:|---|
| `user_registration_enabled` | `0` | 是否允许创建前台用户。 |
| `new_user_default_role` | `subscriber` | 新用户默认角色；公开注册不能授予高于 subscriber 的角色。 |
| `external_identity_login_enabled` | `1` | 外部身份登录全局开关。 |
| `external_identity_auto_register_enabled` | `0` | 未找到 identity 时是否允许自动创建用户。 |
| `user_account_linking_enabled` | `1` | 已登录用户是否可以关联或解除外部身份。 |

关闭注册不会阻止已有 identity 登录。创建新账号必须同时满足：

1. 开放用户注册；
2. 开放外部身份自动注册；
3. 当前身份插件允许本次登录自动注册。

插件通过 `IdentityLoginOptions.AllowRegistration` 只能进一步收紧站点策略，不能绕过 core 中被关闭的注册设置。

## 身份插件接入

身份插件通过 `plugin.PublicAuthHost` 获取公共认证能力，并将同站 start 路径注册到 Provider Registry：

```go
type authHost interface {
    plugin.PublicAuthHost
    HookBus() *hook.Bus
}

func (p *Plugin) Activate(app plugin.App) {
    host, ok := app.(authHost)
    if !ok || host.PublicAuthenticator() == nil {
        return
    }

    p.auth = host.PublicAuthenticator()
    _ = p.auth.Providers().Register(user.ProviderDescriptor{
        ID:       "example-oidc",
        Label:    "Example Identity",
        BeginURL: "/auth/example/start",
        IconURL:  "/auth/example/assets/icon.svg",
        Priority: 20,
    })

    p.routeHandle = host.HookBus().AddAction("routes.register", p.registerRoutes, 20)
}
```

插件完成协议验证后，构造统一身份并交给 core：

```go
verified := user.VerifiedIdentity{
    Provider:      "example-oidc",
    Issuer:        verifiedIssuer,
    Subject:       verifiedSubject,
    Email:         verifiedEmail,
    EmailVerified: emailWasVerifiedByProvider,
    DisplayName:   displayName,
    AvatarURL:     avatarURL,
    ProfileJSON:   safeProfileJSON,
    VerifiedAt:    time.Now().UTC(),
}

result, err := p.auth.LoginVerifiedIdentityWithOptions(
    c,
    verified,
    user.IdentityLoginOptions{AllowRegistration: p.providerAllowsRegistration()},
)
```

调用前必须完成协议验证。OIDC 至少需要验证签名、issuer、audience、expiry、nonce 和 state；钱包签名至少需要一次性 challenge、站点域、过期时间、chain context 和 recovered signer。不能把浏览器提交的未验证字段直接包装成 `VerifiedIdentity`。

插件停用时必须注销 Provider、移除路由 Hook Handle，并用运行态开关保护旧 Router 上尚未结束的请求。身份关联路由不能接收用户 ID；应由 core 从已认证请求上下文获取账号，防止 IDOR。

## 主题接入

使用 `BaseTheme` 的主题自动获得以下 Provider-neutral helper：

| Helper | 返回值 |
|---|---|
| `isLoggedIn .Ctx` | 当前请求是否具有有效前台 Session。 |
| `currentUser .Ctx` | 安全的 `PublicUserView`：ID、用户名、邮箱、显示名、头像和角色。 |
| `loginURL .Ctx` | 带安全站内回跳路径的 `/login` URL。 |
| `logoutURL` | Core 的 `POST /logout` 地址。 |
| `loginProviders` | 当前可用 Provider 的只读描述列表。 |

Header 中的典型用法：

```gotemplate
{{if isLoggedIn .Ctx}}
    {{$account := currentUser .Ctx}}
    <span>{{$account.DisplayName}}</span>
    <form method="post" action="{{logoutURL}}">
        <input type="hidden" name="return_to" value="/">
        <button type="submit">退出</button>
    </form>
{{else}}
    <a href="{{loginURL .Ctx}}">登录</a>
{{end}}
```

主题应优先链接 `loginURL`，让 core 统一处理 Provider 选择、错误提示和回跳校验。主题不能 import `plugins/google-identity`、读取该插件的激活 Option，或根据 Provider ID 写业务分支。

## Google Identity 插件

内置 `google-identity` 插件实现服务端 Google OpenID Connect：Authorization Code Flow、PKCE、签名 state Cookie、nonce、Discovery/JWKS、audience/expiry、access-token hash、已验证邮箱和可选 Google Workspace `hd` 限制。

在 **后台 > 插件 > Google Identity > 设置** 中配置：

1. 在 Google Auth Platform 创建 **Web application** OAuth Client。
2. 本地开发添加精确的 Authorized redirect URI：

   ```text
   http://localhost:8080/auth/google/callback
   ```

3. 生产环境使用站点配置中的 HTTPS 域名，例如：

   ```text
   https://example.com/auth/google/callback
   ```

4. 将 Google 生成的 Client ID 与 Client Secret 填入插件设置。
5. 启用 Google 登录；只有确实需要创建新 Google 用户时才启用 Provider 自动注册。
6. Google 应用处于 Testing 状态时，将允许登录的 Gmail 加入 Test users。

Redirect URI 的 scheme、host、port、path 和末尾斜线必须完全一致。插件通过站点 URL 配置生成回调地址。

## MetaMask Identity 插件

内置 `metamask-identity` 插件为 MetaMask 浏览器扩展实现 EIP-4361 Sign-In with Ethereum。首版支持 EOA 账号，并使用维护中的 `signinwithethereum/siwe-go` 解析和验签，不在 GoPress 内自行实现消息解析或 secp256k1 恢复。

在 **后台 > 插件 > MetaMask Identity > 设置** 中配置：

1. 启用 MetaMask 登录；
2. 设置站点用于认证的 EIP-155 Chain ID。Ethereum Mainnet 为 `1`，本地开发链通常使用 `31337`；
3. 只有确实需要创建新钱包用户时才启用 Provider 自动注册；
4. 确认页面显示的 SIWE Origin 与 Domain 和站点公开 URL 一致。

浏览器登录流程如下：

1. 插件通过 EIP-6963 的 `rdns = io.metamask` 精确选择 MetaMask Provider，再请求当前 EOA 并检查配置的 Chain ID；
2. 服务端生成完整 SIWE message，数据库只保存不透明 Challenge token、nonce 和 message 的 hash；
3. MetaMask 通过 `personal_sign` 签署原始消息，不发交易，也不消耗 Gas；
4. 服务端校验完整消息、Origin、URI、nonce、签发/过期时间、Chain 和恢复出的 EOA 地址；
5. Challenge 在交给 core 前被原子消费一次，随后由 core 公共认证能力创建或登录用户。

钱包 Identity 使用 `Provider = ethereum`、`Issuer = eip155:<chain-id>`，规范化地址作为 `Subject`。请求 JSON 中的钱包地址未经签名验证绝不可信。EIP-1271/EIP-6492 智能合约钱包、移动端连接、ENS 资料和多链关联属于后续扩展，并继续留在 Provider 插件内部。

多个 EVM 钱包扩展同时安装时，不能直接依赖可能被任意扩展占用的 `window.ethereum`。MetaMask 入口优先使用 EIP-6963 公告信息，并在旧注入方式中排除 Phantom；如果未来支持 Phantom，应注册独立的 Provider 与“使用 Phantom 继续”入口，让每个按钮只调用对应钱包。

## 安全检查清单

- Provider Secret 只保留在服务端，不能输出到主题模板。
- OIDC、SIWE 和签名验证使用成熟协议库。
- Provider start URL 必须为同站路径，`return_to` 只能是安全站内地址。
- Provider 的可选 `IconURL` 必须是插件提供的同站本地资源，统一登录页不按 Provider 名称硬编码图标。
- 不按邮箱或钱包地址静默自动关联账号。
- 关联操作从 Session 获取所有者，不接受 URL/表单里的用户 ID。
- Provider 后台设置继续使用 `plugin.read` / `plugin.update` RBAC。
- 账号停用或凭据泄露时撤销相关 Session。
