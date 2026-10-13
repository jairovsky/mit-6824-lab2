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
import "encoding/binary"
import "io/ioutil"

// import "bytes"
// import "../labgob"

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
	currentTerm    int
	votedFor       int
	log            []Log
	volatile       Volatile
	volatileLeader VolatileLeader
}

type Volatile struct {
	commitIndex   int
	lastApplied   int
	electionTimer *time.Timer
	raftState     RaftState
	rand          *rand.Rand
	leaderId      int
}

type VolatileLeader struct {
	nextIndex  []int
	matchIndex []int
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	var term int
	var isleader bool
	// Your code here (2A).

	rf.mu.Lock()
	defer rf.mu.Unlock()
	term = rf.currentTerm
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
	// Example:
	// w := new(bytes.Buffer)
	// e := labgob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// data := w.Bytes()
	// rf.persister.SaveRaftState(data)
}

//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (2C).
	// Example:
	// r := bytes.NewBuffer(data)
	// d := labgob.NewDecoder(r)
	// var xxx
	// var yyy
	// if d.Decode(&xxx) != nil ||
	//    d.Decode(&yyy) != nil {
	//   error...
	// } else {
	//   rf.xxx = xxx
	//   rf.yyy = yyy
	// }
}

//
// example RequestVote RPC handler.
//
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply **RequestVoteReply) {
	// Your code here (2A, 2B).

	rf.mu.Lock()
	defer rf.mu.Unlock()

	rf.resetElectionTimer()

	log.Printf("%d received RequestVote from %d\n", rf.me, args.CandidateId)

	termIsNewer := args.Term > rf.currentTerm
	alreadyVoted := args.Term == rf.currentTerm && args.CandidateId == rf.votedFor

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
			rf.currentTerm = args.Term
			rf.votedFor = args.CandidateId
			rf.volatile.raftState = Follower
		}

		log.Printf("%d candidateLogIsUpToDate=%t ", rf.me, candidateLogIsUpToDate)

		*reply = &RequestVoteReply{
			Term:        rf.currentTerm,
			VoteGranted: candidateLogIsUpToDate,
		}
	} else {
		*reply = &RequestVoteReply{
			Term:        rf.currentTerm,
			VoteGranted: false,
		}
	}
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply **AppendEntriesReply) {

	rf.mu.Lock()
	defer rf.mu.Unlock()

	rf.resetElectionTimer()

	if len(args.Entries) > 0 {
		panic("Phase 2A: Should not be receiving log entries yet")
	}

	log.Printf("%d receives AppendEntries from %d", rf.me, args.LeaderId)

	if args.Term < rf.currentTerm {
		*reply = &AppendEntriesReply{
			Term:    rf.currentTerm,
			Success: false,
		}
	} else {
		rf.volatile.leaderId = args.LeaderId
		rf.volatile.raftState = Follower
		rf.currentTerm = args.Term
		*reply = &AppendEntriesReply{
			Term:    rf.currentTerm,
			Success: true,
		}
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

	return index, term, isLeader
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

	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "DISABLE" {
		log.SetOutput(ioutil.Discard)
	}

	rf.currentTerm = 0
	rf.votedFor = -1
	rf.log = make([]Log, 0)

	rf.volatile.commitIndex = 0
	rf.volatile.lastApplied = 0
	seed := randomInt64FromSystem()
	log.Printf("seed %d\n", seed)
	rf.volatile.rand = rand.New(rand.NewSource(seed))
	rf.volatile.raftState = Candidate
	rf.volatile.leaderId = -1

	rf.volatileLeader.nextIndex = make([]int, len(peers))
	rf.volatileLeader.matchIndex = make([]int, len(peers))

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	rf.resetElectionTimer()
	log.Println("initial election timer set")

	go rf.heartbeatLoop()

	return rf
}

const heartbeatInterval time.Duration = time.Duration(20) * time.Millisecond

func (rf *Raft) heartbeatLoop() {
	for {
		time.Sleep(heartbeatInterval)
		if rf.killed() {
			return
		}

		rf.mu.Lock()
		if rf.volatile.raftState != Leader {
			rf.mu.Unlock()
			continue
		}

		log.Printf("%d looping heartbeat", rf.me)

		rf.resetElectionTimer()

		prevLogIdx, prevLogTerm := rf.lastLogIndexAndTerm()
		req := AppendEntriesArgs{
			Term:         rf.currentTerm,
			LeaderId:     rf.me,
			PrevLogIndex: prevLogIdx,
			PrevLogTerm:  prevLogTerm,
			Entries:      make([]Log, 0),
			LeaderCommit: rf.volatile.commitIndex,
		}
		wg := sync.WaitGroup{}
		resps := make([]AppendEntriesReply, len(rf.peers))
		requestSuccess := make([]bool, len(rf.peers))

		for i, _ := range rf.peers {
			if i != rf.me {
				wg.Add(1)

				completed := make(chan bool)

				icopy := i

				go (func() {
					log.Printf("%d sends AppendEntries to %d\n", rf.me, icopy)
					ok := rf.sendAppendEntries(icopy, &req, &resps[icopy])
					completed <- ok
					close(completed)
				})()
				go (func() {
					defer wg.Done()
					select {
					case ok := <-completed:
						log.Printf("request ended ok %d -> %d\n", rf.me, icopy)
						requestSuccess[icopy] = ok
					case <-time.After(time.Duration(50) * time.Millisecond):
						log.Printf("request timed out %d -> %d\n", rf.me, icopy)
						requestSuccess[icopy] = false
					}
				})()
			}
		}

		wg.Wait()

		highestDisagreeingTerm := -1
		for i, r := range resps {
			if i != rf.me && requestSuccess[i] {
				log.Printf("%d response from %d %v", rf.me, i, r)
				if !r.Success {
					highestDisagreeingTerm =
						max(highestDisagreeingTerm, r.Term)
				}
			}
		}

		if highestDisagreeingTerm > -1 {
			rf.currentTerm = highestDisagreeingTerm
			rf.volatile.raftState = Follower
		}

		rf.mu.Unlock()
	}
}

func (rf *Raft) resetElectionTimer() {

	log.Printf("%d will reset timer", rf.me)

	if rf.volatile.electionTimer != nil {
		rf.volatile.electionTimer.Stop()
	}

	nextElectionTrigger :=
		time.Duration(rf.volatile.rand.Intn(500)+300) *
			time.Millisecond

	if !rf.killed() {
		rf.volatile.electionTimer = time.AfterFunc(nextElectionTrigger, rf.triggerElection)
		log.Printf("%d election timer reset to %v\n", rf.me, nextElectionTrigger)
	}
}

func (rf *Raft) triggerElection() {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	log.Printf("%d trigger election", rf.me)
	rf.currentTerm += 1
	rf.votedFor = rf.me

	lastLogIdx, lastLogTerm := rf.lastLogIndexAndTerm()

	req := RequestVoteArgs{
		Term:         rf.currentTerm,
		CandidateId:  rf.me,
		LastLogIndex: lastLogIdx,
		LastLogTerm:  lastLogTerm,
	}
	wg := sync.WaitGroup{}

	resps := make([]RequestVoteReply, len(rf.peers))
	requestSuccess := make([]bool, len(rf.peers))

	for i, _ := range rf.peers {
		if i != rf.me {
			wg.Add(1)

			completed := make(chan bool)

			icopy := i

			go (func() {
				log.Printf("%d sends RequestVote to %d\n", rf.me, icopy)
				ok := rf.sendRequestVote(icopy, &req, &resps[icopy])
				completed <- ok
				close(completed)
			})()
			go (func() {
				defer wg.Done()
				select {
				case ok := <-completed:
					requestSuccess[icopy] = ok
				case <-time.After(time.Duration(50) * time.Millisecond):
					log.Printf("request timed out %d -> %d\n", rf.me, icopy)
					requestSuccess[icopy] = false
				}
			})()
		}
	}

	wg.Wait()
	log.Println("RequestVote round ended")

	votes := 1
	highestDisagreeingTerm := -1
	for i, r := range resps {
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

	if votes >= (len(rf.peers)/2)+1 {
		rf.volatile.raftState = Leader
		log.Printf("\x1b[0;92m%d im leader\x1b[0;0m", rf.me)
	} else {
		if highestDisagreeingTerm > -1 {
			rf.currentTerm = highestDisagreeingTerm
			rf.volatile.raftState = Follower
			log.Printf("%d im not leader votes=%d highestDisagreeingTerm=%d", rf.me, votes, highestDisagreeingTerm)
		}
	}

	rf.resetElectionTimer()
}

func (rf *Raft) lastLogIndexAndTerm() (int, int) {
	lastLogIdx := max(0, len(rf.log)-1)
	var lastLogTerm int
	if len(rf.log) > 0 {
		lastLogTerm = rf.log[lastLogIdx].Term
	}

	return lastLogIdx, lastLogTerm
}

func max(x, y int) int {
	if x > y {
		return x
	} else {
		return y
	}
}

func randomInt64FromSystem() int64 {

	f, err := os.Open("/dev/urandom")
	if err != nil {
		log.Fatalf("error opening file %v", err)
	}

	var result int64
	err = binary.Read(f, binary.LittleEndian, &result)
	if err != nil {
		log.Fatalf("error reading bytes %v", err)
	}

	return result
}
