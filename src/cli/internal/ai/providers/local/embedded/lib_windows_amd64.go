//go:build windows && amd64

package embedded

import "embed"

//go:embed windows_amd64
var libraries embed.FS

const dir = "windows_amd64"
