package imap

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequest_CallsBeforeAndAfter(t *testing.T) {
	host, port, _ := startIMAPServer(t, false)

	var gotReq *Req
	var gotRes *Res
	_, err := Request(map[string]any{
		"host": host, "port": port, "username": "user", "password": "pass", "tls": false,
		"commands": []any{map[string]any{"name": "select", "mailbox": "INBOX"}},
	},
		WithBefore(func(req *Req) { gotReq = req }),
		WithAfter(func(res *Res) { gotRes = res }),
	)
	require.NoError(t, err)

	require.NotNil(t, gotReq, "before was not called")
	assert.Equal(t, host, gotReq.Host)
	assert.Equal(t, port, gotReq.Port)
	require.NotNil(t, gotRes, "after was not called")
	assert.Equal(t, 0, gotRes.Code)
}

func TestAction_LogsRequestAndResponseWithoutPassword(t *testing.T) {
	host, port, _ := startIMAPServer(t, false)

	var buf bytes.Buffer
	log := hclog.New(&hclog.LoggerOptions{Level: hclog.Trace, Output: &buf, JSONFormat: true})
	_, err := (&Action{log: log}).Run(map[string]any{
		"host": host, "port": port, "username": "user", "password": "pass", "tls": false,
		"commands": []any{map[string]any{"name": "select", "mailbox": "INBOX"}},
	})
	require.NoError(t, err)

	out := buf.String()
	records := map[string]map[string]any{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		var rec map[string]any
		require.NoError(t, json.Unmarshal(sc.Bytes(), &rec))
		msg, _ := rec["@message"].(string)
		records[msg] = rec
	}
	require.NoError(t, sc.Err())

	req, ok := records["imap request prepared"]
	require.True(t, ok, "the request was not logged: %s", out)
	params, _ := req["params"].(map[string]any)
	assert.Equal(t, host, params["host"])
	assert.Equal(t, "user", params["username"])
	assert.NotEmpty(t, params["password"], "the password key is kept, hidden")
	assert.NotEqual(t, "pass", params["password"], "the password is logged unmasked")

	res, ok := records["imap response received"]
	require.True(t, ok, "the response was not logged: %s", out)
	assert.Contains(t, res["params"], "data")
}
