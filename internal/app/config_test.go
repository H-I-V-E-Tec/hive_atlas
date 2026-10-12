package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/H-I-V-E-Tec/hive_atlas/internal/transport"
)

func TestDefaultCenterAndAtlasURL(t *testing.T) {
	t.Setenv("HIVE_HOME", t.TempDir())
	t.Setenv("HIVE_CENTER_URL", "")
	t.Setenv("HIVE_ATLAS_URL", "")
	t.Setenv("ATLAS_OFFLINE", "")

	center, err := centerURL()
	if err != nil || center != "https://hive-center.com.br" {
		t.Fatalf("centerURL() = %q, %v", center, err)
	}
	b, err := backend()
	if err != nil {
		t.Fatal(err)
	}
	remote, ok := b.(transport.Remote)
	if !ok || remote.URL != "https://hive-center.com.br/atlas" {
		t.Fatalf("backend() = %#v", b)
	}
}

func TestSavedCenterURLRemainsConfigurable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HIVE_HOME", home)
	t.Setenv("HIVE_CENTER_URL", "")
	if err := os.WriteFile(filepath.Join(home, "center-url"), []byte("https://center.example.test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	center, err := centerURL()
	if err != nil || center != "https://center.example.test" {
		t.Fatalf("centerURL() = %q, %v", center, err)
	}
}

func TestRetiredCenterURLExplainsMigration(t *testing.T) {
	t.Setenv("HIVE_HOME", t.TempDir())
	t.Setenv("HIVE_CENTER_URL", "https://hive-center.duckdns.org")
	_, err := centerURL()
	if err == nil || !strings.Contains(err.Error(), "https://hive-center.com.br") {
		t.Fatalf("centerURL() error = %v", err)
	}
}

func TestRetiredAtlasURLExplainsMigration(t *testing.T) {
	t.Setenv("HIVE_ATLAS_URL", "https://hive-center.duckdns.org/atlas")
	_, err := atlasURL("https://hive-center.com.br")
	if err == nil || !strings.Contains(err.Error(), "https://hive-center.com.br/atlas") {
		t.Fatalf("atlasURL() error = %v", err)
	}
}
