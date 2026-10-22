package raft

import "testing"

func TestBinSearch(t *testing.T) {

	l := []Log{
		Log{0, 0},
		Log{0, 0},
		Log{0, 0},
		Log{1, 0},
		Log{2, 0},
		Log{2, 0},
		Log{4, 0},
		Log{4, 0},
		Log{4, 0},
		Log{4, 0},
		Log{4, 0},
	}

	if binSearchEarliest(l, 99) != -1 {
		t.Errorf("inexistent entry")
	}

	if binSearchEarliest(l, -99) != -1 {
		t.Errorf("inexistent entry")
	}

	x := binSearchEarliest(l, 1)
	if x != 3 {
		t.Errorf("entry with term 1 is not on index %d", x)
	}

	x = binSearchEarliest(l, 4)
	if x != 6 {
		t.Errorf("entry with term 4 is not on index %d", x)
	}

	if binSearchEarliest(l, 3) != 4 {
		t.Errorf("inexistent term 3 should return earliest index of term 2")
	}

	large := 1_000_000
	l2 := make([]Log, large)
	i := 0
	for i < 10 {
		l2[i] = Log{0, 0}
		i += 1
	}
	for i < large-10 {
		l2[i] = Log{33, 0}
		i += 1
	}
	for i < large {
		l2[i] = Log{99, 0}
		i += 1
	}

	x = binSearchEarliest(l2, 33)
	if x != 10 {
		t.Errorf("entry with term 33 is not on index %d", x)
	}
	x = binSearchEarliest(l2, 34)
	if x != 10 {
		t.Error("inexistent term 34 should return earliest index of term 33", x)
	}
	x = binSearchEarliest(l2, 99)
	if x != large-10 {
		t.Errorf("entry with term 99 is not on index %d", large-10)
	}
}
