package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import "sync"
import "sync/atomic"
import "../labrpc"
import "math/rand"
import "os"
import "time"
import "log"
import "io/ioutil"

import "bytes"
import "../labgob"

//
// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in Lab 3 you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh; at that point you can add fields to
// ApplyMsg, but set CommandValid to false for these other uses.
//
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int
}

// My implementation makes the effort to treat indexes as 0-based. This helps
// declaring and reading some variables given Go's 0-value rules, and to
// reason about the server's state on a fresh installation (server with no logs).
//
// However, and as far as I could grasp, the test code provided
// assumes 1-indexed arrays, meaning the very first log entry on a server has
// index number 1 and not 0 as most programmers would expect.
//
// This constant is used to mark the places where I need to convert indexes
// to comply with the test code.
const offset1IndexedArray = 1

type RaftState int

const (
	Leader RaftState = iota
	Follower
	Candidate
)

//
// A Go object implementing a single Raft peer.
//
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	volatile       Volatile
	volatileLeader VolatileLeader
	durable        Durable
}

type Volatile struct {
	commitIndex   int
	lastApplied   int
	electionTimer *time.Timer
	raftState     RaftState
	rand          *rand.Rand
	leaderId      int
	applyCh       chan ApplyMsg
}

type VolatileLeader struct {
	nextIndex       []int
	matchIndex      []int
	lastInteraction []time.Time
	failedRounds    []int
}

type Durable struct {
	currentTerm int
	votedFor    int
	log         []Log
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	var term int
	var isleader bool
	// Your code here (2A).

	rf.mu.Lock()
	defer rf.mu.Unlock()
	term = rf.durable.currentTerm
	isleader = rf.volatile.raftState == Leader

	return term, isleader
}

//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	// Your code here (2C).
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.durable.currentTerm)
	e.Encode(rf.durable.votedFor)
	e.Encode(rf.durable.log)
	data := w.Bytes()
	rf.persister.SaveRaftState(data)
}

//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (2C).
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var err error = nil

	err = d.Decode(&rf.durable.currentTerm)
	if err != nil {
		log.Fatalf("%d couldn't recover durable state: %v", rf.me, err)
	}
	err = d.Decode(&rf.durable.votedFor)
	if err != nil {
		log.Fatalf("%d couldn't recover durable state: %v", rf.me, err)
	}
	err = d.Decode(&rf.durable.log)
	if err != nil {
		log.Fatalf("%d couldn't recover durable state: %v", rf.me, err)
	}
}

