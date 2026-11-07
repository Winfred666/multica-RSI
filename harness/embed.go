// Package harness ships the workspace startup kit with the server.
package harness

import "embed"

//go:embed startup.gotmpl manifest.json skills plugins/runtime.py plugins/*/hook.py plugins/*/multica.plugin.json
var Files embed.FS
