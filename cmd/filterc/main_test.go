package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = run(args, &out, &errb)
	return code, out.String(), errb.String()
}

// isolateConfig points the default config lookup at empty directories, so a
// developer's ./filterc.toml or ~/.config/filterc/filterc.toml can't leak in.
func isolateConfig(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func TestCompileCommand(t *testing.T) {
	isolateConfig(t)
	code, out, errb := runCLI(t, "compile", "--json", "--min-lat", "1", "--min-lon", "2", "--max-lat", "3", "--max-lon", "4", "--limit", "10", "size == 5 && pokemon != 710")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	want := `{"min":{"lat":1,"lon":2},"max":{"lat":3,"lon":4},"limit":10,"filters":[{"size":{"min":5,"max":5}},{"pokemon":[{"id":710}],"iv":{"min":1,"max":0}}]}` + "\n"
	if out != want {
		t.Errorf("stdout %s\nwant %s", out, want)
	}
}

func TestCompileCommandErrorShowsCaret(t *testing.T) {
	isolateConfig(t)
	code, out, errb := runCLI(t, "compile", "iv >= 90 && x == 1")
	if code != 1 || out != "" {
		t.Errorf("exit %d stdout %q", code, out)
	}
	want := "error: 1:13: unknown field \"x\"\n  iv >= 90 && x == 1\n  " + strings.Repeat(" ", 12) + "^\n"
	if errb != want {
		t.Errorf("stderr %q\nwant %q", errb, want)
	}
}

func TestCompileWarningsGoToStderr(t *testing.T) {
	isolateConfig(t)
	code, _, errb := runCLI(t, "compile", "iv > 100")
	if code != 0 || !strings.Contains(errb, "warning: the expression can never hold") {
		t.Errorf("exit %d stderr %q", code, errb)
	}
}

// A positioned warning gets the same caret lines as an error; the
// unpositioned "can never hold" warning does not.
func TestCompileWarningShowsCaret(t *testing.T) {
	isolateConfig(t)
	code, out, errb := runCLI(t, "compile", "iv >= 90 && gender in [9]")
	if code != 0 || out == "" {
		t.Fatalf("exit %d stdout %q", code, out)
	}
	want := "warning: 1:24: gender in [9]: 9 is outside gender's range -1..3 and is ignored\n" +
		"  iv >= 90 && gender in [9]\n  " + strings.Repeat(" ", 23) + "^\n" +
		"warning: the expression can never hold; the request matches nothing\n"
	if errb != want {
		t.Errorf("stderr %q\nwant %q", errb, want)
	}
}

func TestConfigFileAndFlagPrecedence(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir() // the explicit config lives outside the isolated lookup dirs
	path := filepath.Join(dir, "filterc.toml")
	os.WriteFile(path, []byte(`
listen = ":9999"
[golbat]
url = "http://golbat.example:9001"
secret = "abc"
[bounds]
min = { lat = 51.4, lon = -0.2 }
max = { lat = 51.6, lon = 0.1 }
limit = 300
`), 0o644)
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":9999" || cfg.Golbat.URL != "http://golbat.example:9001" || cfg.Golbat.Secret != "abc" ||
		cfg.Bounds.Min.Lat != 51.4 || cfg.Bounds.Max.Lon != 0.1 || cfg.Bounds.Limit != 300 {
		t.Errorf("config %+v", cfg)
	}
	code, out, errb := runCLI(t, "compile", "--json", "--config", path, "--max-lat", "52", "iv == 100")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb)
	}
	if want := `{"min":{"lat":51.4,"lon":-0.2},"max":{"lat":52,"lon":0.1},"limit":300,"filters":[{"iv":{"min":100,"max":100}}]}` + "\n"; out != want {
		t.Errorf("stdout %s\nwant %s", out, want)
	}
	if _, err := loadConfig(filepath.Join(dir, "missing.toml")); err == nil {
		t.Error("an explicit missing config file must be an error")
	}
	if cfg, err := loadConfig(""); err != nil || cfg.Listen != "127.0.0.1:8080" {
		t.Errorf("no config file: %+v %v", cfg, err)
	}
}

func TestConfigRejectsUnknownKeys(t *testing.T) {
	isolateConfig(t)
	path := filepath.Join(t.TempDir(), "filterc.toml")
	os.WriteFile(path, []byte(`
listen = "127.0.0.1:9999"
[golbat]
url = "http://golbat.example:9001"
secert = "abc"
`), 0o644)
	_, err := loadConfig(path)
	if want := "config " + path + ": unknown keys: golbat.secert"; err == nil || err.Error() != want {
		t.Errorf("err = %v, want %s", err, want)
	}
}

func TestScanCommandNeedsGolbat(t *testing.T) {
	isolateConfig(t)
	code, _, errb := runCLI(t, "scan", "iv == 100")
	if code != 1 || !strings.Contains(errb, "golbat url") {
		t.Errorf("exit %d stderr %q", code, errb)
	}
}
