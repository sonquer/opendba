//go:build darwin && amd64

package embedded

import "embed"

//go:embed darwin_amd64
var libraries embed.FS

const dir = "darwin_amd64"
