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
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"text/template"

	"github.com/sirupsen/logrus"

	"github.com/percona/percona-toolkit/src/go/lib/pginfo"
	"github.com/percona/percona-toolkit/src/go/pt-pg-summary/internal/tu"
	"github.com/percona/percona-toolkit/src/go/pt-pg-summary/templates"
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

var tests []Test = []Test{
	{"source", tu.IPv4Host, tu.SourcePort, tu.Username, tu.Password},
	{"replica", tu.IPv4Host, tu.ReplicaPort, tu.Username, tu.Password},
	{"source_socket", tu.SocketDir, tu.SourcePort, tu.Username, tu.Password},
}

var logger = logrus.New()

const testSleep = 1

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
	if sandboxAvailable {
		return
	}
	if os.Getenv("PT_PG_SANDBOX_REQUIRED") != "" {
		t.Fatalf("no sandbox on %s:%s and PT_PG_SANDBOX_REQUIRED is set", tu.IPv4Host, tu.SourcePort)
	}
	t.Skipf("no sandbox on %s:%s, run sandbox-pg/test-env start", tu.IPv4Host, tu.SourcePort)
}

func skipIfNoSocket(t *testing.T, test Test) {
	if test.host == "" {
		t.Skip("no unix_socket_directories on this server")
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
			skipIfNoSocket(t, test)
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
			skipIfNoSocket(t, test)
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
			skipIfNoSocket(t, test)
			db := connectTo(t, test, "postgres")
			defer db.Close()
			info, err := pginfo.NewWithLogger(db, nil, testSleep, logger)
			if err != nil {
				t.Fatalf("Cannot run NewWithLogger using %q: %s", test.dsn("postgres"), err)
			}
			errs := info.CollectGlobalInfo(db)
			if len(errs) > 0 {
				for _, err := range errs {
					t.Logf("collect error: %s", err)
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
			skipIfNoSocket(t, test)
			db := connectTo(t, test, "postgres")
			defer db.Close()
			info, err := pginfo.NewWithLogger(db, nil, testSleep, logger)
			if err != nil {
				t.Fatalf("Cannot run New using %q: %s", test.dsn("postgres"), err)
			}
			names := info.DatabaseNames()
			n := 0
			for _, name := range names {
				if name != "postgres" && !strings.HasPrefix(name, "template") {
					n++
				}
			}
			if n < 2 {
				t.Errorf("expected at least 2 user databases, got %d: %v", n, names)
			}
			for _, dbName := range names {
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

func TestReplicationTopology(t *testing.T) {
	skipIfNoSandbox(t)

	source := connectTo(t, tests[0], "postgres")
	defer source.Close()

	var inRecovery bool
	if err := source.QueryRow("SELECT pg_is_in_recovery()").Scan(&inRecovery); err != nil {
		t.Fatalf("pg_is_in_recovery on %s: %s", tests[0].name, err)
	}
	if inRecovery {
		t.Errorf("%s is in recovery, expected the primary", tests[0].name)
	}

	info, err := pginfo.NewWithLogger(source, nil, testSleep, logger)
	if err != nil {
		t.Fatalf("Cannot run NewWithLogger using %q: %s", tests[0].dsn("postgres"), err)
	}
	if errs := info.CollectGlobalInfo(source); len(errs) > 0 {
		for _, err := range errs {
			t.Logf("collect error: %s", err)
		}
		t.Fatalf("Cannot collect global information using %q", tests[0].dsn("postgres"))
	}
	if len(info.SlaveHosts10) == 0 {
		t.Errorf("no standby connected to %s, the sandbox is not replicating", tests[0].name)
	}

	replica := connectTo(t, tests[1], "postgres")
	defer replica.Close()

	if err := replica.QueryRow("SELECT pg_is_in_recovery()").Scan(&inRecovery); err != nil {
		t.Fatalf("pg_is_in_recovery on %s: %s", tests[1].name, err)
	}
	if !inRecovery {
		t.Errorf("%s is not in recovery, expected the standby", tests[1].name)
	}
}

func TestReportShowsPortAndDataDirectory(t *testing.T) {
	skipIfNoSandbox(t)

	db := connectTo(t, tests[0], "postgres")
	defer db.Close()

	info, err := pginfo.NewWithLogger(db, nil, testSleep, logger)
	if err != nil {
		t.Fatalf("Cannot run NewWithLogger using %q: %s", tests[0].dsn("postgres"), err)
	}
	if errs := info.CollectGlobalInfo(db); len(errs) > 0 {
		for _, err := range errs {
			t.Logf("collect error: %s", err)
		}
		t.Fatalf("Cannot collect global information using %q", tests[0].dsn("postgres"))
	}

	tmpl := template.Must(template.New("master").Funcs(funcsMap()).Parse(templates.TPL))
	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, "port_and_datadir", info.PortAndDatadir); err != nil {
		t.Fatalf("Cannot render port_and_datadir: %s", err)
	}

	for _, row := range []string{
		`\| port +\| ` + tu.SourcePort + ` +\|`,
		`\| data_directory +\| /\S+ +\|`,
	} {
		if !regexp.MustCompile(row).Match(out.Bytes()) {
			t.Errorf("no row matching %q in:\n%s", row, out.String())
		}
	}
}

const semVerRE = `(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)` +
	`(?:-(?:(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?` +
	`(?:\+(?:[0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?`

func TestVersionOption(t *testing.T) {
	out, err := exec.Command("../../../bin/"+toolname, "--version").Output()
	if err != nil {
		t.Errorf("error executing %s --version: %s", toolname, err.Error())
	}
	re := regexp.MustCompile(toolname + `\n.*Version v?` + semVerRE + `\n`)
	if !re.Match(out) {
		t.Errorf("%s --version returns wrong result:\n%s", toolname, out)
	}
}
