# Publishing to the GoPress Market

The official GoPress market at [gopress.xyz/market](https://gopress.xyz/market) lists [theme packages](theme-packages.md). Every GoPress site can browse, install and upgrade them from **Themes → Theme market** in its admin, with no configuration.

The market is young: today it carries free themes, and support for paid themes and plugins is planned for later releases.

Every version goes through the same path:

1. You sign the package with your own key and upload it.
2. The market runs the package checks and verifies your signature.
3. People review it.
4. The market countersigns it with its own key and publishes it.
5. Sites check both signatures and the signed catalog before installing, and are told about new versions.

This page covers joining, keys, building and signing, submitting, review, and managing published versions. The last section describes what a market server must serve, for anyone running their own market.

> **Market themes are theme packages, so for now they can do less than Go themes.** They can only use what core provides, and some features, such as their own comment or contact forms, are not available to them yet. We open more with each release; see [Go themes and theme packages](theme-packages.md#go-themes-and-theme-packages) for what packages can do today.

## Before you start

- **A working theme package.** Follow [Theme Packages](theme-packages.md): page bundles, no scripts, external hosts declared in `[[external]]`. Preview it on a development site with `gopress serve -theme-package ./my-theme` and make sure its demo data shows the theme well.
- **The `gopress` command line.** The `gopress pkg` commands work outside a GoPress checkout.
- **What the listing shows.** The market takes the theme's name and description from `theme.toml` and its picture from `screenshot.png` at the top level of the package. Add a `README.md`, and a `LICENSE` that lets people use the theme on their sites. Only use fonts, images and other assets you may distribute.

## 1. Join the developer program

1. Sign in at [gopress.xyz](https://gopress.xyz) and open [gopress.xyz/market/join](https://gopress.xyz/market/join). Your account page links to it.
2. Choose a **handle**. It appears in your public page, `gopress.xyz/market/developers/<handle>`, and cannot be changed later. Add a display name, an optional website and a short note about what you plan to publish. You can register your first signing key here (step 2) or later.
3. Read and accept the [Developer Agreement](https://gopress.xyz/developer-agreement).

Applications are reviewed by people. Your account page shows the status. When you are approved, it leads to your **developer area** at `gopress.xyz/market/developer`. When applications are closed, the join page says so.

## 2. Create and register a signing key

Create a key pair on your own computer:

```bash
gopress pkg keygen -out mykey
```

This writes `mykey.key`, the private key (file mode `0600`), and `mykey.pub`, the public key, and prints the key ID.

- In the developer area, open **Signing keys** and paste the contents of `mykey.pub`. Never paste or upload the `.key` file. The market never asks for it.
- You can hold up to two keys at a time, so you can move to a new key without a gap. A new key becomes usable after a waiting period, which the keys page shows, and you get an email whenever a key is added to your account.
- Keep the private key safe and backed up: whoever has it can sign packages as you. If it is lost or may have leaked, revoke it on the keys page and tell the market team. Versions that are already published stay published; sign new versions with another key.

## 3. Check, build and sign

```bash
gopress pkg check -core 0.7.44 ./my-theme
```

```bash
gopress pkg build ./my-theme
```

```bash
gopress pkg sign -key mykey.key my-theme-1.0.0.zip
```

```bash
gopress pkg verify my-theme-1.0.0.zip
```

- `check` runs the same checks as the market. `-core` also checks that `[requires] core` in `theme.toml` supports that GoPress version.
- `build` writes the reproducible archive `<slug>-<version>.zip`.
- `sign` adds `signatures/publisher.json` to the archive without changing its content digest.
- `verify` prints the digest and checks the signature.

Build first, then sign the archive. Use the key you registered and that is past its waiting period. See [Command line](theme-packages.md#command-line) for every option.

**Versions.** The version in `theme.toml` must be a strict semantic version such as `1.2.0` or `1.3.0-beta.1`. Each version number of a theme can be used once and must be higher than every version you have submitted for it, including withdrawn and rejected ones.

## 4. Submit

In the developer area, open **Submit a theme** and upload the signed `.zip`. The market then:

- runs the package checks, the same as `gopress pkg check`, and lists any problem with its [rule ID](theme-packages.md#rule-ids);
- verifies that the package is signed with one of your usable keys;
- checks that the version is new and higher than earlier ones;
- on the first accepted upload of a slug, creates the theme and makes that slug yours.

A package that passes waits for review. Nothing is published before that. Uploading a newer version of the same theme replaces the one waiting in the queue.

## 5. Review

People review every version. They:

- preview it with its demo data and check that the pages display correctly;
- check that its fonts, images and other assets may be used and distributed;
- look for tracking or data collection that is not disclosed;
- check that the name, description and screenshot match the theme;
- check it against the [Developer Agreement](https://gopress.xyz/developer-agreement).

The result is one of three: **approved**, **changes requested** (fix the problems and submit a higher version), or **not accepted**. For the last two you always get the reason.

| Status in your developer area | Meaning |
|---|---|
| Waiting for review | Passed the automatic checks; in the review queue. |
| Changes requested | Fix what the reviewer asked for and submit a higher version. |
| Not accepted | The version will not be published. |
| Approved, waiting to be published | Passed review. You can still withdraw it until it is countersigned. |
| Countersigned, waiting to be published | The market has signed it and will publish it. |
| Published | In the market; sites can install it. |
| Replaced | A newer upload took its place before review. |
| Withdrawn | You withdrew it before it was countersigned. |
| Yanked | You took it out of the market after publishing. |
| Revoked | The market revoked it. |
| Key revoked | The key that signed it was revoked before it was published. Sign a new version with another key. |

## 6. Published

The market countersigns an approved version (`signatures/market.json`) and publishes it. Then:

- it appears in **Themes → Theme market** on GoPress sites, which install the newest version whose `[requires] core` their GoPress meets. Its page on the market is `gopress.xyz/market/<slug>`, and your developer page lists it;
- sites that installed an earlier version and have update checks on show **New version** within about a day, and upgrade from the market page.

If a version fixes a security problem, tell the market team at contact@gopress.xyz when you submit it. It is then marked as a security update, which sites show in red.

## 7. Managing published versions

| Action | Who | Effect |
|---|---|---|
| Withdraw a version | You, before it is countersigned | It will not be published. Its version number cannot be used again. |
| Take a version out of the market | You, after it is published | It leaves the catalog. Sites that installed it keep it, and update checks call it unsupported once a higher version exists. This cannot be undone; publish a new version instead. |
| Take a theme out of the market | You | The theme leaves the market. Sites that installed it keep it and can still download its published versions. You can list it again later. |
| Revoke a version or a theme | The market | For serious security problems or violations of the Developer Agreement. Sites mark the version *Revoked*: it cannot be installed, switched to or rolled back to, and sites still using it show a warning to their admins until they switch or upgrade. A theme the market took out of the market can only be listed again by the market. |

Published versions stay downloadable, so sites can reinstall them and roll back.

## Running your own market

A site can use a self-hosted market instead of the official one by setting `endpoint`, `catalog_keys` and `package_keys` in [`[market]`](../getting-started/configuration.md#market). Such a market serves three things.

### Signed catalog

`GET <endpoint>/catalog?schema=1` answers with an envelope:

```json
{
  "format": "gopress-catalog-v1",
  "payload": "<base64 of the catalog JSON>",
  "signatures": [{ "kid": "…", "alg": "ed25519", "sig": "…" }]
}
```

Write the catalog JSON and wrap it with `gopress pkg catalog sign`. The signatures cover the payload bytes exactly as sent, so the JSON never needs a canonical form. Sign with one to four keys, for example the old and the new key while you rotate:

```bash
gopress pkg catalog sign -key catalog.key -out catalog.json payload.json
```

Check it the way a site will:

```bash
gopress pkg catalog verify -catalog-key "$(cat catalog.pub)" -package-key "$(cat market.pub)" -endpoint https://market.example.com/api/market/v1 catalog.json
```

The catalog JSON:

```json
{
  "schema": 1,
  "sequence": 42,
  "generated_at": "2026-11-22T08:00:00Z",
  "expires_at": "2026-11-29T08:00:00Z",
  "items": [ … ],
  "revoked": [ … ]
}
```

- **`sequence`** is a positive number that grows with every new catalog. A site refuses a catalog with a lower sequence than one it has seen, and refuses the same sequence with different content.
- **`generated_at`** and **`expires_at`** are RFC 3339 times. Sites refuse an expired catalog, so sign a new one well before it expires, for example every day with a validity of a few days.
- Limits: up to 5,000 items and 5,000 revocations, a payload of at most 5 MB and an envelope of at most 7 MB.

Each item describes one version:

| Field | Rules |
|---|---|
| `kind`, `slug`, `version` | `"theme"`, the package slug and its strict semantic version. Each kind, slug and version appears once. Items of other kinds are skipped. |
| `digest`, `size` | The content digest (`gopress pkg digest`, `h1:…`) and the size in bytes of the `.zip` (at most 10 MB). |
| `download_url` | HTTPS URL of the `.zip`. |
| `core` | Optional: the package's `[requires] core` constraint. Sites only install versions whose constraint their GoPress meets. |
| `requires` | Optional: up to 20 plugin requirements, `{ "slug", "version", "optional" }`. |
| `pricing` | `"free"`. This version installs free packages only; paid packages are planned. |
| `title`, `summary`, `author` | Display text: title required, at most 100 characters; summary at most 500; author at most 100. |
| `localized` | Optional: up to 20 languages, for example `{ "zh-CN": { "title": "…", "summary": "…" } }`. |
| `listing_url`, `screenshot_url` | Optional HTTPS URLs of the theme's page and picture. |
| `external` | The hosts the package declares in `[[external]]` (up to 20). Sites check the download against them. |
| `publisher` | `{ "kid", "public_key" }` of the key that signed the package. |
| `signatures` | `{ "publisher": …, "market": … }`, the signatures from the package's `signatures/publisher.json` and `signatures/market.json`. |

Sites hide items whose signatures do not verify with the publisher key and one of the site's `package_keys`.

Each revocation removes versions from sites:

```json
{
  "kind": "theme",
  "slug": "aurora",
  "versions": ["1.2.0"],
  "severity": "high",
  "reason": "vulnerability",
  "note": "Fixed in 1.2.1."
}
```

`versions` lists versions or is `["*"]` for all of them. `severity` is `critical`, `high`, `medium` or `low`; `reason` is `malware`, `vulnerability`, `infringement` or `other`; `note` is optional, at most 1,000 characters. Unknown values count as `high` and `other`, so a revocation is never ignored.

### Packages

Serve each `.zip` at its `download_url` over HTTPS, without redirects, at exactly the size in the catalog. Before listing a package, check it with `gopress pkg check` and `gopress pkg verify`, then countersign it with a package key:

```bash
gopress pkg sign -role market -key market.key aurora-1.2.0.zip
```

A countersignature requires a valid publisher signature. Keep package keys and catalog keys separate: a site refuses a key listed in both.

### Update checks

`POST <endpoint>/updates/check` tells sites about new versions of the themes they installed from your market. The request and response are described in [Configuration](../getting-started/configuration.md#market). A market without this endpoint shows no update notices; installing and upgrading still work.
