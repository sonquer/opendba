//go:build linux && amd64

package embedded

import "embed"

//go:embed linux_amd64
var libraries embed.FS

const dir = "linux_amd64"
