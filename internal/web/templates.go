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

{{define "directory"}}{{template "head" .}}
<h1>{{.MallName}}</h1>
<p><small>Shops in this mall</small></p>
{{range .Shops}}<div class="note"><a href="{{.URL}}">{{.Name}}</a></div>
{{else}}<p><small>No shops yet.</small></p>{{end}}
{{template "foot" .}}{{end}}

{{define "storefront"}}{{template "head" .}}
<h1>{{.Shop}}</h1>
<p><small>Part of {{.MallName}}</small></p>
<p>This storefront is being set up.</p>
<p><small><a href="/admin/login">Shop admin</a></small></p>
{{template "foot" .}}{{end}}

{{define "login"}}{{template "head" .}}
<h1>Log in to {{.Shop}}</h1>
{{if .Error}}<p class="err">{{.Error}}</p>{{end}}
<form method="post" action="/admin/login">
<label for="email">Email</label>
<input id="email" name="email" type="email" value="{{.Email}}" required autocomplete="username">
<label for="password">Password</label>
<input id="password" name="password" type="password" required autocomplete="current-password">
<button type="submit">Log in</button>
</form>
{{template "foot" .}}{{end}}

{{define "setup"}}{{template "head" .}}
<h1>Set up {{.Shop}}</h1>
{{if .Error}}<p class="err">{{.Error}}</p>{{end}}
{{if .Token}}
<p>Choose a password for <b>{{.Email}}</b>.</p>
<form method="post" action="/admin/setup">
<input type="hidden" name="token" value="{{.Token}}">
<label for="password">Password</label>
<input id="password" name="password" type="password" required minlength="10" maxlength="128" autocomplete="new-password">
<small>At least 10 characters.</small>
<label for="confirm">Repeat password</label>
<input id="confirm" name="confirm" type="password" required minlength="10" maxlength="128" autocomplete="new-password">
<button type="submit">Save and continue</button>
</form>
{{end}}
{{template "foot" .}}{{end}}

{{define "dashboard"}}{{template "head" .}}
<h1>{{.Shop}}</h1>
<p><small>Signed in as {{.UserEmail}} ({{.Role}}) in {{.MallName}}</small></p>
<form method="post" action="/admin/logout"><button type="submit">Log out</button></form>
<h2>Notes</h2>
<form method="post" action="/admin/notes">
<label for="body">New note</label>
<textarea id="body" name="body" rows="3" required maxlength="2000"></textarea>
<button type="submit">Add note</button>
</form>
{{range .Notes}}<div class="note"><div>{{.Body}}</div><small>{{.Author}} · {{.CreatedAt.UTC.Format "2006-01-02 15:04"}} UTC</small></div>
{{else}}<p><small>No notes yet.</small></p>{{end}}
{{template "foot" .}}{{end}}
`
