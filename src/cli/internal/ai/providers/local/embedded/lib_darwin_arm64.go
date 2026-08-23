//go:build darwin && arm64

package embedded

import "embed"

//go:embed darwin_arm64
var libraries embed.FS

const dir = "darwin_arm64"
