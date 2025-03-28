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

import (
	//	"bytes"

	"bytes"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"lab4/constants"
	"lab4/labgob"
	"lab4/labrpc"
	"lab4/logger"
)

// as each Raft peer becomes aware that successive log Entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int
}

type RaftState int

const (
	Follower = iota
	Candidate
	Leader
)

// Log Structure
type LogEntry struct {
	Term    int32
	Command interface{}
}

// A Go object implementing a single Raft peer.
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()
	leaderId  int                 // the id of the leader for the current term
	logger    *logger.Logger

	// Your data here (4A, 4B, 4C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.
	raftState RaftState // the current state of this Raft (Follower, Candidate, Leader)
	currTerm  int32     // current term at this Raft
	votedFor  int       // the peer this Raft voted for during the last election
	heartbeat bool      // keeps track of the heartbeats

	// 4B
	logs        []LogEntry
	commitIndex int
	lastApplied int
	nextIndex   []int
	matchIndex  []int
	applyCh     chan ApplyMsg
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// Your code here (4A).
	return int(rf.currTerm), rf.raftState == Leader
}

// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
// before you've implemented snapshots, you should pass nil as the
// second argument to persister.Save().
// after you've implemented snapshots, pass the current snapshot
// (or nil if there's not yet a snapshot).
func (rf *Raft) persist() {
	// Your code here (4C).
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.currTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.logs)
	raftstate := w.Bytes()
	rf.persister.Save(raftstate, nil)
}

// restore previously persisted state.
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (4C).
	// Example:
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var currTerm int32
	var votedFor int
	var logs []LogEntry
	if d.Decode(&currTerm) != nil ||
		d.Decode(&votedFor) != nil ||
		d.Decode(&logs) != nil {
		//error...
		rf.logger.Log(0, "Error reading persisted state")
	} else {
		rf.currTerm = currTerm
		rf.votedFor = votedFor
		rf.logs = logs
	}
}

// example RequestVote RPC arguments structure.
// field names must start with capital letters!
type RequestVoteArgs struct {
	// Your data here (4A, 4B).
	Term        int32
	CandId      int
	LastLogIdx  int
	LastLogTerm int
}

// example RequestVote RPC reply structure.
// field names must start with capital letters!
type RequestVoteReply struct {
	// Your data here (4A).
	Term        int32
	VoteGranted bool
}

// example RequestVote RPC handler.
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	// Your code here (2A, 2B).

	// I can vote if:
	// 1 - the term of the requester is >= of my term
	// 2 - I haven't voted for the requested term before

	// if the requester term is behind me, it means that the requester is out of sync; I reject the vote
	if args.Term < rf.currTerm {
		reply.VoteGranted = false
		reply.Term = rf.currTerm
		return
	}

	// if the requester term is more than me, it means that it is an election period; I grant the vote
	if args.Term > rf.currTerm {
		rf.currTerm = args.Term // reset my term to the new one
		rf.raftState = Follower // reset my state to Follower until the election ends or I become a Candidate
		rf.votedFor = -1        // reset my vote
		rf.persist()
	}

	// // rf.votedFor < 0: it's a new term; I should grant the vote if I haven't granted my vote to someone else
	if rf.votedFor < 0 || rf.votedFor == args.CandId {
		rf.votedFor = args.CandId
		reply.VoteGranted = true
		rf.persist()
	}

	lastLogIndex := len(rf.logs) - 1
	lastLogTerm := int(rf.logs[lastLogIndex].Term)

	isCandidateLogNewer := (args.LastLogTerm > lastLogTerm) ||
		(args.LastLogTerm == lastLogTerm && args.LastLogIdx >= lastLogIndex)

	// Grant vote if not voted yet and candidate's log is up-to-date
	if (rf.votedFor == -1 || rf.votedFor == args.CandId) && isCandidateLogNewer {
		rf.votedFor = args.CandId
		reply.VoteGranted = true
		rf.persist() // ???
	} else {
		reply.VoteGranted = false
	}

	// !!! fix request vote for terms

}

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
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
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
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	rf.mu.Lock()

	index := -1
	term := int(rf.currTerm)
	isLeader := (rf.raftState == Leader)

	if isLeader {
		// if leader, append it to logs
		// logs will be added alongside heartbeats to reduce RPC calls
		rf.logs = append(rf.logs, LogEntry{Term: rf.currTerm, Command: command})
		// rf.lastApplied += 1
		index = len(rf.logs) - 1
		rf.mu.Unlock()

		rf.persist()

		// go rf.SendAllLogs()
	} else {
		rf.mu.Unlock()
	}

	return index, term, isLeader
}

