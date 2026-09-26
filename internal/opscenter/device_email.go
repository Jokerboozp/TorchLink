package opscenter

import _ "embed"

//go:embed device_email.html
var deviceEmailHTML string

// Only this exact former built-in template is migrated; custom templates stay intact.
const legacyDeviceEmailHTML = `<h2>{{ .CommonAnnotations.summary }}</h2><pre style="white-space:pre-wrap;font-family:inherit">{{ .CommonAnnotations.description }}</pre>`
