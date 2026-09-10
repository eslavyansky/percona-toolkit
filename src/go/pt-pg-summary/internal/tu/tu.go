// This program is copyright 2019-2026 Percona LLC and/or its affiliates.
//
// THIS PROGRAM IS PROVIDED "AS IS" AND WITHOUT ANY EXPRESS OR IMPLIED
// WARRANTIES, INCLUDING, WITHOUT LIMITATION, THE IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE.
//
// This program is free software; you can redistribute it and/or modify it under
// the terms of the GNU General Public License as published by the Free Software
// Foundation, version 2.
//
// You should have received a copy of the GNU General Public License, version 2
// along with this program; if not, see <https://www.gnu.org/licenses/>.

package tu // test utils

import (
	"os"
	"path/filepath"
)

const (
	ipv4Host = "127.0.0.1"
	username = "postgres"
	password = "root"

	sourcePort  = "15432"
	replicaPort = "15433"

	tmpDir = "/tmp"
)

var (
	// IPv4Host env(PG_IPV4_HOST) or 127.0.0.1
	IPv4Host = getVar("PG_IPV4_HOST", ipv4Host)
	// Password env(PG_PASSWORD) or root
	Password = getVar("PG_PASSWORD", password)
	// Username env(PG_USERNAME) or postgres
	Username = getVar("PG_USERNAME", username)

	// SourcePort env(PG_SOURCE_PORT) or 15432, the sandbox-pg source instance
	SourcePort = getVar("PG_SOURCE_PORT", sourcePort)
	// ReplicaPort env(PG_REPLICA_PORT) or 15433, the sandbox-pg replica instance
	ReplicaPort = getVar("PG_REPLICA_PORT", replicaPort)

	// TmpDir env(TMP_DIR) or /tmp, the directory holding the sandbox-pg instances
	TmpDir = getVar("TMP_DIR", tmpDir)
)

// SocketDir returns the directory holding the Unix socket of the sandbox-pg
// instance running on the given port.
func SocketDir(port string) string {
	return filepath.Join(TmpDir, port)
}

func getVar(varname, defaultValue string) string {
	if v := os.Getenv(varname); v != "" {
		return v
	}
	return defaultValue
}