func (rf *Raft) RequestVote(args *RequestVoteArgs, reply **RequestVoteReply) {
	// Your code here (2A, 2B).

	rf.mu.Lock()
	defer rf.mu.Unlock()

	log.Printf("%d received RequestVote from %d\n", rf.me, args.CandidateId)

	termIsNewer := args.Term > rf.durable.currentTerm
	alreadyVoted := args.Term == rf.durable.currentTerm && args.CandidateId == rf.durable.votedFor

	log.Printf("%d termIsNewer=%t alreadyVoted=%t", rf.me, termIsNewer, alreadyVoted)
	if termIsNewer || alreadyVoted {

		lastLogIdx, lastLogTerm := rf.lastLogIndexAndTerm()

		candidateLogTermIsGreater := args.LastLogTerm > lastLogTerm
		candidateLogTermIsEqual := args.LastLogTerm == lastLogTerm
		candidateLogIndexIsGTE := args.LastLogIndex >= lastLogIdx

		candidateLogIsUpToDate :=
			candidateLogTermIsGreater ||
				(candidateLogTermIsEqual && candidateLogIndexIsGTE)

		if candidateLogIsUpToDate {
			rf.resetElectionTimer(0)
			rf.durable.currentTerm = args.Term
			rf.durable.votedFor = args.CandidateId
			rf.volatile.raftState = Follower
			rf.persist()
		}

		log.Printf("%d candidateLogIsUpToDate=%t ", rf.me, candidateLogIsUpToDate)

		*reply = &RequestVoteReply{
			Term:        rf.durable.currentTerm,
			VoteGranted: candidateLogIsUpToDate,
		}
	} else {

		log.Printf("%d rejected RequestVote from %d", rf.me, args.CandidateId)

		*reply = &RequestVoteReply{
			Term:        rf.durable.currentTerm,
			VoteGranted: false,
		}
	}
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply **AppendEntriesReply) {

	log.Printf("%d receives AppendEntries from %d %v", rf.me, args.LeaderId, args)

	rf.mu.Lock()
	defer rf.mu.Unlock()

	lastLogIdx := rf.lastLogIndex()
	logConflict := args.PrevLogIndex > lastLogIdx ||
		(args.PrevLogIndex > -1 &&
			rf.durable.log[args.PrevLogIndex].Term != args.PrevLogTerm)

	changedState := false
	defer func() {
		if changedState {
			rf.persist()
		}
	}()

	if args.Term > rf.durable.currentTerm {
		rf.volatile.leaderId = args.LeaderId
		rf.volatile.raftState = Follower
		rf.durable.currentTerm = args.Term
		changedState = true
	}

	if args.Term < rf.durable.currentTerm {
		*reply = &AppendEntriesReply{
			Term:    rf.durable.currentTerm,
			Success: false,
		}

		log.Printf("%d rejected AppendEntries: stale term", rf.me)

	} else if logConflict {

		rf.resetElectionTimer(0)

		rewindTo := binSearchEarliest(rf.durable.log, args.PrevLogTerm, args.PrevLogIndex)
		rewindTo = max(rf.volatile.lastApplied+1, rewindTo)
		*reply = &AppendEntriesReply{
			Term:     rf.durable.currentTerm,
			Success:  false,
			RewindTo: rewindTo,
		}
		log.Printf("%d rejected AppendEntries: log conflict", rf.me)
	} else {

		rf.resetElectionTimer(0)

		indexToWrite := args.PrevLogIndex + 1
		notEndOfLog := indexToWrite < len(rf.durable.log)

		if notEndOfLog {
			// Prunes old entries that are on the end of the slice
			// but were not overriden.
			// This is necessary to prevent incorrect log size
			// on future rounds of RequestVote.
			rf.durable.log = rf.durable.log[:indexToWrite]
			changedState = true
		}

		if len(args.Entries) > 0 {
			log.Printf("%d appending new entries %v", rf.me, args.Entries)
			rf.durable.log = append(rf.durable.log, args.Entries...)
			changedState = true
		}
		log.Printf("%d log after appending %v %v", rf.me, args.Entries, rf.durable.log)

		if args.LeaderCommit > rf.volatile.commitIndex {
			rf.volatile.commitIndex = min(args.LeaderCommit, rf.lastLogIndex())
		}
		rf.applyPending()

		*reply = &AppendEntriesReply{
			Term:    rf.durable.currentTerm,
			Success: true,
		}
		log.Printf("%d accepted AppendEntries", rf.me)
	}
}

//
// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
//
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

//
// the service using Raft (e.g. a k/v server) wants to start
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
//
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := true

	// Your code here (2B).

	rf.mu.Lock()
	defer rf.mu.Unlock()
	index, term, isLeader = rf.appendCommand(command)

	log.Printf("%d append command %v %t", rf.me, command, isLeader)

	return index + offset1IndexedArray, term, isLeader
}

func (rf *Raft) appendCommand(command interface{}) (int, int, bool) {

	isLeader := rf.volatile.raftState == Leader

	if !isLeader {
		return -1, -1, false
	}

	l := Log{
		Term:    rf.durable.currentTerm,
		Command: command,
	}
	rf.durable.log = append(rf.durable.log, l)
	rf.persist()

	return rf.lastLogIndex(), l.Term, isLeader
}

//
// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
//
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

