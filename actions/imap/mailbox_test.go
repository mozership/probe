package imap

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listedNames returns the names of the mailboxes the last list found.
func listedNames(t *testing.T, data map[string]any) []string {
	t.Helper()
	var names []string
	for _, mb := range data["list"].(map[string]any)["mailboxes"].([]any) {
		names = append(names, mb.(map[string]any)["name"].(string))
	}
	return names
}

// TestMailboxCommands runs list, create, delete, rename, subscribe and
// unsubscribe against a server holding only INBOX, each on a server of its
// own, since they change it.
func TestMailboxCommands(t *testing.T) {
	cmd := func(name string, kv ...string) map[string]any {
		c := map[string]any{"name": name}
		for i := 0; i+1 < len(kv); i += 2 {
			c[kv[i]] = kv[i+1]
		}
		return c
	}
	list := cmd("list")

	tests := []struct {
		name     string
		commands []map[string]any
		// wantErr is part of res.error, for a run that fails with res.code 1.
		wantErr string
		// entry is the res.data entry to compare with want.
		entry string
		want  map[string]any
		// listed is what a list at the end of the commands finds.
		listed []string
	}{
		{
			name:     "list finds every mailbox without a pattern",
			commands: []map[string]any{list},
			listed:   []string{"INBOX"},
		},
		{
			name:     "list keeps to the pattern",
			commands: []map[string]any{cmd("create", "mailbox", "Work"), cmd("list", "pattern", "W*")},
			listed:   []string{"Work"},
		},
		{
			name:     "create",
			commands: []map[string]any{cmd("create", "mailbox", "Work"), list},
			entry:    "create",
			want:     map[string]any{"success": true, "mailbox": "Work"},
			listed:   []string{"INBOX", "Work"},
		},
		{
			name:     "create without a name",
			commands: []map[string]any{cmd("create")},
			wantErr:  "mailbox name is required for CREATE command",
		},
		{
			name:     "create a mailbox that exists",
			commands: []map[string]any{cmd("create", "mailbox", "INBOX")},
			wantErr:  "failed to Create mailbox INBOX",
		},
		{
			name:     "delete",
			commands: []map[string]any{cmd("create", "mailbox", "Work"), cmd("delete", "mailbox", "Work"), list},
			entry:    "delete",
			want:     map[string]any{"success": true, "mailbox": "Work"},
			listed:   []string{"INBOX"},
		},
		{
			name:     "delete without a name",
			commands: []map[string]any{cmd("delete")},
			wantErr:  "mailbox name is required for DELETE command",
		},
		{
			name:     "delete a mailbox that does not exist",
			commands: []map[string]any{cmd("delete", "mailbox", "Nowhere")},
			wantErr:  "failed to Delete mailbox Nowhere",
		},
		{
			name: "rename",
			commands: []map[string]any{
				cmd("create", "mailbox", "Old"),
				cmd("rename", "oldmailbox", "Old", "newmailbox", "New"),
				list,
			},
			entry:  "rename",
			want:   map[string]any{"success": true, "old_mailbox": "Old", "new_mailbox": "New"},
			listed: []string{"INBOX", "New"},
		},
		{
			name:     "rename without a new name",
			commands: []map[string]any{cmd("rename", "oldmailbox", "INBOX")},
			wantErr:  "both old and new mailbox names are required for RENAME command",
		},
		{
			name:     "rename a mailbox that does not exist",
			commands: []map[string]any{cmd("rename", "oldmailbox", "Nowhere", "newmailbox", "New")},
			wantErr:  "failed to Rename mailbox from Nowhere to New",
		},
		{
			name:     "subscribe",
			commands: []map[string]any{cmd("subscribe", "mailbox", "INBOX")},
			entry:    "subscribe",
			want:     map[string]any{"success": true, "mailbox": "INBOX"},
		},
		{
			name:     "subscribe without a name",
			commands: []map[string]any{cmd("subscribe")},
			wantErr:  "mailbox name is required for SUBSCRIBE command",
		},
		{
			name:     "subscribe to a mailbox that does not exist",
			commands: []map[string]any{cmd("subscribe", "mailbox", "Nowhere")},
			wantErr:  "failed to Subscribe to mailbox Nowhere",
		},
		{
			name:     "unsubscribe",
			commands: []map[string]any{cmd("subscribe", "mailbox", "INBOX"), cmd("unsubscribe", "mailbox", "INBOX")},
			entry:    "unsubscribe",
			want:     map[string]any{"success": true, "mailbox": "INBOX"},
		},
		{
			name:     "unsubscribe without a name",
			commands: []map[string]any{cmd("unsubscribe")},
			wantErr:  "mailbox name is required for UNSUBSCRIBE command",
		},
		{
			name:     "unsubscribe from a mailbox that does not exist",
			commands: []map[string]any{cmd("unsubscribe", "mailbox", "Nowhere")},
			wantErr:  "failed to Unsubscribe from mailbox Nowhere",
		},
		{
			// This pins behavior that has not changed: the failed command
			// stops the run and leaves its entry with what the command of
			// the same name before it returned. The Success: false answer
			// a failure used to build was never stored, so dropping it
			// makes no difference here.
			name:     "a failed command leaves the entry of the one before it",
			commands: []map[string]any{cmd("create", "mailbox", "Work"), cmd("create"), cmd("delete", "mailbox", "Work")},
			wantErr:  "failed to execute command 2 (create): mailbox name is required",
			entry:    "create",
			want:     map[string]any{"success": true, "mailbox": "Work"},
		},
		{
			name:     "an unknown command",
			commands: []map[string]any{cmd("lsub")},
			wantErr:  "failed to execute command 1: unsupported command 'lsub'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port, _ := startIMAPServer(t, false)
			ret, err := runCommands(t, host, port, tt.commands...)
			require.NoError(t, err, "a failed command is not an action error")

			res := ret["res"].(map[string]any)
			data := res["data"].(map[string]any)
			if tt.wantErr != "" {
				assert.Equal(t, 1, res["code"])
				assert.Contains(t, res["error"], tt.wantErr)
			} else {
				require.Equal(t, 0, res["code"], res["error"])
			}
			if tt.want != nil {
				entry := data[tt.entry].(map[string]any)
				for k, v := range tt.want {
					assert.Equal(t, v, entry[k], "%s.%s", tt.entry, k)
				}
			}
			if tt.listed != nil {
				assert.ElementsMatch(t, tt.listed, listedNames(t, data))
				assert.Equal(t, len(tt.listed), data["list"].(map[string]any)["count"])
			}
		})
	}
}

