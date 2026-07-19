# Public Authentication

GoPress public authentication is a core account and session capability with protocol-specific authentication supplied by plugins. Core does not know about Google, MetaMask, or any theme. A provider plugin verifies its own protocol and passes a provider-neutral `user.VerifiedIdentity` to core; themes only consume the authenticated request context and template helpers.

## Architecture Boundary

```text
Browser
  -> provider plugin start/callback route
  -> provider protocol verification (OIDC, wallet signature, ...)
  -> core PublicAuth.LoginVerifiedIdentityWithOptions
  -> IdentityBroker policy and account transaction
  -> revocable core session
  -> theme reads the safe current-user view
```

The ownership rules are:

- **Core** owns users, identity bindings, registration policy, account linking, sessions, cookies, and the login page.
- **Provider plugins** own protocol redirects, challenges, signatures or token verification, provider settings, and profile-to-`VerifiedIdentity` mapping.
- **Themes** own presentation only. They use core template helpers and never import or detect a provider plugin.

This separation lets Google OIDC and MetaMask wallet sign-in coexist without adding provider names or protocol branches to core.

## Core Data Model

### User

`user.User` is the local account. `Email` is nullable and `PasswordHash` may be empty so external-only and wallet-only accounts are representable. Administrative users created with a password continue to use the same table.

### UserIdentity

`user.UserIdentity` binds an external principal to a local user. The stable identity key is the unique tuple:

```text
(provider, issuer, subject)
```

Core treats `subject` as opaque. An OIDC plugin should use the verified ID Token subject, while a wallet plugin can use a canonical chain/address identifier. Email is profile data, not the identity key. GoPress does not silently link an incoming identity to an existing account by matching email.

### UserSession

`user.UserSession` is a revocable public-site session. The browser receives a random bearer token in the `gopress_user_session` cookie; the database stores only its SHA-256 hash. Sessions record expiry, revocation, last-seen time, IP address, User-Agent, and the identity used to sign in.

The cookie is `HttpOnly`, `SameSite=Lax`, and `Secure` when the configured site URL uses HTTPS. Public-authenticated pages bypass the shared page cache.

## Registration Policy

The **Admin > Settings > Account Settings** section controls core policy:

| Option key | Default | Purpose |
|---|---:|---|
| `user_registration_enabled` | `0` | Allows creation of public user accounts. |
| `new_user_default_role` | `subscriber` | Role assigned to a newly provisioned account. Public registration cannot grant a role above subscriber. |
| `external_identity_login_enabled` | `1` | Global kill switch for external identity login. |
| `external_identity_auto_register_enabled` | `0` | Allows a verified identity to provision an account when no binding exists. |
| `user_account_linking_enabled` | `1` | Allows authenticated users to link or unlink external identities. |

Existing identity bindings can sign in when external login is enabled even if registration is closed. Creating a new account requires all of the following:

1. Public registration is enabled.
2. External identity auto-registration is enabled.
3. The provider plugin allows registration for that login attempt.

A provider can narrow site policy with `IdentityLoginOptions.AllowRegistration`; it cannot override a disabled core policy.

## Provider Plugin Integration

Provider plugins consume `plugin.PublicAuthHost` and register a same-site start URL with the core provider registry:

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

After completing protocol verification, construct the neutral assertion and hand it to core:

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

The plugin must verify the protocol before this call. For OIDC that includes signature, issuer, audience, expiry, nonce, and state validation. For signed-wallet login it includes a one-time challenge, domain binding, expiry, chain context, and recovered signer validation. Do not pass unverified browser claims to core.

On deactivation, unregister the provider, remove route Hook handles, and guard requests already running on the old router. Provider routes must not accept a user ID for linking; core derives the account from the authenticated request context to prevent IDOR.

## Theme Integration

Themes using `BaseTheme` receive these provider-neutral helpers:

| Helper | Result |
|---|---|
| `isLoggedIn .Ctx` | Whether the request has a valid public session. |
| `currentUser .Ctx` | A `PublicUserView` with ID, username, email, display name, avatar URL, and role. |
| `loginURL .Ctx` | `/login` URL with a safe same-site return path. |
| `logoutURL` | Core `POST /logout` endpoint. |
| `loginProviders` | Read-only descriptors for currently available providers. |

