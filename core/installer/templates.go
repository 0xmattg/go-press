package installer

const layoutTemplate = `
{{define "layout"}}
<!DOCTYPE html>
<html lang="{{.Lang}}">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<title>{{.Title}}</title>
	<style>
		:root {
			--primary: #2563eb;
			--primary-hover: #1d4ed8;
			--danger: #dc2626;
			--danger-soft: #fee2e2;
			--success: #15803d;
			--success-soft: #dcfce7;
			--info: #0f766e;
			--info-soft: #ccfbf1;
			--bg: #f1f5f9;
			--card-bg: #ffffff;
			--sidebar-bg: #1e293b;
			--text: #1e293b;
			--text-muted: #64748b;
			--border: #e2e8f0;
			--radius: 6px;
		}
		*, *::before, *::after {
			box-sizing: border-box;
		}
		body {
			margin: 0;
			font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
			font-size: 14px;
			background: var(--sidebar-bg);
			color: var(--text);
		}
		a {
			color: var(--primary);
			text-decoration: none;
		}
		code {
			font-family: ui-monospace, SFMono-Regular, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
		}
		.install-shell {
			min-height: 100vh;
			display: flex;
			align-items: center;
			justify-content: center;
			padding: 32px 16px;
		}
		.install-card {
			width: 100%;
			max-width: 760px;
			background: var(--card-bg);
			border-radius: var(--radius);
			box-shadow: 0 10px 32px rgba(15, 23, 42, .24);
			overflow: hidden;
		}
		.install-header {
			padding: 32px 40px 24px;
			text-align: center;
			border-bottom: 1px solid var(--border);
			background: var(--card-bg);
		}
		.install-brand {
			margin: 0 0 8px;
			font-size: 2rem;
			font-weight: 700;
		}
		.install-subtitle {
			margin: 0 0 20px;
			color: var(--text-muted);
			font-size: 1rem;
		}
		.page-title {
			margin: 0 0 8px;
			font-size: 1.5rem;
			font-weight: 600;
		}
		.page-lead {
			margin: 0 auto;
			max-width: 560px;
			color: var(--text-muted);
			line-height: 1.7;
		}
		.config-path {
			margin-top: 16px;
			font-size: .8rem;
			color: var(--text-muted);
		}
		.config-path code {
			display: inline-block;
			padding: 3px 6px;
			border-radius: 4px;
			background: var(--bg);
			color: var(--text);
		}
		.install-steps {
			display: grid;
			grid-template-columns: repeat(3, 1fr);
			gap: 12px;
			padding: 20px 40px 0;
		}
		.install-step {
			display: flex;
			gap: 12px;
			align-items: flex-start;
			padding: 14px 16px;
			border: 1px solid var(--border);
			border-radius: var(--radius);
			background: var(--bg);
		}
		.install-step.is-current {
			border-color: var(--primary);
			background: rgba(37, 99, 235, .08);
		}
		.install-step.is-done {
			border-color: rgba(37, 99, 235, .22);
		}
		.step-index {
			width: 24px;
			height: 24px;
			border-radius: 999px;
			display: inline-flex;
			align-items: center;
			justify-content: center;
			background: #cbd5e1;
			color: #0f172a;
			font-size: .75rem;
			font-weight: 700;
			flex: 0 0 auto;
		}
		.install-step.is-current .step-index,
		.install-step.is-done .step-index {
			background: var(--primary);
			color: #fff;
		}
		.step-title {
			margin: 0 0 4px;
			font-size: .9rem;
			font-weight: 600;
		}
		.step-copy {
			margin: 0;
			color: var(--text-muted);
			font-size: .78rem;
			line-height: 1.55;
		}
		.install-content {
			padding: 28px 40px 40px;
		}
		.alert {
			padding: .75rem 1rem;
			margin-bottom: 1rem;
			border-radius: var(--radius);
			font-size: .875rem;
			line-height: 1.6;
		}
		.alert-info {
			background: var(--info-soft);
			color: var(--info);
		}
		.alert-error {
			background: var(--danger-soft);
			color: #991b1b;
		}
		.alert-success {
			background: var(--success-soft);
			color: var(--success);
		}
		.intro-copy,
		.summary {
			margin: 0 0 1rem;
			color: var(--text-muted);
			line-height: 1.8;
		}
		.feature-list {
			margin: 0;
			padding-left: 1.2rem;
			color: var(--text-muted);
		}
		.feature-list li {
			line-height: 1.8;
			margin-bottom: .35rem;
		}
		.form-grid {
			display: grid;
			grid-template-columns: repeat(2, minmax(0, 1fr));
			gap: 1rem;
		}
		.form-group {
			margin-bottom: 0;
		}
		.form-group.full {
			grid-column: 1 / -1;
		}
		.form-field-stack {
			display: flex;
			flex-direction: column;
			gap: 1rem;
		}
		.form-group label {
			display: block;
			font-size: .8rem;
			font-weight: 600;
			margin-bottom: .35rem;
		}
		.form-label-row {
			display: flex;
			align-items: center;
			gap: .4rem;
			margin-bottom: .35rem;
		}
		.form-label-row label {
			margin-bottom: 0;
		}
		.info-popover {
			position: relative;
			display: inline-flex;
			align-items: center;
		}
		.info-trigger {
			width: 18px;
			height: 18px;
			border: 1px solid #cbd5e1;
			border-radius: 999px;
			background: #f8fafc;
			color: var(--text-muted);
			font-size: .72rem;
			font-weight: 700;
			line-height: 1;
			cursor: help;
		}
		.info-trigger:hover,
		.info-trigger:focus {
			border-color: var(--primary);
			color: var(--primary);
			outline: none;
			box-shadow: 0 0 0 3px rgba(37, 99, 235, .1);
		}
		.info-bubble {
			position: absolute;
			left: 50%;
			bottom: calc(100% + 8px);
			z-index: 10;
			width: min(280px, 70vw);
			padding: .55rem .65rem;
			border: 1px solid var(--border);
			border-radius: var(--radius);
			background: #0f172a;
			color: #fff;
			font-size: .75rem;
			font-weight: 500;
			line-height: 1.55;
			box-shadow: 0 10px 24px rgba(15, 23, 42, .18);
			opacity: 0;
			pointer-events: none;
			transform: translate(-50%, 4px);
			transition: opacity .15s, transform .15s;
		}
		.info-bubble::after {
			content: "";
			position: absolute;
			left: 50%;
			top: 100%;
			border-width: 6px 6px 0;
			border-style: solid;
			border-color: #0f172a transparent transparent;
			transform: translateX(-50%);
		}
		.info-popover:hover .info-bubble,
		.info-trigger:focus + .info-bubble {
			opacity: 1;
			transform: translate(-50%, 0);
		}
		.form-group input,
		.form-group select {
			width: 100%;
			padding: .5rem .75rem;
			border: 1px solid var(--border);
			border-radius: var(--radius);
			font-size: .875rem;
			background: #fff;
			transition: border-color .15s, box-shadow .15s;
		}
		.form-group input:focus,
		.form-group select:focus {
			outline: none;
			border-color: var(--primary);
			box-shadow: 0 0 0 3px rgba(37, 99, 235, .1);
		}
		.form-help {
			display: block;
			margin-top: .35rem;
			font-size: .75rem;
			color: var(--text-muted);
			line-height: 1.6;
		}
		.form-actions {
			display: flex;
			flex-wrap: wrap;
			gap: .75rem;
			margin-top: 1.5rem;
		}
		.form-actions.center {
			justify-content: center;
		}
		.language-options {
			display: grid;
			grid-template-columns: repeat(2, minmax(0, 1fr));
			gap: 12px;
			margin-top: 1rem;
		}
		.language-option input {
			position: absolute;
			opacity: 0;
			pointer-events: none;
		}
		.language-card {
			display: block;
			padding: 16px;
			border: 1px solid var(--border);
			border-radius: var(--radius);
			background: #fff;
			cursor: pointer;
			transition: border-color .15s, box-shadow .15s, background .15s;
		}
		.language-card strong {
			display: block;
			margin-bottom: 4px;
			font-size: .98rem;
		}
		.language-card span {
			color: var(--text-muted);
			font-size: .8rem;
		}
		.language-option input:checked + .language-card {
			border-color: var(--primary);
			background: rgba(37, 99, 235, .06);
			box-shadow: 0 0 0 3px rgba(37, 99, 235, .1);
		}
		.btn {
			display: inline-flex;
			align-items: center;
			justify-content: center;
			padding: .55rem 1rem;
			border: 1px solid var(--border);
			border-radius: var(--radius);
			background: var(--card-bg);
			color: var(--text);
			font-size: .875rem;
			font-weight: 600;
			cursor: pointer;
			text-decoration: none;
			transition: background .15s, border-color .15s, color .15s;
		}
		.btn:hover {
			background: var(--bg);
		}
		.btn:disabled,
		.btn.is-disabled {
			background: #cbd5e1;
			border-color: #cbd5e1;
			color: #64748b;
			cursor: not-allowed;
		}
		.btn-primary {
			background: var(--primary);
			border-color: var(--primary);
			color: #fff;
		}
		.btn-primary:hover {
			background: var(--primary-hover);
			border-color: var(--primary-hover);
		}
		.btn-primary:disabled,
		.btn-primary.is-disabled {
			background: #cbd5e1;
			border-color: #cbd5e1;
			color: #64748b;
		}
		.btn-block {
			width: 100%;
		}
		.database-feedback[hidden] {
			display: none;
		}
		@media (max-width: 768px) {
			.install-shell {
				padding: 20px 12px;
			}
			.install-header,
			.install-content,
			.install-steps {
				padding-left: 20px;
				padding-right: 20px;
			}
			.install-steps,
			.form-grid,
			.language-options {
				grid-template-columns: 1fr;
			}
			.form-actions .btn {
				width: 100%;
			}
		}
	</style>
</head>
<body>
	<div class="install-shell">
		<div class="install-card">
			<div class="install-header">
				<h1 class="install-brand">GoPress</h1>
				<p class="install-subtitle">{{T .Lang "brand.subtitle"}}</p>
				<h2 class="page-title">{{.Heading}}</h2>
				<p class="page-lead">{{.Lead}}</p>
				{{if ne .Page "welcome"}}<p class="config-path">{{T .Lang "config.path"}} <code>{{.ConfigPath}}</code></p>{{end}}
			</div>

			{{if ne .Page "welcome"}}
			<div class="install-steps">
				<div class="install-step {{if ge .Step 1}}{{if eq .Step 1}}is-current{{else}}is-done{{end}}{{end}}">
					<span class="step-index">1</span>
					<div>
						<p class="step-title">{{T .Lang "step.database.title"}}</p>
						<p class="step-copy">{{T .Lang "step.database.copy"}}</p>
					</div>
				</div>
				<div class="install-step {{if ge .Step 2}}{{if eq .Step 2}}is-current{{else}}is-done{{end}}{{end}}">
					<span class="step-index">2</span>
					<div>
						<p class="step-title">{{T .Lang "step.site.title"}}</p>
						<p class="step-copy">{{T .Lang "step.site.copy"}}</p>
					</div>
				</div>
				<div class="install-step {{if ge .Step 3}}{{if eq .Step 3}}is-current{{else}}is-done{{end}}{{end}}">
					<span class="step-index">3</span>
					<div>
						<p class="step-title">{{T .Lang "step.success.title"}}</p>
						<p class="step-copy">{{T .Lang "step.success.copy"}}</p>
					</div>
				</div>
			</div>
			{{end}}

			<div class="install-content">
				{{if .BootError}}<div class="alert alert-info">{{.BootError}}</div>{{end}}
				{{if .Error}}<div class="alert alert-error">{{.Error}}</div>{{end}}
				{{if eq .Page "welcome"}}{{template "welcome" .}}{{end}}
				{{if eq .Page "database"}}{{template "database" .}}{{end}}
				{{if eq .Page "site"}}{{template "site" .}}{{end}}
				{{if eq .Page "success"}}{{template "success" .}}{{end}}
			</div>
		</div>
	</div>
	{{if eq .Page "database"}}
	<script>
	(function () {
		const form = document.getElementById("database-form");
		if (!form) return;

		const testButton = document.getElementById("database-test-button");
		const continueButton = document.getElementById("database-continue-button");
		const feedback = document.getElementById("database-test-feedback");
		const fields = form.querySelectorAll("input, select");

		const setFeedback = (kind, message) => {
			feedback.hidden = false;
			feedback.textContent = message;
			feedback.className = "alert database-feedback " + (kind === "success" ? "alert-success" : "alert-error");
		};

		const disableContinue = () => {
			continueButton.disabled = true;
			continueButton.classList.add("is-disabled");
		};

		const enableContinue = () => {
			continueButton.disabled = false;
			continueButton.classList.remove("is-disabled");
		};

		const resetTestState = () => {
			feedback.hidden = true;
			feedback.textContent = "";
			feedback.className = "alert database-feedback";
			disableContinue();
		};

		fields.forEach((field) => {
			field.addEventListener("input", resetTestState);
			field.addEventListener("change", resetTestState);
		});

		form.addEventListener("submit", (event) => {
			if (continueButton.disabled) {
				event.preventDefault();
			}
		});

		testButton.addEventListener("click", async () => {
			testButton.disabled = true;
			testButton.textContent = "{{T .Lang "database.testing"}}";

			try {
				const response = await fetch("/install/database/test", {
					method: "POST",
					body: new FormData(form),
					headers: { "X-Requested-With": "XMLHttpRequest" }
				});

				const payload = await response.json();
				if (response.ok && payload.ok) {
					setFeedback("success", payload.message || "{{T .Lang "database.test_success"}}");
					enableContinue();
					return;
				}

				setFeedback("error", payload.error || "{{T .Lang "database.test_failed"}}");
				disableContinue();
			} catch (error) {
				setFeedback("error", "{{T .Lang "database.test_failed_network"}}");
				disableContinue();
			} finally {
				testButton.disabled = false;
				testButton.textContent = "{{T .Lang "database.test"}}";
			}
		});

		resetTestState();
	})();
	</script>
	{{end}}
</body>
</html>
{{end}}
`

