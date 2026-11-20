//go:build !unix

package agentcli

import "io/fs"

func ownerUID(fs.FileInfo) (int, bool) { return 0, false }
