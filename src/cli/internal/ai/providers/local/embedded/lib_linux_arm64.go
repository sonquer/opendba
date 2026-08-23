//go:build linux && arm64

package embedded

import "embed"

//go:embed linux_arm64
var libraries embed.FS

const dir = "linux_arm64"