const welcomeTemplate = `
{{define "welcome"}}
<form method="post" action="/install/language">
	<div class="form-group">
		<label>{{T .Lang "welcome.language_label"}}</label>
		<div class="language-options">
			<label class="language-option">
				<input type="radio" name="language" value="en" {{if eq .Lang "en"}}checked{{end}}>
				<span class="language-card">
					<strong>English</strong>
					<span>{{T .Lang "welcome.option_en_help"}}</span>
				</span>
			</label>
			<label class="language-option">
				<input type="radio" name="language" value="zh-CN" {{if eq .Lang "zh-CN"}}checked{{end}}>
				<span class="language-card">
					<strong>简体中文</strong>
					<span>{{T .Lang "welcome.option_zh_help"}}</span>
				</span>
			</label>
		</div>
		<small class="form-help">{{T .Lang "welcome.language_help"}}</small>
	</div>
	<div class="form-actions center">
		<button class="btn btn-primary" type="submit">{{T .Lang "welcome.start"}}</button>
	</div>
</form>
{{end}}
`

const databaseTemplate = `
{{define "database"}}
<form id="database-form" method="post" action="/install/database">
	<div id="database-test-feedback" class="alert database-feedback" hidden></div>
	<p class="intro-copy">{{T .Lang "database.intro"}}</p>
	<ul class="feature-list" style="margin-bottom:1rem;">
		<li>{{T .Lang "database.note_create"}}</li>
		<li>{{T .Lang "database.note_switch"}}</li>
	</ul>
	<div class="form-grid">
		<div class="form-group full">
			<label for="database">{{T .Lang "database.name"}}</label>
			<input id="database" name="database" type="text" value="{{.Database.Database}}" required>
			<small class="form-help">{{T .Lang "database.name_help"}}</small>
		</div>
		<div class="form-group">
			<label for="user">{{T .Lang "database.user"}}</label>
			<input id="user" name="user" type="text" value="{{.Database.User}}" required>
		</div>
		<div class="form-group">
			<label for="password">{{T .Lang "database.password"}}</label>
			<input id="password" name="password" type="password" value="{{.Database.Password}}" required>
		</div>
		<div class="form-group">
			<label for="host">{{T .Lang "database.host"}}</label>
			<input id="host" name="host" type="text" value="{{.Database.Hostname}}" required>
		</div>
		<div class="form-group">
			<label for="port">{{T .Lang "database.port"}}</label>
			<input id="port" name="port" type="text" value="{{.Database.Port}}" required>
		</div>
		<div class="form-field-stack">
			<div class="form-group">
				<label for="schema">{{T .Lang "database.schema"}}</label>
				<input id="schema" name="schema" type="text" value="{{.Database.Schema}}" required>
			</div>
			<div class="form-group">
				<label for="server_port">{{T .Lang "database.server_port"}}</label>
				<input id="server_port" name="server_port" type="number" min="1" max="65535" value="{{.Database.ServerPort}}" required>
				<small class="form-help">{{T .Lang "database.server_port_help"}}</small>
			</div>
		</div>
		<div class="form-group">
			<label for="table_prefix">{{T .Lang "database.table_prefix"}}</label>
			<input id="table_prefix" name="table_prefix" type="text" value="{{.Database.TablePrefix}}" placeholder="gp_">
			<small class="form-help">{{T .Lang "database.table_prefix_help"}}</small>
		</div>
	</div>
	<div class="form-actions">
		<button class="btn btn-primary" id="database-test-button" type="button">{{T .Lang "database.test"}}</button>
		<button class="btn btn-primary is-disabled" id="database-continue-button" type="submit" disabled>{{T .Lang "database.continue"}}</button>
		<a class="btn" href="/install">{{T .Lang "database.back"}}</a>
	</div>
</form>
{{end}}
`

