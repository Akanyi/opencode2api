package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMultipleProxyFiles(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"first.txt":  "http://one:80\nhttp://two:80 # comment\n",
		"second.txt": "http://two:80\nhttp://three:80#pool\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := Config{Proxies: []string{"http://one:80"}, ProxyFile: "first.txt", ProxyFiles: []string{"first.txt", "second.txt"}}
	if err := resolveProxyFiles(filepath.Join(dir, "config.json"), &cfg); err != nil {
		t.Fatal(err)
	}
	want := []string{"http://one:80", "http://two:80", "http://three:80#pool"}
	if !reflect.DeepEqual(cfg.RuntimeProxies(), want) {
		t.Fatalf("got %v, want %v", cfg.RuntimeProxies(), want)
	}
	if !reflect.DeepEqual(cfg.Proxies, []string{"http://one:80"}) {
		t.Fatal("external entries leaked into inline configuration")
	}
	cfg.ProxyFiles = append(cfg.ProxyFiles, "missing.txt")
	if err := resolveProxyFiles(filepath.Join(dir, "config.json"), &cfg); err == nil {
		t.Fatal("missing file must reject configuration")
	}
	if err := os.WriteFile(filepath.Join(dir, "empty.txt"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := resolveProxyFiles(filepath.Join(dir, "config.json"), &Config{ProxyFiles: []string{"empty.txt"}}); err == nil {
		t.Fatal("empty configured files must not silently enable direct access")
	}
}

func TestProxyCSV(t *testing.T) {
	entries, err := ParseProxyContent("export.csv", strings.NewReader("\ufeffIP,Port,User,Pass\n127.0.0.1,8080,u,p@ss\n::1,8081,u,p\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"http://u:p%40ss@127.0.0.1:8080", "http://u:p@[::1]:8081"}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("got %v, want %v", entries, want)
	}
}
