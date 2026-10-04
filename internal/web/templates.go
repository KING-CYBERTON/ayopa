package web

const pageTemplates = `
{{define "head"}}<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
body { font-family: system-ui, sans-serif; max-width: 34rem; margin: 3rem auto; padding: 0 1rem; line-height: 1.5; }
label { display: block; margin: 1rem 0 .25rem; font-weight: 600; }
input, textarea, button { font: inherit; padding: .5rem; width: 100%; box-sizing: border-box; }
button { margin-top: 1rem; cursor: pointer; }
.err { color: #b00020; }
.ok { color: #1b5e20; }
.note { border-top: 1px solid #ddd; padding: .75rem 0; }
.note div { white-space: pre-wrap; }
small { color: #666; }
</style>
</head>
<body>
{{end}}

{{define "foot"}}</body>
</html>{{end}}

{{define "signup"}}{{template "head" .}}
<h1>Create your workspace</h1>
{{if .Error}}<p class="err">{{.Error}}</p>{{end}}
<form method="post" action="/signup">
<label for="subdomain">Subdomain</label>
<input id="subdomain" name="subdomain" value="{{.Subdomain}}" required minlength="3" maxlength="63" autocomplete="off" autocapitalize="none">
<small>Your workspace will live at <b>yourname</b>.{{.BaseDomain}}</small>
<label for="email">Email</label>
<input id="email" name="email" type="email" value="{{.Email}}" required autocomplete="email">
<label for="password">Password</label>
<input id="password" name="password" type="password" required minlength="10" maxlength="128" autocomplete="new-password">
<small>At least 10 characters.</small>
<button type="submit">Create workspace</button>
</form>
{{template "foot" .}}{{end}}

{{define "login"}}{{template "head" .}}
<h1>Log in to {{.Tenant}}</h1>
{{if .Notice}}<p class="ok">{{.Notice}}</p>{{end}}
{{if .Error}}<p class="err">{{.Error}}</p>{{end}}
<form method="post" action="/login">
<label for="email">Email</label>
<input id="email" name="email" type="email" value="{{.Email}}" required autocomplete="username">
<label for="password">Password</label>
<input id="password" name="password" type="password" required autocomplete="current-password">
<button type="submit">Log in</button>
</form>
{{template "foot" .}}{{end}}

{{define "dashboard"}}{{template "head" .}}
<h1>{{.Tenant}}</h1>
<p><small>Signed in as {{.UserEmail}}</small></p>
<form method="post" action="/logout"><button type="submit">Log out</button></form>
<h2>Notes</h2>
<form method="post" action="/notes">
<label for="body">New note</label>
<textarea id="body" name="body" rows="3" required maxlength="2000"></textarea>
<button type="submit">Add note</button>
</form>
{{range .Notes}}<div class="note"><div>{{.Body}}</div><small>{{.Author}} · {{.CreatedAt.UTC.Format "2006-01-02 15:04"}} UTC</small></div>
{{else}}<p><small>No notes yet.</small></p>{{end}}
{{template "foot" .}}{{end}}
`
