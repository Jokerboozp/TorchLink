package toolaccounts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type mockAccount struct {
	Password   string
	Granted    bool
	Privileges string
}

// Model an image-created bootstrap account that can administer users and data,
// but cannot grant NAMED COLLECTION ADMIN. GRANT ALL must fail in this fixture.
const mockClickHousePrivileges = "SELECT, INSERT, CREATE USER, ALTER USER, DROP USER"

func TestBootstrapCreatesOnlyMissingAccounts(t *testing.T) {
	for _, database := range []string{"postgres", "clickhouse"} {
		t.Run(database, func(t *testing.T) {
			run, statePath := bootstrapFixture(t, database)
			// Exercise shell syntax, both SQL quote characters, Unicode and trailing
			// newlines. These must remain data, never shell or SQL instructions.
			user, password := "ad'min\";`\\雪\n", "pa'\\ss\";$(false)\n\n"
			output, err := run(user, password, "")
			if err != nil {
				t.Fatalf("bootstrap failed: %v, %s", err, output)
			}
			state := readMockAccounts(t, statePath)
			wantPassword := password
			wantPrivileges := ""
			if database == "clickhouse" {
				sum := sha256.Sum256([]byte(password))
				wantPassword = hex.EncodeToString(sum[:])
				wantPrivileges = mockClickHousePrivileges
			}
			if state[user] != (mockAccount{Password: wantPassword, Granted: true, Privileges: wantPrivileges}) {
				t.Fatal("created account did not preserve credentials or inherit the bootstrap account's privileges")
			}
			// Existing accounts retain even deliberately restricted permissions.
			state[user] = mockAccount{Password: "previous-password", Granted: false}
			writeMockAccounts(t, statePath, state)
			output, err = run(user, "different-password", "")
			if err != nil {
				t.Fatalf("second bootstrap failed: %v, %s", err, output)
			}
			if got := readMockAccounts(t, statePath)[user]; got != state[user] {
				t.Fatal("existing account was changed")
			}
		})
	}
}

func TestBootstrapFailsWithoutLeakingCredentials(t *testing.T) {
	for _, database := range []string{"postgres", "clickhouse"} {
		for _, failure := range []string{"connect", "create"} {
			t.Run(database+"/"+failure, func(t *testing.T) {
				run, statePath := bootstrapFixture(t, database)
				output, err := run("admin", "secret-password", failure)
				if err == nil {
					t.Fatal("client failure was reported as success")
				}
				for _, secret := range []string{"secret-password", "connection-password", "CLIENT_DIAGNOSTIC"} {
					if strings.Contains(output, secret) {
						t.Fatal("client output exposed a credential or raw diagnostic")
					}
				}
				if len(readMockAccounts(t, statePath)) != 0 {
					t.Fatal("failed creation left an account")
				}
			})
		}
	}
}

func TestClickHouseGrantFailureCleanup(t *testing.T) {
	for _, failure := range []string{"grant", "grant-drop"} {
		t.Run(failure, func(t *testing.T) {
			run, statePath := bootstrapFixture(t, "clickhouse")
			output, err := run("admin", "secret-password", failure)
			if err == nil {
				t.Fatal("grant failure was reported as success")
			}
			_, exists := readMockAccounts(t, statePath)["admin"]
			if failure == "grant" && exists {
				t.Fatal("newly created account was not rolled back")
			}
			if failure == "grant-drop" && (!exists || !strings.Contains(output, "手工恢复")) {
				t.Fatal("rollback failure must identify the need for manual recovery")
			}
			if failure == "grant" {
				if output, err := run("admin", "secret-password", ""); err != nil {
					t.Fatalf("retry after rollback failed: %v, %s", err, output)
				}
			}
		})
	}
}

func TestBootstrapRejectsEmptyCredentials(t *testing.T) {
	for _, database := range []string{"postgres", "clickhouse"} {
		t.Run(database, func(t *testing.T) {
			run, _ := bootstrapFixture(t, database)
			for _, pair := range [][2]string{{"", "password"}, {"admin", ""}} {
				if _, err := run(pair[0], pair[1], ""); err == nil {
					t.Fatal("empty credentials were accepted")
				}
			}
		})
	}
}

func bootstrapFixture(t *testing.T, database string) (func(string, string, string) (string, error), string) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("POSIX shell is not installed")
	}
	if database == "clickhouse" {
		if _, err := exec.LookPath("sha256sum"); err != nil {
			t.Skip("sha256sum is required; the ClickHouse Alpine image includes it")
		}
	}
	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	client := "psql"
	if database == "clickhouse" {
		client = "clickhouse-client"
	}
	wrapper := "#!/bin/sh\nexec '" + strings.ReplaceAll(executable, "'", "'\\''") + "' -test.run=^TestToolAccountClientHelper$ -- " + database + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, client), []byte(wrapper), 0700); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, "state.json")
	writeMockAccounts(t, statePath, map[string]mockAccount{})
	return func(user, password, failure string) (string, error) {
		cmd := exec.Command("sh", database+".sh")
		cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
			"GO_WANT_TOOL_ACCOUNT_HELPER=1", "TOOL_ACCOUNT_STATE="+statePath, "TOOL_ACCOUNT_FAILURE="+failure,
			"SERVICE_ADMIN_USER="+user, "SERVICE_ADMIN_PASSWORD="+password,
			"POSTGRES_USER=iot", "POSTGRES_PASSWORD=unused-fallback", "PGUSER=postgres", "PGPASSWORD=connection-password",
			"PGHOST=db-a,db-b", "PGPORT=5432", "PGDATABASE=iot", "PGTARGETSESSIONATTRS=read-write",
			"CLICKHOUSE_USER=iot", "CLICKHOUSE_PASSWORD=connection-password", "CLICKHOUSE_HOST=clickhouse", "CLICKHOUSE_PORT=9000")
		output, err := cmd.CombinedOutput()
		return string(output), err
	}, statePath
}