//
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
//
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Your initialization code here (2A, 2B, 2C).

	log.SetFlags(log.Lshortfile | log.Lmicroseconds)
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "DISABLE" {
		log.SetOutput(ioutil.Discard)
	}

	rf.volatile.commitIndex = -1
	rf.volatile.lastApplied = -1
	seed := randomInt64FromSystem()
	log.Printf("%d seed %d\n", rf.me, seed)
	rf.volatile.rand = rand.New(rand.NewSource(seed))
	rf.volatile.raftState = Candidate
	rf.volatile.leaderId = -1

	rf.volatileLeader.nextIndex = make([]int, len(peers))
	rf.volatileLeader.matchIndex = make([]int, len(peers))
	rf.volatileLeader.lastInteraction = make([]time.Time, len(peers))
	rf.volatileLeader.failedRounds = make([]int, len(peers))

	rf.volatile.applyCh = applyCh

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	rf.resetElectionTimer(0)
	log.Printf("%d initial election timer set", rf.me)

	go rf.heartbeatLoop()

	return rf
}

const heartbeatInterval time.Duration = time.Duration(5) * time.Millisecond
const requestTimeout time.Duration = time.Duration(10) * time.Millisecond
const maxIdleInterval time.Duration = time.Duration(30) * time.Millisecond

func (rf *Raft) heartbeatLoop() {
	for {
		time.Sleep(heartbeatInterval)
		if rf.killed() {
			return
		}

		rf.replicateLogs()
	}
}

func (rf *Raft) replicateLogs() {

	rf.mu.Lock()
	if rf.volatile.raftState != Leader {
		rf.mu.Unlock()
		return
	}
	rf.resetElectionTimer(0)

	log.Printf("%d looping heartbeat", rf.me)

	wg := sync.WaitGroup{}
	reqs := make([]AppendEntriesArgs, len(rf.peers))
	resps := make([]AppendEntriesReply, len(rf.peers))
	requestSuccess := make([]bool, len(rf.peers))
	logLengthToSend := make([]int, len(rf.peers))
	hadInteraction := make([]bool, len(rf.peers))

	type RequestResult struct {
		sent  bool
		ok    bool
		reply AppendEntriesReply
	}

	for i, _ := range rf.peers {
		if i != rf.me {
			wg.Add(1)

			completed := make(chan RequestResult, 1)

			icopy := i

			var prevLogIdx, prevLogTerm int
			var entries []Log
			if len(rf.durable.log) > 0 {
				nextIndex := rf.volatileLeader.nextIndex[icopy]
				prevLogIdx = nextIndex - 1

				if prevLogIdx == -1 {
					prevLogIdx = -1
					prevLogTerm = -1
				} else {
					prevLogTerm = rf.durable.log[prevLogIdx].Term
				}
				entries = make([]Log, len(rf.durable.log[nextIndex:]))
				copy(entries, rf.durable.log[nextIndex:])
				logLengthToSend[icopy] = len(entries)
			}
			log.Printf("%d entries to send %v", rf.me, entries)

			reqs[icopy] = AppendEntriesArgs{
				Term:         rf.durable.currentTerm,
				LeaderId:     rf.me,
				PrevLogIndex: prevLogIdx,
				PrevLogTerm:  prevLogTerm,
				Entries:      entries,
				LeaderCommit: rf.volatile.commitIndex,
			}

			go (func() {

				reply := AppendEntriesReply{}

				timeSinceLastInteraction :=
					time.Since(rf.volatileLeader.lastInteraction[icopy])

				noEntriesToSend := logLengthToSend[icopy] == 0
				talkedRecently := timeSinceLastInteraction <= maxIdleInterval

				if noEntriesToSend && talkedRecently {
					msg := "%d does not need to send AppendEntries to %d"
					log.Printf(msg, rf.me, icopy)
					completed <- RequestResult{false, false, reply}
					close(completed)
					return
				}

				log.Printf("%d sends AppendEntries to %d\n", rf.me, icopy)
				ok := rf.sendAppendEntries(icopy, &reqs[icopy], &reply)
				completed <- RequestResult{true, ok, reply}
				close(completed)
			})()
			go (func() {
				defer wg.Done()
				select {
				case result := <-completed:
					if result.sent {
						log.Printf("request ended ok %d -> %d\n", rf.me, icopy)
						requestSuccess[icopy] = result.ok
						resps[icopy] = result.reply
					}
					hadInteraction[icopy] = result.sent
				case <-time.After(requestTimeout):
					log.Printf("request timed out %d -> %d\n", rf.me, icopy)
					requestSuccess[icopy] = false
				}
			})()
		}
	}

	rf.mu.Unlock()

	wg.Wait()

	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.volatile.raftState != Leader {
		return
	}

	now := time.Now()
	for i, _ := range rf.volatileLeader.lastInteraction {
		if hadInteraction[i] {
			rf.volatileLeader.lastInteraction[i] = now
		}
	}

	agreements := 1
	highestDisagreeingTerm := -1
	unreachablePeers := 0
	for i, r := range resps {
		if i != rf.me && !requestSuccess[i] {
			rf.volatileLeader.failedRounds[i] += 1
			if rf.volatileLeader.failedRounds[i] > 10 {
				unreachablePeers += 1
			}
		}
		if i != rf.me && requestSuccess[i] {
			rf.volatileLeader.failedRounds[i] = 0
			log.Printf("%d response from %d %v", rf.me, i, r)
			if !r.Success {
				highestDisagreeingTerm = max(highestDisagreeingTerm, r.Term)
				if r.Term == rf.durable.currentTerm {

					before := rf.volatileLeader.nextIndex[i]
					rf.volatileLeader.nextIndex[i] =
						min(max(0, r.RewindTo), rf.lastLogIndex()+1)

					log.Printf("%d rewind nextIndex %d->%d", rf.me, before, rf.volatileLeader.nextIndex[i])
				}
			} else {
				agreements += 1
				rf.volatileLeader.nextIndex[i] += logLengthToSend[i]
				rf.volatileLeader.matchIndex[i] = rf.volatileLeader.nextIndex[i] - 1
			}
		}
	}

	log.Printf("%d agreements=%d", rf.me, agreements)

	if unreachablePeers >= rf.minVotesForQuorum() {
		log.Printf("%d detected network partition. Stepping down from Leader state", rf.me)
		rf.volatile.raftState = Follower
		rf.persist()
		return
	}

	if highestDisagreeingTerm > rf.durable.currentTerm {
		log.Printf("%d stepping down from Leader state", rf.me)
		rf.durable.currentTerm = highestDisagreeingTerm
		rf.volatile.raftState = Follower
		rf.persist()
	} else {
		log.Printf("%d matchIndex %v", rf.me, rf.volatileLeader.matchIndex)
		for n := rf.volatile.commitIndex + 1; n <= rf.lastLogIndex(); n += 1 {
			matches := 1
			for _, mi := range rf.volatileLeader.matchIndex {
				if mi >= n && rf.durable.log[n].Term == rf.durable.currentTerm {
					matches += 1
				}
			}
			if matches >= rf.minVotesForQuorum() {
				log.Printf("%d commit index update %d", rf.me, n)
				rf.volatile.commitIndex = n
			}
		}
		rf.applyPending()
	}
}

