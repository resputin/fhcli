package auth

import (
	_ "embed"
	"encoding/base64"
	"html"
)

// The callback pages match laddertruck's OAuth consent screen (FH-7340; the
// design is the "CLI" surface of ~/Code/docs/ai-ops/oauth-consent-prototype).
// They're served from localhost, so the CSS and logo are inlined and the
// fonts fall back to the system's.

//go:embed assets/logo.svg
var logoSVG []byte

var logoDataURI = "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(logoSVG)

const (
	iconOK  = `<div class="state-icon ok"><svg viewBox="0 0 256 256" fill="currentColor" aria-hidden="true"><path d="m229.7 77.7-128 128a8 8 0 0 1-11.4 0l-56-56a8 8 0 0 1 11.4-11.4l50.3 50.4L218.3 66.3a8 8 0 0 1 11.4 11.4Z"/></svg></div>`
	iconErr = `<div class="state-icon err"><svg viewBox="0 0 256 256" fill="currentColor" aria-hidden="true"><path d="M205.7 194.3a8 8 0 0 1-11.4 11.4L128 139.3l-66.3 66.4a8 8 0 0 1-11.4-11.4l66.4-66.3-66.4-66.3a8 8 0 0 1 11.4-11.4l66.3 66.4 66.3-66.4a8 8 0 0 1 11.4 11.4L139.3 128Z"/></svg></div>`
)

const pageCSS = `
:root{--surface-primary:#fff;--surface-secondary:#F3F4F7;--surface-tertiary:#EAECF0;--content-primary:#2B2F36;--content-secondary:#464A53;--content-tertiary:#6C6F7F;--stroke-primary:rgba(0,0,0,.08);--action-primary:#1767F2;--ocean-soft:#EAF1FE;--error-soft:#FDECEB;--error-text:#B72F29;--shadow:0 1px 2px rgba(16,24,40,.04),0 8px 24px rgba(16,24,40,.06)}
@media (prefers-color-scheme:dark){:root{--surface-primary:#25272E;--surface-secondary:#202228;--surface-tertiary:#2E3038;--content-primary:#F3F4F7;--content-secondary:#DCDDE0;--content-tertiary:#A4A8BC;--stroke-primary:rgba(255,255,255,.06);--ocean-soft:rgba(23,103,242,.16);--error-soft:rgba(183,47,41,.18);--error-text:#F19D99;--shadow:0 8px 24px rgba(0,0,0,.4)}.wordmark{filter:invert(1) hue-rotate(180deg) saturate(1.4)}}
*{box-sizing:border-box}html,body{margin:0}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:var(--surface-secondary);color:var(--content-primary);min-height:100vh;-webkit-font-smoothing:antialiased}
.page{min-height:100vh;display:flex;flex-direction:column;align-items:center;padding:48px 16px}
.card{width:100%;max-width:480px;background:var(--surface-primary);border:1px solid var(--stroke-primary);border-radius:12px;box-shadow:var(--shadow)}
.card-body{padding:32px 32px 8px;text-align:center}
.state-icon{width:56px;height:56px;border-radius:50%;display:grid;place-items:center;margin:0 auto 16px}
.state-icon.ok{background:var(--ocean-soft);color:var(--action-primary)}
.state-icon.err{background:var(--error-soft);color:var(--error-text)}
.state-icon svg{width:28px;height:28px}
h1{font-size:20px;line-height:28px;font-weight:600;margin:0 0 6px;letter-spacing:-.01em}
.sub{font-size:14px;line-height:20px;color:var(--content-tertiary);margin:0 0 24px}
.label{font-size:12px;font-weight:600;text-transform:uppercase;letter-spacing:.06em;color:var(--content-tertiary);margin:0 0 8px;text-align:left}
.err-detail{font-family:ui-monospace,Menlo,monospace;font-size:12px;text-align:left;background:var(--surface-secondary);border-radius:8px;padding:10px 12px;color:var(--content-secondary);margin:0 0 24px;white-space:pre-wrap;word-break:break-word}
.cmd{font-family:ui-monospace,Menlo,monospace;font-size:13px;background:var(--surface-tertiary);padding:1px 6px;border-radius:4px;color:var(--content-secondary)}
.page-foot{margin-top:32px}.wordmark{height:22px;display:block}
@media (max-width:480px){.page{padding:0}.card{border:0;border-radius:0;box-shadow:none}.card-body{padding:24px 16px 8px}}
`

// page renders a callback card. inner is trusted HTML; use pageDetail for
// text that came from the request.
func page(title, inner string) string {
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<title>` + html.EscapeString(title) + `</title><style>` + pageCSS + `</style></head><body><main class="page">` +
		`<section class="card"><div class="card-body">` + inner + `</div></section>` +
		`<footer class="page-foot"><img class="wordmark" src="` + logoDataURI + `" alt="FireHydrant, a Freshworks company"></footer>` +
		`</main></body></html>`
}

func successPage() string {
	return page("Logged in to the FireHydrant CLI", iconOK+
		`<h1>You're logged in to the CLI</h1><p class="sub">You can close this tab and go back to your terminal.</p>`)
}

func cancelledPage(detail string) string {
	return page("Login cancelled", iconErr+
		`<h1>Login cancelled</h1><p class="sub">The CLI wasn't given access. Run <span class="cmd">fhcli login</span> to try again.</p>`+
		pageDetail(detail))
}

func failedPage(detail string) string {
	return page("Login failed", iconErr+
		`<h1>Login failed</h1><p class="sub">Return to your terminal for details, then run <span class="cmd">fhcli login</span> to try again.</p>`+
		pageDetail(detail))
}

func noticePage(heading, sub string) string {
	return page(heading, iconErr+`<h1>`+html.EscapeString(heading)+`</h1><p class="sub">`+html.EscapeString(sub)+`</p>`)
}

func pageDetail(detail string) string {
	if detail == "" {
		return ""
	}
	return `<p class="label">Details</p><pre class="err-detail">` + html.EscapeString(detail) + `</pre>`
}
