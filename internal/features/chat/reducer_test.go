package chat

import (
	"context"
	"errors"
	"testing"

	"openclaw-tui/internal/msg"
)

func TestReduce_LockErrorQueuesBackoffRetry(t *testing.T) {
	state := InitialState()
	state.ActiveMsgID = 1
	state.ActivePrompt = "hello"
	state.ActiveAttempt = 1
	state.Sending = true

	next, rr := Reduce(state, msg.ChatReplyMsg{
		Err:        errors.New("session file locked (timeout 10000ms): pid=1 /tmp/main.jsonl.lock"),
		Prompt:     "hello",
		MessageID:  1,
		Attempt:    1,
		MaxAttempt: MaxSendAttempts,
	})

	if rr.NeedSessionDiscover {
		t.Fatalf("lock errors should not trigger session rediscovery")
	}
	if rr.Cmd == nil {
		t.Fatalf("lock errors should schedule a retry cmd")
	}
	if next.PendingMsg != "hello" || next.PendingMsgID != 1 || next.PendingAttempt != 2 {
		t.Fatalf("pending retry not queued correctly: %+v", next)
	}
}

func TestReduce_NonLockErrorKeepsReconnectFlow(t *testing.T) {
	state := InitialState()
	state.ActiveMsgID = 1
	state.ActivePrompt = "hello"
	state.ActiveAttempt = 1
	state.Sending = true

	next, rr := Reduce(state, msg.ChatReplyMsg{
		Err:        errors.New("network blip"),
		Prompt:     "hello",
		MessageID:  1,
		Attempt:    1,
		MaxAttempt: MaxSendAttempts,
	})

	if !rr.NeedSessionDiscover {
		t.Fatalf("non-lock error should trigger session rediscovery")
	}
	if next.PendingAttempt != 2 {
		t.Fatalf("expected next pending attempt=2, got %d", next.PendingAttempt)
	}
}

func TestReduce_TimeoutDoesNotRetry(t *testing.T) {
	// A timeout may mean the message was already delivered to the agent.
	// We must NOT retry — that would send the message twice.
	for _, err := range []error{context.DeadlineExceeded, context.Canceled} {
		state := InitialState()
		state.ActiveMsgID = 1
		state.ActivePrompt = "hello"
		state.ActiveAttempt = 1
		state.Sending = true

		next, rr := Reduce(state, msg.ChatReplyMsg{
			Err:        err,
			Prompt:     "hello",
			MessageID:  1,
			Attempt:    1,
			MaxAttempt: MaxSendAttempts,
		})

		if rr.NeedSessionDiscover {
			t.Errorf("timeout should NOT trigger session rediscovery (%v)", err)
		}
		if rr.Cmd != nil {
			t.Errorf("timeout should NOT schedule a retry cmd (%v)", err)
		}
		if next.PendingMsg != "" {
			t.Errorf("timeout should NOT queue pending message (%v)", err)
		}
		if next.ActiveMsgID != 0 {
			t.Errorf("timeout should clear active state (%v)", err)
		}
	}
}
