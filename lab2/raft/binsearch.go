package raft

// binSearchEarliest finds the *first* log entry with term T.
// If no term T exists in the logs, it will find the first
// log entry that has the highest term less than T
// If T is less than the lowest term in then logs, returns -1
func binSearchEarliest(logs []Log, t int, upperIndex int) int {

	if len(logs) == 0 {
		return -1
	}

	i := 0
	j := min(len(logs)-1, max(0, upperIndex))

	if t < logs[i].Term {
		return -1
	}

	last_m := -1
	for {
		if t == logs[j].Term {
			if j == 0 || logs[j-1].Term < t {
				return j
			}
		}

		m := (i + j) / 2
		if m == last_m {
			// didn't find any entry with term t
			// try the preceding term number
			// which is guaranteed to exist
			return binSearchEarliest(logs, logs[i].Term, upperIndex)
		}
		if t <= logs[m].Term {
			j = m
		} else {
			i = m
		}
		last_m = m
	}
	return j
}