func (rf *Raft) applyLogs() {
	rf.logger.Log(0, "Applying logs for node %v with last applied %v and commit index %v", rf.me, rf.lastApplied, rf.commitIndex)
	for i := rf.lastApplied + 1; i <= rf.commitIndex; i++ {
		rf.applyCh <- ApplyMsg{
			CommandValid: true,
			Command:      rf.logs[i].Command,
			CommandIndex: i,
		}
		rf.lastApplied = i
		// rf.logger.Log(0, "Applied logs at commit index %v for node %v", rf.commitIndex, rf.me)
	}
}

type AppendEntriesArg struct {
	//TODO 4b
	Term         int32
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int32
}
type AppendEntriesReply struct {
	//TODO 4b
	Term       int32
	Success    bool
	NextIndex  int // next log index
	MatchIndex int // commit index
	Reply      int
}

/*
Invoked by leader to replicate log Entries (§5.3); also used as
heartbeat (§5.2).

Arguments:
	term -> 									leader’s term
	leaderId -> 			so follower can redirect clients
	prevLogIndex ->	index of log entry immediately preceding
					new ones
	prevLogTerm -> 					term of prevLogIndex entry
	Entries[]-> 	log Entries to store (empty for heartbeat;
						may send more than one for efficiency)
	leaderCommit ->						leader’s commitIndex

	Results:
	term ->				currentTerm, for leader to update itself
	success -> 			true if follower contained entry matching
									prevLogIndex and prevLogTerm

	Receiver implementation:
	1. Reply false if term < currentTerm (§5.1)
	2. Reply false if log doesn’t contain an entry at prevLogIndex
	whose term matches prevLogTerm (§5.3)
	3. If an existing entry conflicts with a new one (same index
	but different terms), delete the existing entry and all that
	follow it (§5.3)
	4. Append any new Entries not already in the log
	5. If leaderCommit > commitIndex, set commitIndex =
	min(leaderCommit, index of last new entry)
*/

// HeartBeat reset the timer if it is called by the leader
func (rf *Raft) AppendEntries(args *AppendEntriesArg, reply *AppendEntriesReply) {
	// TODO Implement for 4b
	// we need to update the logs
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// set heartbeat
	if args.Term < rf.currTerm {
		reply.Term = rf.currTerm
		reply.Success = false
		reply.Reply = 1
		return
	}

	rf.heartbeat = true
	rf.raftState = Follower
	rf.leaderId = args.LeaderId
	rf.currTerm = args.Term
	rf.persist()

	// commit index update, also apply logs that should be commited

	// log append
	/*
		2. Reply false if log doesn’t contain an entry at prevLogIndex
		whose term matches prevLogTerm (§5.3)
		3. If an existing entry conflicts with a new one (same index
		but different terms), delete the existing entry and all that
		follow it (§5.3)
		4. Append any new Entries not already in the log
		5. If leaderCommit > commitIndex, set commitIndex =
		min(leaderCommit, index of last new entry)
	*/
	// if args.Entries == nil {
	// 	// if heartbeat, return early
	// 	reply.Success = true
	// 	return
	// }
	if args.PrevLogIndex > (len(rf.logs) - 1) {
		reply.Success = false
		reply.NextIndex = len(rf.logs)
		reply.Reply = 2
		return
	} else if rf.logs[args.PrevLogIndex].Term != int32(args.PrevLogTerm) {
		lastIndex := args.PrevLogIndex
		for lastIndex > 0 && rf.logs[lastIndex].Term != int32(args.PrevLogTerm) {
			lastIndex--
		}
		reply.Success = false
		reply.NextIndex = lastIndex + 1
		reply.Reply = 2
		return
	}

	// rf.logger.Log(0, "Appending Entry to Node %v", rf.me)
	isLogModified := false
	ind := args.PrevLogIndex + 1
	for i, entry := range args.Entries {
		// If we already have a log at this index but terms are different, delete everything after this point
		if ind < len(rf.logs) {
			if rf.logs[ind].Term != entry.Term {
				rf.logs = rf.logs[:ind] // Delete conflicting logs
				isLogModified = true
			}
		}
		if ind >= len(rf.logs) {
			rf.logs = append(rf.logs, entry) // Append new Entries
			isLogModified = true

		}
		ind++
		i++
	}

	if isLogModified {
		rf.persist()
	}

	if args.LeaderCommit > int32(rf.commitIndex) {
		// rf.logger.Log(0, "Commit index: %v for node %v", rf.commitIndex, rf.me)
		lastNewIndex := args.PrevLogIndex + len(args.Entries)
		if int(args.LeaderCommit) < lastNewIndex {
			rf.commitIndex = int(args.LeaderCommit)
		} else {
			rf.commitIndex = lastNewIndex
		}
		rf.logger.Log(0, "apply log for follower: %v", rf.me)
		rf.applyLogs()
	}

	// rf.logger.Log(0, "Follower logs updated:")
	// rf.printLogs(rf.me)

	reply.Success = true

}

// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

func (rf *Raft) callAppendEntry(args *AppendEntriesArg, reply *AppendEntriesReply, node int) {
	// callers side of append entry
	ok := rf.peers[node].Call("Raft.AppendEntries", args, reply)

	rf.mu.Lock()
	defer rf.mu.Unlock()

	if ok && reply.Term > rf.currTerm {
		// turn into follower if term is higher
		rf.raftState = Follower
		rf.currTerm = reply.Term
		rf.persist()
		return
	}

	if len(args.Entries) != 0 {
		// print logs when not heartbeat
		// rf.logger.Log(0, "Leader logs current")
		// rf.printLogs(rf.me)
	}

	if ok && reply.Success {
		// rf.logger.Log(0, "Append Entry Success: %v", rf.me)
		// update matchIndex and nextIndex
		newMatchIndex := args.PrevLogIndex + len(args.Entries)
		if newMatchIndex > rf.matchIndex[node] {
			rf.matchIndex[node] = newMatchIndex
		}
		rf.nextIndex[node] = rf.matchIndex[node] + 1
	} else if ok && !reply.Success {
		// decrement nextIndex and retry
		if reply.Reply == 2 {
			rf.nextIndex[node] = reply.NextIndex
			go rf.callAppendEntry(args, reply, node)
		}
		// rf.logger.Log(0, "Append Entry Failed:")
		// rf.logger.Log(0, "Reply Next Index: %v", reply.NextIndex)
	}

	// we count for majority each time we get an append entry
	for n := len(rf.logs) - 1; n >= rf.commitIndex; n-- {
		if rf.logs[n].Term != rf.currTerm {
			continue
		}

		count := 1

		if rf.logs[n].Term == rf.currTerm {
			for i := 0; i < len(rf.peers); i++ {
				if i != rf.me && rf.matchIndex[i] >= n {
					count++
				}
			}
		}
		if count >= len(rf.peers)/2+1 && n != rf.commitIndex {
			rf.commitIndex = n
			rf.logger.Log(0, "New Commit index: %v for node %v", rf.commitIndex, rf.me)
			rf.applyLogs()
			break
		}
	}

	// print logs here to check
}

func (rf *Raft) printLogs(node int) {
	// print logs of node
	// rf.mu.Lock()
	// defer rf.mu.Unlock()
	rf.logger.Log(0, "Logs of Node %v", node)
	for i, log := range rf.logs {
		rf.logger.Log(0, "	Index: %v, Term: %v, Command: %v", i, log.Term, log.Command)
	}
}

