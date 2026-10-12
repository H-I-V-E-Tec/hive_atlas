package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/H-I-V-E-Tec/hive_atlas/internal/core"
	"github.com/H-I-V-E-Tec/hive_atlas/internal/transport"
	"github.com/H-I-V-E-Tec/hive_atlas/signals"
)

var Version = "v2.0.0-dev"
var Revision = ""
var DefaultCenterURL = "https://hive-center.com.br"

func hiveHome() string {
	if v := os.Getenv("HIVE_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hive")
}
func tokenPath() string {
	if v := os.Getenv("HIVE_TOKEN_FILE"); v != "" {
		return v
	}
	return filepath.Join(hiveHome(), "token")
}
func centerURL() (string, error) {
	value := os.Getenv("HIVE_CENTER_URL")
	if value == "" {
		if b, err := os.ReadFile(filepath.Join(hiveHome(), "center-url")); err == nil {
			value = strings.TrimSpace(string(b))
		}
	}
	if value == "" {
		value = DefaultCenterURL
	}
	if strings.TrimRight(value, "/") == "https://hive-center.duckdns.org" {
		return "", fmt.Errorf("HIVE Center moved to %s; update HIVE_CENTER_URL or run hive login --center-url %s", DefaultCenterURL, DefaultCenterURL)
	}
	return transport.ValidateURL(value)
}

func atlasURL(center string) (string, error) {
	value := os.Getenv("HIVE_ATLAS_URL")
	if value == "" {
		value = center + "/atlas"
	}
	if strings.TrimRight(value, "/") == "https://hive-center.duckdns.org/atlas" {
		return "", fmt.Errorf("Hive Atlas moved to %s/atlas; update HIVE_ATLAS_URL or repeat hive setup atlas", DefaultCenterURL)
	}
	return transport.ValidateURL(value)
}

func localLibrary(path string) (*core.Library, error) {
	data := signals.Core
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		data = b
	}
	return core.Load(data)
}
func backend() (transport.Backend, error) {
	if os.Getenv("ATLAS_OFFLINE") == "1" {
		lib, err := localLibrary(os.Getenv("ATLAS_LIBRARY"))
		return transport.Local{Lib: lib}, err
	}
	center, err := centerURL()
	if err != nil {
		return nil, err
	}
	url, err := atlasURL(center)
	if err != nil {
		return nil, err
	}
	client, err := transport.HTTPClient("")
	if err != nil {
		return nil, err
	}
	return transport.Remote{URL: url, TokenFile: tokenPath(), Client: client}, nil
}

func remoteMCP() (*RemoteMCP, error) {
	b, err := backend()
	if err != nil {
		return nil, err
	}
	r, ok := b.(transport.Remote)
	if !ok {
		return nil, fmt.Errorf("remote MCP requires an online session")
	}
	if _, err := r.Token(); err != nil {
		return nil, err
	}
	return &RemoteMCP{Remote: r}, nil
}

func privateWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".atlas-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
