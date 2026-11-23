# 发布到 GoPress 市场

GoPress 官方市场 [gopress.xyz/market](https://gopress.xyz/market) 收录[主题包](theme-packages.md)。每个 GoPress 站点都可以在后台「主题 → 主题市场」里浏览、安装和升级，不需要任何配置。

市场还处于早期：目前上架的是免费主题，后续版本计划支持付费主题和插件。

每个版本都走同一条流程：

1. 你用自己的密钥给主题包签名，然后上传。
2. 市场运行主题包检查，并验证你的签名。
3. 人工审核。
4. 市场用自己的密钥加签，然后发布。
5. 站点安装前会核对两个签名和签名目录，之后也会收到新版本提示。

本文介绍加入开发者计划、密钥、打包和签名、提交、审核，以及管理已发布的版本。最后一节说明市场服务端要提供什么，供自建市场参考。

> **市场主题就是主题包，所以目前能做的事比 Go 主题少。** 它们只能使用 core 提供的能力，一些功能（例如主题自己的评论表单或联系表单）暂时还用不了。我们会随着版本迭代逐步开放；主题包目前能做什么，见[和 Go 主题的区别](theme-packages.md#和-go-主题的区别)。

## 开始之前

- **一个能正常使用的主题包。** 按[主题包](theme-packages.md)的要求编写：页面 bundle、不含脚本、外部域名在 `[[external]]` 里声明。在开发站点上用 `gopress serve -theme-package ./my-theme` 预览，确认演示数据能把主题展示好。
- **`gopress` 命令行。** `gopress pkg` 系列命令不需要 GoPress 源码目录。
- **市场展示什么。** 市场从 `theme.toml` 取主题名称和描述，从主题包顶层的 `screenshot.png` 取截图。另外请附上 `README.md`，以及允许他人在自己站点上使用该主题的 `LICENSE`。字体、图片等素材必须是你有权分发的。

## 1. 加入开发者计划

1. 登录 [gopress.xyz](https://gopress.xyz)，打开 [gopress.xyz/market/join](https://gopress.xyz/market/join)。账户页也有入口。
2. 选一个 **handle**。它会出现在你的公开主页地址 `gopress.xyz/market/developers/<handle>` 里，之后不能修改。再填写显示名称、网站（选填），简单说明你打算发布什么。第一把签名公钥可以在这里登记（见第 2 步），也可以以后再登记。
3. 阅读并同意[开发者协议](https://gopress.xyz/developer-agreement)。

申请由人工审核，进度显示在你的账户页上。通过后，账户页会把你带到**开发者区** `gopress.xyz/market/developer`。暂停接受申请时，申请页会说明。

## 2. 生成并登记签名密钥

在你自己的电脑上生成密钥对：

```bash
gopress pkg keygen -out mykey
```

它会生成私钥 `mykey.key`（文件权限 `0600`）和公钥 `mykey.pub`，并打印密钥 ID。

- 在开发者区打开「签名公钥」，粘贴 `mykey.pub` 的内容。永远不要粘贴或上传 `.key` 文件，市场也永远不会索要它。
- 每个账号最多同时持有两把公钥，方便换用新密钥时不中断。新登记的公钥要过一段等待期才能使用（公钥页上会显示），每次有公钥加到你的账号上，你都会收到邮件。
- 妥善保管并备份私钥：拿到它的人就能以你的名义签名。私钥丢失或可能泄露时，在公钥页撤销对应的公钥，并告诉市场团队。已发布的版本不受影响；之后的新版本换另一把密钥签名。

## 3. 检查、打包、签名

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

- `check` 运行和市场相同的检查。加 `-core` 时，还会检查 `theme.toml` 里的 `[requires] core` 是否支持这个 GoPress 版本。
- `build` 生成可复现的压缩包 `<slug>-<version>.zip`。
- `sign` 把 `signatures/publisher.json` 写进压缩包，不改变内容摘要。
- `verify` 打印摘要并验证签名。

先打包，再给压缩包签名。用你登记过、已过等待期的密钥签名。全部参数见[命令行](theme-packages.md#命令行)。

**版本号。** `theme.toml` 里的版本必须是严格的语义化版本，例如 `1.2.0` 或 `1.3.0-beta.1`。同一个主题的每个版本号只能用一次，并且必须高于你为它提交过的所有版本，包括已撤回和未通过的。

## 4. 提交

在开发者区打开「提交主题」，上传签好名的 `.zip`。市场会：

- 运行主题包检查（和 `gopress pkg check` 相同），有问题时按[规则编号](theme-packages.md#规则编号)逐条列出；
- 验证主题包是用你的某把可用公钥签名的；
- 检查版本号是新的，并且高于之前的版本；
- 某个 slug 第一次上传通过时，创建这个主题，这个 slug 从此归你。

通过检查的主题包进入审核队列，在此之前不会发布任何内容。同一主题再上传更新的版本，会替换队列里等待的那个。

## 5. 审核

每个版本都由人工审核。审核时会：

- 用演示数据预览，确认页面显示正常；
- 确认字体、图片等素材可以使用和分发；
- 检查有没有未披露的追踪或数据收集；
- 确认名称、描述和截图与主题一致；
- 对照[开发者协议](https://gopress.xyz/developer-agreement)检查。

结果有三种：**通过**、**要求修改**（改好后提交更高的版本）、**未通过**。后两种都会告诉你原因。

| 开发者区里的状态 | 含义 |
|---|---|
| 等待审核 | 通过了自动检查，在审核队列里。 |
| 要求修改 | 按审核意见修改，再提交更高的版本。 |
| 未通过 | 这个版本不会发布。 |
| 已通过，等待发布 | 审核通过。加签之前你仍然可以撤回。 |
| 已加签，等待发布 | 市场已经加签，即将发布。 |
| 已发布 | 已在市场上，站点可以安装。 |
| 已被替换 | 审核前被更新的上传替换了。 |
| 已撤回 | 你在加签前撤回了它。 |
| 已下线 | 发布后你把它撤出了市场。 |
| 已撤销 | 市场撤销了它。 |
| 密钥已撤销 | 签名用的密钥在发布前被撤销了。请换另一把密钥签一个新版本。 |

## 6. 发布之后

市场为通过审核的版本加签（`signatures/market.json`）并发布。之后：

- 它会出现在各 GoPress 站点的「主题 → 主题市场」里，站点会安装 `[requires] core` 满足自身 GoPress 版本的最新版本。它在市场上的页面是 `gopress.xyz/market/<slug>`，你的开发者主页也会列出它；
- 安装过旧版本、打开了更新检查的站点，大约一天内会显示「有新版本」，并从市场页升级。

如果某个版本修复了安全问题，提交时请发邮件到 contact@gopress.xyz 告诉市场团队。这个版本会被标为安全更新，站点会用红色提示。

## 7. 管理已发布的版本

| 操作 | 谁来做 | 效果 |
|---|---|---|
| 撤回版本 | 你，在加签之前 | 不会发布。这个版本号不能再用。 |
| 把版本撤出市场 | 你，在发布之后 | 从目录中移除。已安装的站点继续保留；有更高版本后，更新检查会把它标为不再支持。不能恢复，请改为发布新版本。 |
| 把主题撤出市场 | 你 | 主题从市场下架。已安装的站点继续保留，仍然可以下载它已发布的版本。以后可以重新上架。 |
| 撤销版本或主题 | 市场 | 用于严重的安全问题或违反开发者协议的情况。站点会把该版本标为「已撤销」：不能安装、不能切换过去、不能回滚过去；仍在使用它的站点，后台会一直提醒管理员，直到换掉或升级。由市场下架的主题只能由市场重新上架。 |

已发布的版本会一直可以下载，方便站点重装和回滚。

## 自建市场

站点在 [`[market]`](../getting-started/configuration.md#market) 里设置 `endpoint`、`catalog_keys` 和 `package_keys` 后，就改用自建市场。自建市场要提供以下三样。

### 签名目录

`GET <endpoint>/catalog?schema=1` 返回一个信封：

```json
{
  "format": "gopress-catalog-v1",
  "payload": "<目录 JSON 的 base64>",
  "signatures": [{ "kid": "…", "alg": "ed25519", "sig": "…" }]
}
```

先写好目录 JSON，再用 `gopress pkg catalog sign` 封装。签名针对原样发送的 payload 字节，所以 JSON 不需要规范化。可以用 1 到 4 把密钥签名，例如轮换期间新旧两把一起签：

```bash
gopress pkg catalog sign -key catalog.key -out catalog.json payload.json
```

按站点的方式验证：

```bash
gopress pkg catalog verify -catalog-key "$(cat catalog.pub)" -package-key "$(cat market.pub)" -endpoint https://market.example.com/api/market/v1 catalog.json
```

目录 JSON：

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

- **`sequence`** 是正整数，每生成一份新目录就要增大。站点见过某个序号后，会拒绝序号更小的目录，也会拒绝同一序号但内容不同的目录。
- **`generated_at`**、**`expires_at`** 是 RFC 3339 时间。站点拒绝已过期的目录，所以要在过期前很早就重新签名，例如每天签一次、有效期几天。
- 上限：条目和撤销各最多 5000 条，payload 最大 5 MB，信封最大 7 MB。

每个条目描述一个版本：

| 字段 | 规则 |
|---|---|
| `kind`、`slug`、`version` | `"theme"`、主题包的 slug 和严格语义化版本号。同一个 kind、slug、version 只能出现一次。其他 kind 的条目会被跳过。 |
| `digest`、`size` | 内容摘要（`gopress pkg digest`，`h1:…`）和 `.zip` 的字节数（最大 10 MB）。 |
| `download_url` | `.zip` 的 HTTPS 地址。 |
| `core` | 选填：主题包的 `[requires] core` 约束。站点只安装自身版本满足约束的版本。 |
| `requires` | 选填：最多 20 个插件依赖，`{ "slug", "version", "optional" }`。 |
| `pricing` | `"free"`。当前版本只安装免费主题包，付费主题包在计划中。 |
| `title`、`summary`、`author` | 显示文字：标题必填，最多 100 个字符；简介最多 500；作者最多 100。 |
| `localized` | 选填：最多 20 种语言，例如 `{ "zh-CN": { "title": "…", "summary": "…" } }`。 |
| `listing_url`、`screenshot_url` | 选填：主题介绍页和截图的 HTTPS 地址。 |
| `external` | 主题包在 `[[external]]` 里声明的域名（最多 20 个）。站点会拿下载的包和它们核对。 |
| `publisher` | 签名密钥的 `{ "kid", "public_key" }`。 |
| `signatures` | `{ "publisher": …, "market": … }`，取自主题包里的 `signatures/publisher.json` 和 `signatures/market.json`。 |

签名无法用发布者公钥和站点 `package_keys` 中的某一把验证通过的条目，站点会隐藏。

每条撤销记录会让站点移除对应版本：

```json
{
  "kind": "theme",
  "slug": "aurora",
  "versions": ["1.2.0"],
  "severity": "high",
  "reason": "vulnerability",
  "note": "已在 1.2.1 修复。"
}
```

`versions` 列出版本号，或者写 `["*"]` 表示全部版本。`severity` 取 `critical`、`high`、`medium` 或 `low`；`reason` 取 `malware`、`vulnerability`、`infringement` 或 `other`；`note` 选填，最多 1000 个字符。不认识的取值按 `high` 和 `other` 处理，所以撤销记录永远不会被忽略。

### 主题包下载

每个 `.zip` 放在它的 `download_url`，使用 HTTPS、不跳转，大小必须与目录记录完全一致。上架前先用 `gopress pkg check` 和 `gopress pkg verify` 检查，再用市场包密钥加签：

```bash
gopress pkg sign -role market -key market.key aurora-1.2.0.zip
```

加签要求主题包已有有效的发布者签名。市场包密钥和目录密钥要分开：同一把密钥同时出现在两组里，站点会拒绝。

### 更新检查

`POST <endpoint>/updates/check` 告诉站点它从你的市场安装的主题有没有新版本，请求和响应格式见[配置文件](../getting-started/configuration.md#market)。没有这个接口的市场不会显示更新提示，安装和升级照常可用。
