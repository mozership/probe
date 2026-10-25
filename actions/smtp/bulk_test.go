package smtp

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestBulkDeliverWithResult(t *testing.T) {
	tests := []struct {
		name             string
		session          int
		message          int
		expectedSent     int
		expectedSessions int
	}{
		{
			name:             "Single session, single message",
			session:          1,
			message:          1,
			expectedSent:     1,
			expectedSessions: 1,
		},
		{
			name:             "Single session, multiple messages",
			session:          1,
			message:          5,
			expectedSent:     5,
			expectedSessions: 1,
		},
		{
			name:             "Multiple sessions, multiple messages",
			session:          3,
			message:          10,
			expectedSent:     10,
			expectedSessions: 3,
		},
		{
			name:             "Sessions equal to messages",
			session:          5,
			message:          5,
			expectedSent:     5,
			expectedSessions: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Start mock SMTP server
			addr := startMockServer(t, false)

			bulk := &Bulk{
				Addr:       addr,
				From:       "test@example.com",
				To:         "recipient@example.com",
				Subject:    "Test",
				MyHostname: "test.local",
				Session:    tt.session,
				Message:    tt.message,
				Length:     100,
			}

			result := bulk.DeliverWithResult()

			// Verify sent message count
			if result.Sent != tt.expectedSent {
				t.Errorf("Expected Sent = %d, got %d", tt.expectedSent, result.Sent)
			}

			// Verify session count
			if result.Sessions != tt.expectedSessions {
				t.Errorf("Expected Sessions = %d, got %d", tt.expectedSessions, result.Sessions)
			}

			// Verify total equals sent (when no failures)
			if result.Total != tt.expectedSent {
				t.Errorf("Expected Total = %d, got %d", tt.expectedSent, result.Total)
			}

			// Verify no failures
			if result.Failed != 0 {
				t.Errorf("Expected Failed = 0, got %d", result.Failed)
			}

			// Verify no error
			if result.Error != "" {
				t.Errorf("Expected no error, got: %s", result.Error)
			}
		})
	}
}

func TestBulkSendShare(t *testing.T) {
	tests := []struct {
		name          string
		session       int
		message       int
		expectedCount int
	}{
		{
			name:          "Single message per session",
			session:       1,
			message:       1,
			expectedCount: 1,
		},
		{
			name:          "Multiple messages per session",
			session:       1,
			message:       5,
			expectedCount: 5,
		},
		{
			name:          "Evenly distributed messages",
			session:       3,
			message:       9,
			expectedCount: 3, // Each session sends 3 messages
		},
		{
			name:          "Unevenly distributed messages",
			session:       3,
			message:       10,
			expectedCount: 4, // First session sends 4, others send 3
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Start mock SMTP server
			addr := startMockServer(t, false)

			bulk := &Bulk{
				Addr:       addr,
				From:       "test@example.com",
				To:         "recipient@example.com",
				Subject:    "Test",
				MyHostname: "test.local",
				Session:    tt.session,
				Message:    tt.message,
				Length:     100,
			}

			count, err := bulk.sendShare(bulk.makeData())

			// Verify no error
			if err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			// Verify count is within expected range
			// For the first goroutine, it should return the expected count
			// Note: This test only verifies a single sendShare() call
			if count < 1 || count > tt.expectedCount {
				t.Errorf("Expected count between 1 and %d, got %d", tt.expectedCount, count)
			}
		})
	}
}

func TestCalcMessageNumEachSessionTotal(t *testing.T) {
	// However the messages divide, the sessions together get exactly
	// message of them.
	tests := []struct {
		session, message int
		want             []int
	}{
		{session: 5, message: 1, want: []int{1, 0, 0, 0, 0}},
		{session: 4, message: 2, want: []int{1, 1, 0, 0}},
		{session: 3, message: 7, want: []int{3, 3, 1}},
		{session: 2, message: 3, want: []int{2, 1}},
		{session: 3, message: 3, want: []int{1, 1, 1}},
		{session: 2, message: 0, want: []int{0, 0}},
		// Rounding up does not overflow.
		{session: 3, message: math.MaxInt, want: []int{math.MaxInt/3 + 1, math.MaxInt/3 + 1, math.MaxInt - 2*(math.MaxInt/3+1)}},
	}

	for _, tt := range tests {
		b := &Bulk{Session: tt.session, Message: tt.message}
		var got []int
		for i := 0; i < tt.session; i++ {
			got = append(got, b.calcMessageNumEachSession())
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("session %d, message %d: got %v, want %v", tt.session, tt.message, got, tt.want)
		}
	}
}

func TestSplitRecipients(t *testing.T) {
	tests := map[string][]string{
		"a@example.com":                    {"a@example.com"},
		"a@example.com,b@example.com":      {"a@example.com", "b@example.com"},
		"a@example.com, b@example.com":     {"a@example.com", "b@example.com"},
		" a@example.com ,, b@example.com ": {"a@example.com", "b@example.com"},
		"":                                 nil,
	}
	for in, want := range tests {
		if got := splitRecipients(in); !reflect.DeepEqual(got, want) {
			t.Errorf("splitRecipients(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMakeData(t *testing.T) {
	// The body is a line of text and length asterisks in lines of 80.
	for length, want := range map[int]string{
		0:   "\n",
		3:   "***\n",
		80:  strings.Repeat("*", 80) + "\n",
		81:  strings.Repeat("*", 80) + "\n*\n",
		160: strings.Repeat("*", 80) + "\n" + strings.Repeat("*", 80) + "\n",
	} {
		b := &Bulk{From: "f@example.com", To: "t@example.com", Subject: "hi", Length: length}
		got := string(b.makeData())
		head, body, ok := strings.Cut(got, "\n\nThis is a test mail.\n\n")
		if !ok {
			t.Fatalf("length %d: makeData() = %q, want the line of text after the headers", length, got)
		}
		if !strings.HasPrefix(head, "From: f@example.com\nTo: t@example.com\nDate: ") || !strings.HasSuffix(head, "\nSubject: hi") {
			t.Errorf("length %d: headers = %q", length, head)
		}
		if body != want {
			t.Errorf("length %d: body = %q, want %q", length, body, want)
		}
	}
}
