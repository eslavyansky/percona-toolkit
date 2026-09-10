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

package main

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/percona/percona-toolkit/src/go/lib/pginfo"
	"github.com/percona/percona-toolkit/src/go/pt-pg-summary/internal/tu"
)

type Test struct {
	name     string
	host     string
	port     string
	username string
	password string
}

func (test Test) dsn(dbName string) string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s sslmode=disable dbname=%s",
		test.host, test.port, test.username, test.password, dbName)
}

// The sandbox-pg test environment provides a source and a physical replica,
// both reachable over TCP and over a Unix socket in the instance directory.
var tests []Test = []Test{
	{"source", tu.IPv4Host, tu.SourcePort, tu.Username, tu.Password},
	{"replica", tu.IPv4Host, tu.ReplicaPort, tu.Username, tu.Password},
	{"source_socket", tu.SocketDir(tu.SourcePort), tu.SourcePort, tu.Username, tu.Password},
}

var logger = logrus.New()

// testSleep is the pause CollectGlobalInfo takes between the two reads of the
// status counters.  The tool defaults to 30 seconds; the tests only need the
// code path to run.
const testSleep = 1

// sandboxAvailable tells whether the sandbox-pg source instance answered
// during TestMain.  The tests are skipped, not failed, when it did not.
var sandboxAvailable bool

func TestMain(m *testing.M) {
	logger.SetLevel(logrus.WarnLevel)
	if db, err := connect(tests[0].dsn("postgres")); err == nil {
		sandboxAvailable = true
		db.Close()
	}
	code := m.Run()
	os.Exit(code)
}

func skipIfNoSandbox(t *testing.T) {
	if !sandboxAvailable {
		t.Skipf("the sandbox-pg source instance does not answer on %s:%s, start it with 'sandbox-pg/test-env start'",
			tu.IPv4Host, tu.SourcePort)
	}
}

func connectTo(t *testing.T, test Test, dbName string) *sql.DB {
	db, err := connect(test.dsn(dbName))
	if err != nil {
		t.Fatalf("Cannot connect to the db using %q: %s", test.dsn(dbName), err)
	}
	return db
}

func TestConnection(t *testing.T) {
	skipIfNoSandbox(t)
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			db := connectTo(t, test, "postgres")
			db.Close()
		})
	}
}

func TestNewWithLogger(t *testing.T) {
	skipIfNoSandbox(t)
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			db := connectTo(t, test, "postgres")
			defer db.Close()
			if _, err := pginfo.NewWithLogger(db, nil, testSleep, logger); err != nil {
				t.Errorf("Cannot run NewWithLogger using %q: %s", test.dsn("postgres"), err)
			}
		})
	}
}

func TestCollectGlobalInfo(t *testing.T) {
	skipIfNoSandbox(t)
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			db := connectTo(t, test, "postgres")
			defer db.Close()
			info, err := pginfo.NewWithLogger(db, nil, testSleep, logger)
			if err != nil {
				t.Fatalf("Cannot run NewWithLogger using %q: %s", test.dsn("postgres"), err)
			}
			errs := info.CollectGlobalInfo(db)
			if len(errs) > 0 {
				logger.Errorf("Cannot collect info")
				for _, err := range errs {
					logger.Error(err)
				}
				t.Errorf("Cannot collect global information using %q", test.dsn("postgres"))
			}
		})
	}
}

func TestCollectPerDatabaseInfo(t *testing.T) {
	skipIfNoSandbox(t)
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			db := connectTo(t, test, "postgres")
			defer db.Close()
			info, err := pginfo.NewWithLogger(db, nil, testSleep, logger)
			if err != nil {
				t.Fatalf("Cannot run New using %q: %s", test.dsn("postgres"), err)
			}
			for _, dbName := range info.DatabaseNames() {
				conn := connectTo(t, test, dbName)
				if err := info.CollectPerDatabaseInfo(conn, dbName); err != nil {
					t.Errorf("Cannot collect information for the %s database using %q: %s",
						dbName, test.dsn(dbName), err)
				}
				conn.Close()
			}
		})
	}
}

// semVerRE is the SemVer pattern from https://semver.org (RE2-compatible
// variant), used to validate the version line printed by --version.
const semVerRE = `(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)` +
	`(?:-(?:(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?` +
	`(?:\+(?:[0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?`

/*
Option --version
*/
func TestVersionOption(t *testing.T) {
	out, err := exec.Command("../../../bin/"+toolname, "--version").Output()
	if err != nil {
		t.Errorf("error executing %s --version: %s", toolname, err.Error())
	}
	// We are using MustCompile here, because hard-coded RE should not fail
	re := regexp.MustCompile(toolname + `\n.*Version v?` + semVerRE + `\n`)
	if !re.Match(out) {
		t.Errorf("%s --version returns wrong result:\n%s", toolname, out)
	}
}