func (rf *Raft) applyPending() {

	for i := rf.volatile.lastApplied + 1; i <= rf.volatile.commitIndex; i += 1 {
		l := rf.durable.log[i]
		msg := ApplyMsg{
			CommandValid: true,
			Command:      l.Command,
			CommandIndex: i + offset1IndexedArray,
		}
		log.Printf("%d sending ApplyMsg %v", rf.me, msg)
		rf.volatile.applyCh <- msg
		log.Printf("%d applied", rf.me)
		rf.volatile.lastApplied = i
	}
}

func (rf *Raft) resetElectionTimer(priority int) {

	log.Printf("%d will reset timer", rf.me)

	if rf.volatile.electionTimer != nil {
		rf.volatile.electionTimer.Stop()
	}

	nextElectionTrigger :=
		time.Duration(rf.volatile.rand.Intn(80)+100+priority) *
			time.Millisecond

	if !rf.killed() {
		rf.volatile.electionTimer = time.AfterFunc(nextElectionTrigger, func() { rf.triggerElection(true, 0) })
		log.Printf("%d election timer reset to %v\n", rf.me, nextElectionTrigger)
	}
}

func (rf *Raft) triggerElection(increaseTerm bool, attempt int) {

	if rf.killed() {
		return
	}

	rf.mu.Lock()

	log.Printf("%d trigger election", rf.me)
	rf.volatile.raftState = Candidate
	if increaseTerm {
		rf.durable.currentTerm += 1
	}
	rf.durable.votedFor = rf.me
	rf.persist()

	lastLogIdx, lastLogTerm := rf.lastLogIndexAndTerm()

	req := RequestVoteArgs{
		Term:         rf.durable.currentTerm,
		CandidateId:  rf.me,
		LastLogIndex: lastLogIdx,
		LastLogTerm:  lastLogTerm,
	}
	wg := sync.WaitGroup{}

	resps := make([]RequestVoteReply, len(rf.peers))
	requestSuccess := make([]bool, len(rf.peers))

	type RequestResult struct {
		ok    bool
		reply RequestVoteReply
	}

	for i, _ := range rf.peers {
		if i != rf.me {
			wg.Add(1)

			completed := make(chan RequestResult, 1)

			icopy := i

			go (func() {
				reply := RequestVoteReply{}
				log.Printf("%d sends RequestVote to %d\n", rf.me, icopy)
				ok := rf.sendRequestVote(icopy, &req, &reply)
				completed <- RequestResult{ok, reply}
				close(completed)
			})()
			go (func() {
				defer wg.Done()
				select {
				case result := <-completed:
					requestSuccess[icopy] = result.ok
					resps[icopy] = result.reply
				case <-time.After(requestTimeout):
					log.Printf("request timed out %d -> %d\n", rf.me, icopy)
					requestSuccess[icopy] = false
				}
			})()
		}
	}

	rf.mu.Unlock()

	wg.Wait()

	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.volatile.raftState != Candidate {
		return
	}

	log.Printf("%d RequestVote round ended", rf.me)

	votes := 1
	highestDisagreeingTerm := -1
	failedReqs := 0
	for i, r := range resps {
		if i != rf.me && !requestSuccess[i] {
			failedReqs += 1
		}
		if i != rf.me && requestSuccess[i] {
			log.Printf("%d response from %d %v", rf.me, i, r)
			if r.VoteGranted {
				votes += 1
			} else {
				highestDisagreeingTerm =
					max(highestDisagreeingTerm, r.Term)
			}
		}
	}

	if failedReqs >= rf.minVotesForQuorum() {
		if attempt > 10 {
			log.Printf("%d detected network partition. Delaying RequestVote", rf.me)
			rf.resetElectionTimer(1000)
			return
		}
		go rf.triggerElection(false, attempt+1)
		return
	}

	if votes >= rf.minVotesForQuorum() {
		rf.volatile.raftState = Leader
		for i, _ := range rf.volatileLeader.nextIndex {
			rf.volatileLeader.nextIndex[i] = rf.lastLogIndex() + 1
		}
		for i, _ := range rf.volatileLeader.matchIndex {
			rf.volatileLeader.matchIndex[i] = -1
		}
		rf.volatileLeader.lastInteraction = make([]time.Time, len(rf.peers))
		rf.volatileLeader.failedRounds = make([]int, len(rf.peers))
		log.Printf("%d im leader", rf.me)
		log.Printf("%d reset nextIndex to %v ", rf.me, rf.volatileLeader.nextIndex)
	} else {
		if highestDisagreeingTerm > -1 {
			rf.durable.currentTerm = highestDisagreeingTerm
			rf.volatile.raftState = Follower
			log.Printf("%d im not leader votes=%d highestDisagreeingTerm=%d", rf.me, votes, highestDisagreeingTerm)
			rf.persist()
		}
	}

	rf.resetElectionTimer(0)
}

func (rf *Raft) lastLogIndex() int {
	return len(rf.durable.log) - 1
}

func (rf *Raft) lastLogIndexAndTerm() (int, int) {
	lastLogIdx := rf.lastLogIndex()
	var lastLogTerm int
	if len(rf.durable.log) > 0 {
		lastLogTerm = rf.durable.log[lastLogIdx].Term
	}

	return lastLogIdx, lastLogTerm
}

func (rf *Raft) minVotesForQuorum() int {

	return (len(rf.peers) / 2) + 1
}

func max(x, y int) int {
	if x > y {
		return x
	} else {
		return y
	}
}

func min(x, y int) int {
	if x < y {
		return x
	} else {
		return y
	}
}