Typical header rendering:

```gotemplate
{{if isLoggedIn .Ctx}}
    {{$account := currentUser .Ctx}}
    <span>{{$account.DisplayName}}</span>
    <form method="post" action="{{logoutURL}}">
        <input type="hidden" name="return_to" value="/">
        <button type="submit">Sign out</button>
    </form>
{{else}}
    <a href="{{loginURL .Ctx}}">Sign in</a>
{{end}}
```

Prefer linking to `loginURL` so core owns provider selection, error handling, and return-path validation. A theme must not import `plugins/google-identity`, check plugin activation options, or branch on provider IDs.

## Google Identity Plugin

The bundled `google-identity` plugin implements server-side Google OpenID Connect with Authorization Code Flow, PKCE, signed state cookie, nonce, Discovery/JWKS verification, audience and expiry validation, access-token hash verification, verified email enforcement, and optional Google Workspace `hd` restriction.

Configure it under **Admin > Plugins > Google Identity > Settings**:

1. Create a **Web application** OAuth client in Google Auth Platform.
2. For local development, add this exact Authorized redirect URI:

   ```text
   http://localhost:8080/auth/google/callback
   ```

3. For production, use the configured HTTPS site origin, for example:

   ```text
   https://example.com/auth/google/callback
   ```

4. Paste the generated Client ID and Client Secret into the plugin settings.
5. Enable Google sign-in. Enable provider auto-registration only when new Google accounts should be provisioned.
6. In Google testing mode, add the Gmail accounts that may sign in as test users.

The redirect URI must match exactly, including scheme, host, port, path, and trailing slash. The plugin reads its callback origin from the configured site URL.

## MetaMask Identity Plugin

The bundled `metamask-identity` plugin implements EIP-4361 Sign-In with Ethereum for the MetaMask browser extension. It supports EOA accounts in its first release and uses the maintained `signinwithethereum/siwe-go` parser and verifier instead of implementing message parsing or secp256k1 recovery locally.

Configure it under **Admin > Plugins > MetaMask Identity > Settings**:

1. Enable MetaMask sign-in.
2. Set the site's authentication EIP-155 Chain ID. Ethereum Mainnet is `1`; a local development chain commonly uses `31337`.
3. Enable provider auto-registration only when new wallet accounts should be provisioned.
4. Confirm the displayed SIWE Origin and Domain match the public site URL.

The browser flow is:

1. The plugin selects the MetaMask provider through EIP-6963 with `rdns = io.metamask`, then requests the current EOA and verifies the configured Chain ID.
2. The server creates the complete SIWE message and stores only hashes of its opaque challenge token, nonce, and message.
3. MetaMask signs the exact message with `personal_sign`; no transaction is submitted and no gas is spent.
4. The server validates the exact message, origin, URI, nonce, issue/expiry time, chain, and recovered EOA address.
5. The challenge is atomically consumed once before the verified Ethereum identity is passed to core public auth.

Wallet identities use `Provider = ethereum`, `Issuer = eip155:<chain-id>`, and the normalized address as `Subject`. Wallet addresses from request JSON are never trusted without signature verification. EIP-1271/EIP-6492 smart-contract wallets, mobile connectors, ENS profiles, and multi-chain linking remain later extensions and will stay inside the provider plugin.

When several EVM wallet extensions are installed, code must not rely directly on `window.ethereum`, which may be claimed by any extension. The MetaMask entry first uses EIP-6963 announcements and excludes Phantom from the legacy injected-provider fallback. Future Phantom support should register a separate provider and button so each entry invokes only its named wallet.

## Security Checklist

- Keep provider secrets server-side and never render them into frontend templates.
- Use mature protocol libraries for OIDC, SIWE, and signature verification.
- Accept only same-site provider start URLs and safe local `return_to` paths.
- An optional provider `IconURL` must point to a same-site asset served by the plugin; the shared login page does not hard-code provider-specific icons.
- Never auto-link by email or wallet address without an authenticated linking flow.
- Derive linking ownership from the current session, not a form or URL user ID.
- Keep admin provider settings behind `plugin.read` and `plugin.update` RBAC checks.
- Revoke sessions when an account is disabled or credentials are suspected to be compromised.
