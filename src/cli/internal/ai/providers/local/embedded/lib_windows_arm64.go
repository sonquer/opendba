//go:build windows && arm64

package embedded

import "embed"

//go:embed windows_arm64
var libraries embed.FS

const dir = "windows_arm64"