func (rf *Raft) startSendingHB() {

	// check if I'm still the leader before sending HBs
	for !rf.killed() && rf.raftState == Leader {
		rf.mu.Lock()
		currTerm := rf.currTerm
		leaderId := rf.me
		rf.mu.Unlock()
		for i := range rf.peers {
			if i != rf.me {
				go func(i int) {
					rf.mu.Lock()
					prevInd := rf.nextIndex[i] - 1
					prevLog := int(rf.logs[prevInd].Term)
					entries := rf.logs[rf.nextIndex[i]:]
					rf.mu.Unlock()

					args := &AppendEntriesArg{
						Term:         currTerm,
						LeaderId:     leaderId,
						PrevLogIndex: prevInd,
						PrevLogTerm:  prevLog,
						Entries:      make([]LogEntry, len(entries)),
						LeaderCommit: int32(rf.commitIndex),
					}
					copy(args.Entries, entries) // copy the logs from nextIndex

					reply := &AppendEntriesReply{}
					// ok := rf.peers[i].Call("Raft.AppendEntries", args, reply)
					go rf.callAppendEntry(args, reply, i)

					// rf.mu.Lock()
					// if ok && reply.Term > rf.currTerm {
					// 	rf.raftState = Follower
					// 	rf.currTerm = reply.Term
					// }
					// rf.mu.Unlock()
				}(i)

				// if logs, check if append entries result is majority and choose to commit
				// after each accept, check for majority and commit index

			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// startEelction starts an election
func (rf *Raft) startElection() {
	// starting a new election
	rf.mu.Lock()

	// only Followers and Candidates can start elections
	// skip if I'm a leader - might happen when there was timeout from previous elections or another leader is selected
	if rf.raftState == Leader {
		rf.mu.Unlock()
		return
	}

	// 0. transition to the Candidate state
	rf.raftState = Candidate

	// 1. increment my term
	rf.currTerm += 1

	// 2. vote for myself
	rf.votedFor = rf.me

	rf.persist()

	// 3. ask others to vote for me as well
	args := &RequestVoteArgs{}
	args.Term = rf.currTerm
	args.CandId = rf.me
	args.LastLogIdx = len(rf.logs) - 1
	args.LastLogTerm = int(rf.logs[args.LastLogIdx].Term)

	rf.mu.Unlock()

	// should ask the peers in parallel for their vote;
	// so we'll wait on this channel after sending the requests in parallel
	voteCh := make(chan bool)

	gotVotes := 1                   // gotVotes counts granted votes for me in this round of election; counted my vote already
	majority := len(rf.peers)/2 + 1 // majority is the threshold for winning the current election
	recVotes := 1                   // recVotes counts all peers voted (mine counted); in case we haven't reached a majority of votes

	// asking peers to vote until
	// 1. I win!
	// 2. someone else wins!
	// 3. another timeout happens
	for i := 0; i < len(rf.peers); i += 1 {
		// skip asking myself - already voted
		if i != rf.me {
			go func(i int) {
				reply := &RequestVoteReply{}
				ok := rf.sendRequestVote(i, args, reply)
				voteCh <- ok && reply.VoteGranted
			}(i)
		}
	}

	// let's count the votes
	for gotVotes < majority && recVotes < len(rf.peers) {
		if <-voteCh {
			gotVotes += 1
		}
		recVotes += 1
	}

	// counting ended; let's see the results
	// 1. let's check if there's another server who has already been elected
	rf.mu.Lock()
	if rf.raftState != Candidate {
		// I'm not a Candidate anymore; we're done with counting
		rf.mu.Unlock()
		return
	}
	rf.mu.Unlock()

	// Did I get the majority of votes?
	if gotVotes >= majority {
		// change state to Leader
		rf.mu.Lock()
		rf.raftState = Leader
		rf.leaderId = rf.me
		rf.mu.Unlock()

		// RESET nextIndex and matchIndex
		rf.nextIndex = make([]int, len(rf.peers))
		rf.matchIndex = make([]int, len(rf.peers))

		lastIndex := len(rf.logs) - 1
		for i := range rf.peers {
			rf.nextIndex[i] = lastIndex + 1
		}

		// start sending HBs
		go rf.startSendingHB()
	}
}

func (rf *Raft) ticker() {
	var ms int64

	for !rf.killed() {
		// Your code here (4A)
		// Check if a leader election should be started.

		// avoid the first vote split in the first round of election
		ms = 350 + (rand.Int63() % 150)
		time.Sleep(time.Duration(ms) * time.Millisecond)

		// check if we got a heartbeat from the leader
		// if we haven't recieved any hearts; start an election
		if !rf.heartbeat {
			go rf.startElection()
		}
		// reset the heartbeat
		rf.heartbeat = false

	}
}

// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {

	// Your initialization code here (4A, 4B, 4C).
	rf := &Raft{
		mu:          sync.Mutex{},
		peers:       peers,
		persister:   persister,
		me:          me,
		logger:      logger.NewLogger(me+1, true, fmt.Sprintf("raft-%d", me), constants.RaftLoggingMap),
		dead:        0,
		leaderId:    -1,
		raftState:   Follower,
		currTerm:    0,
		votedFor:    -1,
		heartbeat:   false,
		logs:        make([]LogEntry, 0),
		commitIndex: 0,
		lastApplied: 0,
		applyCh:     applyCh,
	}

	rf.logs = append(rf.logs, LogEntry{Term: 0, Command: nil})

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	rf.logger.Log(constants.LogRaftStart, "Raft server started")

	// start ticker goroutine to start elections
	go rf.ticker()

	return rf
}

// test fail agree
// TestRejoin4B
// TestCount4B
