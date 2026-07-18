package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestListEventsReconnectableBySequence(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "events.sqlite")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()
	seedRun(t, s, "run-1")
	if err := s.Transition(ctx, "run-1", StateRunning, "launch"); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if _, err := s.AppendEvent(ctx, "run-1", "started", []byte(`{"v":1}`), 10); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if _, err := s.AppendEvent(ctx, "run-1", "progress", []byte(`{"n":1}`), 20); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	if _, err := s.AppendEvent(ctx, "run-1", "progress", []byte(`{"n":2}`), 30); err != nil {
		t.Fatalf("append 3: %v", err)
	}

	// Full read from the start.
	all, err := s.ListEvents(ctx, "run-1", 0)
	if err != nil {
		t.Fatalf("ListEvents 0: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("len(ListEvents 0) = %d, want 3", len(all))
	}
	for i, e := range all {
		if e.Seq != int64(i+1) {
			t.Errorf("all[%d].Seq = %d, want %d", i, e.Seq, i+1)
		}
	}

	// A reader that last consumed seq 1 resumes and sees only seqs 2..3.
	after1, err := s.ListEvents(ctx, "run-1", 1)
	if err != nil {
		t.Fatalf("ListEvents 1: %v", err)
	}
	if len(after1) != 2 || after1[0].Seq != 2 || after1[1].Seq != 3 {
		t.Errorf("ListEvents(1) = %v, want seqs [2 3]", seqsOf(after1))
	}

	// A reader caught up to the tail sees nothing new.
	after3, err := s.ListEvents(ctx, "run-1", 3)
	if err != nil {
		t.Fatalf("ListEvents 3: %v", err)
	}
	if len(after3) != 0 {
		t.Errorf("ListEvents(3) = %v, want empty", seqsOf(after3))
	}

	// A high unsigned cursor (well above any real sequence) yields no events
	// and no error: the cursor is an opaque uint64 high-water mark, never
	// wrapping or special-cased.
	high, err := s.ListEvents(ctx, "run-1", int64(1)<<62)
	if err != nil {
		t.Fatalf("ListEvents high cursor: %v", err)
	}
	if len(high) != 0 {
		t.Errorf("ListEvents(high) = %v, want empty", seqsOf(high))
	}

	// Reconnect: close the store, reopen the same database, and resume from
	// the last consumed sequence. Events and sequences must be stable and
	// complete — the journal is the reconnect source of truth.
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	resumed, err := s2.ListEvents(ctx, "run-1", 1)
	if err != nil {
		t.Fatalf("ListEvents after reopen: %v", err)
	}
	if len(resumed) != 2 || resumed[0].Seq != 2 || resumed[1].Seq != 3 {
		t.Errorf("after reopen ListEvents(1) = %v, want seqs [2 3]", seqsOf(resumed))
	}
	// The committed offset also survives restart (no reset on reconnect).
	off, err := s2.CommittedOffset(ctx, "run-1")
	if err != nil {
		t.Fatalf("CommittedOffset after reopen: %v", err)
	}
	if off != 30 {
		t.Errorf("committed offset after reopen = %d, want 30 (must not reset)", off)
	}
}

func seqsOf(events []Event) []int64 {
	out := make([]int64, len(events))
	for i, e := range events {
		out[i] = e.Seq
	}
	return out
}
