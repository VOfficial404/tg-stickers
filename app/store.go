package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

type prefs struct {
	Server   string `json:"server"`
	Remember bool   `json:"remember"`
	Fit      string `json:"fit"`
}

// store 保存连接信息；令牌放在系统凭据管理器里
type store interface {
	load() prefs
	save(prefs) error
	token() (string, error)
	setToken(string) error
}

type systemStore struct{}

func (systemStore) dir() (string, error) { return mygo.App.Path(mygo.PathUserData) }
func (s systemStore) load() prefs {
	p := prefs{Server: "http://127.0.0.1:8787"}
	if d, e := s.dir(); e == nil {
		if b, e := os.ReadFile(filepath.Join(d, "prefs.json")); e == nil {
			_ = json.Unmarshal(b, &p)
		}
	}
	return p
}
func (s systemStore) save(p prefs) error {
	d, e := s.dir()
	if e != nil {
		return e
	}
	if e = os.MkdirAll(d, 0o700); e != nil {
		return e
	}
	b, e := json.MarshalIndent(p, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(d, "prefs.json"), b, 0o600)
}
func (systemStore) token() (string, error) {
	b, e := mygo.Secrets.Get("server-token")
	return string(b), e
}
func (systemStore) setToken(t string) error {
	if t != "" {
		return mygo.Secrets.Set("server-token", []byte(t))
	}
	e := mygo.Secrets.Delete("server-token")
	if errors.Is(e, mygo.ErrSecretNotFound) {
		return nil
	}
	return e
}
