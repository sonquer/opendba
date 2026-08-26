//go:build !(darwin && arm64) && !(darwin && amd64) && !(linux && amd64) && !(linux && arm64) && !(windows && amd64) && !(windows && arm64)

package embedded

import "embed"

// libraries is empty on a machine nobody publishes a build for.
var libraries embed.FS

const dir = ""
