package db

import (
	"path/filepath"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/internal/actiontest"
)

func TestFieldsDeclareWhatItReturns(t *testing.T) {
	kinds := actiontest.NewKinds(t, Spec.ReqTypes, Spec.ResTypes)
	dsn := "file:" + filepath.Join(t.TempDir(), "test.db")
	a := &Action{log: hclog.NewNullLogger()}
	for _, query := range []string{
		"CREATE TABLE users (id INTEGER)",
		"INSERT INTO users (id) VALUES (1)",
		"SELECT id FROM users",
		"SELECT * FROM missing_table",
	} {
		with := map[string]any{"dsn": dsn, "query": query, "params": []any{}, "timeout": "5s"}
		ret, err := a.Run(with)
		if err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		actiontest.CheckFields(t, Spec.ReqFields, Spec.ResFields, nil, ret)
		kinds.Check(ret)
	}
	kinds.Covered()
}

func TestRequired(t *testing.T) {
	a := &Action{log: hclog.NewNullLogger()}
	with := map[string]any{"dsn": "file:" + filepath.Join(t.TempDir(), "test.db"), "query": "SELECT 1"}
	actiontest.CheckRequired(t, Spec.Required, with, func(with map[string]any) error {
		_, err := a.Run(with)
		return err
	})
}