const siteTemplate = `
{{define "site"}}
<form method="post" action="/install/site">
	<input type="hidden" name="admin_language" value="{{.Site.AdminLanguage}}">
	<div class="form-grid">
		<div class="form-group full">
			<label for="site_name">{{T .Lang "site.name"}}</label>
			<input id="site_name" name="site_name" type="text" value="{{.Site.SiteName}}" required>
		</div>
		<div class="form-group full">
			<label for="site_url">{{T .Lang "site.url"}}</label>
			<input id="site_url" name="site_url" type="url" value="{{.Site.SiteURL}}" required>
			<small class="form-help">{{T .Lang "site.url_help"}}</small>
		</div>
		<div class="form-group full">
			<label for="tagline">{{T .Lang "site.tagline"}}</label>
			<input id="tagline" name="tagline" type="text" value="{{.Site.Tagline}}">
		</div>
		<div class="form-group">
			<label for="language">{{T .Lang "site.language"}}</label>
			<select id="language" name="language">
				<option value="zh-CN" {{if eq .Site.Language "zh-CN"}}selected{{end}}>简体中文</option>
				<option value="en" {{if eq .Site.Language "en"}}selected{{end}}>English</option>
			</select>
		</div>
		<div class="form-group">
			<div class="form-label-row">
				<label for="timezone">{{T .Lang "site.timezone"}}</label>
				<span class="info-popover">
					<button class="info-trigger" type="button" aria-label="{{T .Lang "site.timezone_help"}}">i</button>
					<span class="info-bubble" role="tooltip">{{T .Lang "site.timezone_help"}}</span>
				</span>
			</div>
			<select id="timezone" name="timezone">
				<option value="Local" {{if eq .Site.Timezone "Local"}}selected{{end}}>Server local time</option>
				<option value="UTC" {{if eq .Site.Timezone "UTC"}}selected{{end}}>UTC</option>
				<option value="Asia/Shanghai" {{if eq .Site.Timezone "Asia/Shanghai"}}selected{{end}}>Asia/Shanghai</option>
				<option value="Asia/Hong_Kong" {{if eq .Site.Timezone "Asia/Hong_Kong"}}selected{{end}}>Asia/Hong_Kong</option>
				<option value="Asia/Tokyo" {{if eq .Site.Timezone "Asia/Tokyo"}}selected{{end}}>Asia/Tokyo</option>
				<option value="Asia/Singapore" {{if eq .Site.Timezone "Asia/Singapore"}}selected{{end}}>Asia/Singapore</option>
				<option value="Asia/Dubai" {{if eq .Site.Timezone "Asia/Dubai"}}selected{{end}}>Asia/Dubai</option>
				<option value="Europe/London" {{if eq .Site.Timezone "Europe/London"}}selected{{end}}>Europe/London</option>
				<option value="Europe/Berlin" {{if eq .Site.Timezone "Europe/Berlin"}}selected{{end}}>Europe/Berlin</option>
				<option value="America/New_York" {{if eq .Site.Timezone "America/New_York"}}selected{{end}}>America/New_York</option>
				<option value="America/Chicago" {{if eq .Site.Timezone "America/Chicago"}}selected{{end}}>America/Chicago</option>
				<option value="America/Denver" {{if eq .Site.Timezone "America/Denver"}}selected{{end}}>America/Denver</option>
				<option value="America/Los_Angeles" {{if eq .Site.Timezone "America/Los_Angeles"}}selected{{end}}>America/Los_Angeles</option>
				<option value="Australia/Sydney" {{if eq .Site.Timezone "Australia/Sydney"}}selected{{end}}>Australia/Sydney</option>
			</select>
		</div>
		<div class="form-group">
			<label for="theme">{{T .Lang "site.theme"}}</label>
			<select id="theme" name="theme">
				{{range .ThemeOptions}}
				<option value="{{.Slug}}" {{if eq $.Site.Theme .Slug}}selected{{end}}>{{.Name}}</option>
				{{end}}
			</select>
		</div>
		<div class="form-group">
			<label for="admin_username">{{T .Lang "site.admin_username"}}</label>
			<input id="admin_username" name="admin_username" type="text" value="{{.Site.AdminUsername}}" required>
		</div>
		<div class="form-group">
			<label for="admin_email">{{T .Lang "site.admin_email"}}</label>
			<input id="admin_email" name="admin_email" type="email" value="{{.Site.AdminEmail}}" required>
		</div>
		<div class="form-group">
			<label for="admin_password">{{T .Lang "site.admin_password"}}</label>
			<input id="admin_password" name="admin_password" type="password" required>
		</div>
		<div class="form-group">
			<label for="admin_password_confirm">{{T .Lang "site.admin_password_confirm"}}</label>
			<input id="admin_password_confirm" name="admin_password_confirm" type="password" required>
		</div>
	</div>
	<div class="form-actions">
		<button class="btn btn-primary" type="submit">{{T .Lang "site.finish"}}</button>
		<a class="btn" href="/install/database">{{T .Lang "site.back"}}</a>
	</div>
</form>
{{end}}
`

const successTemplate = `
{{define "success"}}
<p class="summary">{{T .Lang "success.summary"}}</p>
<ul class="feature-list">
	<li>{{T .Lang "success.config_saved" .ConfigPath}}</li>
	<li>{{T .Lang "success.db_saved"}}</li>
	<li>{{T .Lang "success.jwt"}}</li>
	<li>{{T .Lang "success.port"}}</li>
</ul>
<div class="form-actions">
	<a class="btn btn-primary" href="/admin/login">{{T .Lang "success.admin"}}</a>
	<a class="btn" href="/">{{T .Lang "success.frontend"}}</a>
</div>
{{end}}
`