// TestMailboxCommands_SuccessIsFalseForACommandThatDidNotRun covers the only
// way success is false: the entry of a command that did not run is left at
// zero values.
func TestMailboxCommands_SuccessIsFalseForACommandThatDidNotRun(t *testing.T) {
	host, port, _ := startIMAPServer(t, false)
	ret, err := runCommands(t, host, port, map[string]any{"name": "noop"})
	require.NoError(t, err)
	data := ret["res"].(map[string]any)["data"].(map[string]any)
	assert.Equal(t, true, data["noop"].(map[string]any)["success"])
	for _, entry := range []string{"create", "delete", "rename", "subscribe", "unsubscribe"} {
		assert.Equal(t, false, data[entry].(map[string]any)["success"], entry)
	}
}

// TestRequest_ConnectionParamsAsStrings covers port, tls and
// insecure_skip_tls given as strings, as a template expands them, which
// MapToStructByTags converts.
func TestRequest_ConnectionParamsAsStrings(t *testing.T) {
	host, port, _ := startIMAPServer(t, false)

	with := map[string]any{
		"host": host, "port": strconv.Itoa(port), "username": "user", "password": "pass",
		"tls": "false", "insecure_skip_tls": "true", "timeout": "5s",
		"commands": []any{map[string]any{"name": "noop"}},
	}
	ret, err := Request(with)
	require.NoError(t, err)
	req := ret["req"].(map[string]any)
	assert.Equal(t, port, req["port"])
	assert.Equal(t, false, req["tls"])
	assert.Equal(t, true, req["insecure_skip_tls"])
	assert.Equal(t, "5s", with["timeout"], "the caller's map is left as it was")

	for key, bad := range map[string]string{"port": "imap", "tls": "maybe", "insecure_skip_tls": "maybe"} {
		with := map[string]any{"host": host, "port": port, "username": "user", "password": "pass", "tls": false}
		with[key] = bad
		_, err := Request(with)
		require.Error(t, err, key)
		assert.Contains(t, err.Error(), "invalid imap parameters", key)
		assert.Contains(t, err.Error(), "'"+key+"'", key)
	}
}
