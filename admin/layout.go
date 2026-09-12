package admin

import "html/template"

// tailwindScriptTag loads Tailwind's Play CDN, per ADR 0003 in the harness
// docs: no Node/npm build step, JIT-compiled utility classes at runtime.
const tailwindScriptTag = `<script src="https://cdn.tailwindcss.com"></script>`

// layoutTemplate is the shared base layout every admin page renders into.
// Pages supply their body by defining a "content" template in the same
// *template.Template set (see cloneWithContent); executed on its own, the
// "content" block falls back to empty output so the layout stays valid,
// renderable HTML in isolation.
var layoutTemplate = template.Must(template.New("layout").Parse(`<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>tanGO Admin</title>
` + tailwindScriptTag + `
<style type="text/tailwindcss">
@layer components {
  /* Component classes (button, card, table, input, badge, ...) land here
     once the default theme is designed. */
}
</style>
</head>
<body>
<nav></nav>
<main>{{block "content" .}}{{end}}</main>
</body>
</html>
`))

// freshLayout returns a fresh, never-executed clone of layoutTemplate.
// html/template forbids cloning a template after it has executed, so
// layoutTemplate itself is never executed directly — only clones are.
func freshLayout() *template.Template {
	return template.Must(layoutTemplate.Clone())
}

// cloneWithContent returns a fresh layout clone with "content" defined from
// contentSource, so ExecuteTemplate(w, "layout", data) renders the shared
// head/nav/Tailwind setup around that page's body.
func cloneWithContent(contentSource string) *template.Template {
	return template.Must(freshLayout().New("content").Parse(contentSource))
}