func readMockAccounts(t *testing.T, path string) map[string]mockAccount {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var accounts map[string]mockAccount
	if err := json.Unmarshal(data, &accounts); err != nil {
		t.Fatal(err)
	}
	return accounts
}

func writeMockAccounts(t *testing.T, path string, accounts map[string]mockAccount) {
	t.Helper()
	data, err := json.Marshal(accounts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

// The mock clients exercise the real shell scripts without contacting a DB.
// SQL grammar support is based on PostgreSQL 17 / ClickHouse 25.7; this does not
// replace deployment verification against those database engines.
func TestToolAccountClientHelper(t *testing.T) {
	if os.Getenv("GO_WANT_TOOL_ACCOUNT_HELPER") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) < 2 {
		os.Exit(90)
	}
	database, args := args[1], args[2:]
	fail := func() {
		fmt.Fprintln(os.Stderr, "CLIENT_DIAGNOSTIC connection-password", os.Getenv("SERVICE_ADMIN_PASSWORD"))
		os.Exit(21)
	}
	for _, arg := range args {
		if strings.Contains(arg, os.Getenv("SERVICE_ADMIN_PASSWORD")) || strings.Contains(arg, "connection-password") {
			os.Exit(91)
		}
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(92)
	}
	query := strings.TrimSpace(string(data))
	failure := os.Getenv("TOOL_ACCOUNT_FAILURE")
	if failure == "connect" {
		fail()
	}
	statePath := os.Getenv("TOOL_ACCOUNT_STATE")
	state := readMockAccounts(t, statePath)
	if database == "postgres" {
		for _, required := range []string{"\\getenv tool_admin_user SERVICE_ADMIN_USER", "\\getenv tool_admin_password SERVICE_ADMIN_PASSWORD", "format('CREATE ROLE %I LOGIN SUPERUSER PASSWORD %L'", "WHERE NOT EXISTS", "rolname = :'tool_admin_user'", "\\gexec", "pg_advisory_xact_lock"} {
			if !strings.Contains(query, required) {
				os.Exit(93)
			}
		}
		if os.Getenv("PGUSER") != "postgres" || os.Getenv("PGPASSWORD") != "connection-password" || os.Getenv("PGHOST") != "db-a,db-b" || os.Getenv("PGTARGETSESSIONATTRS") != "read-write" {
			os.Exit(94)
		}
		user := os.Getenv("SERVICE_ADMIN_USER")
		if _, ok := state[user]; !ok {
			if failure == "create" {
				fail()
			}
			state[user] = mockAccount{Password: os.Getenv("SERVICE_ADMIN_PASSWORD"), Granted: true}
		}
	} else {
		if os.Getenv("CLICKHOUSE_PASSWORD") != "connection-password" {
			os.Exit(95)
		}
		// Only hex-escaped string literals are permitted for usernames. Decode
		// them to ensure dangerous characters survive as exactly one SQL value.
		re := regexp.MustCompile(`'((?:\\x[0-9a-f]{2})+)'`)
		match := re.FindStringSubmatch(query)
		if match == nil {
			os.Exit(96)
		}
		name, err := hex.DecodeString(strings.ReplaceAll(match[1], `\x`, ""))
		if err != nil {
			os.Exit(97)
		}
		user := string(name)
		switch {
		case strings.HasPrefix(query, "SELECT count()"):
			if _, ok := state[user]; ok {
				fmt.Println(1)
			} else {
				fmt.Println(0)
			}
		case strings.HasPrefix(query, "CREATE USER"):
			if failure == "create" {
				fail()
			}
			if _, ok := state[user]; ok {
				fail()
			}
			hash := regexp.MustCompile(`sha256_hash BY '([0-9a-f]{64})';$`).FindStringSubmatch(query)
			if hash == nil {
				os.Exit(98)
			}
			state[user] = mockAccount{Password: hash[1]}
		case strings.HasPrefix(query, "GRANT ALL ON *.*"):
			// Reproduce ClickHouse 25.7 code 497: the bootstrap account lacks
			// NAMED COLLECTION ADMIN and cannot grant the ALL privilege group.
			fail()
		case strings.HasPrefix(query, "GRANT CURRENT GRANTS ON *.*") && strings.HasSuffix(query, "WITH GRANT OPTION;"):
			if strings.HasPrefix(failure, "grant") {
				fail()
			}
			account := state[user]
			account.Granted = true
			account.Privileges = mockClickHousePrivileges
			state[user] = account
		case strings.HasPrefix(query, "DROP USER"):
			if failure == "grant-drop" {
				fail()
			}
			delete(state, user)
		default:
			os.Exit(99)
		}
	}
	writeMockAccounts(t, statePath, state)
	os.Exit(0)
}
