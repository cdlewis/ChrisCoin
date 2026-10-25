package processor

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	gh "github.com/cdlewis/ChrisCoin/internal/github"
	"github.com/cdlewis/ChrisCoin/internal/ledger"
)

const repo = "test/coin"

var now = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
var ctx = context.Background()

type fakeStore struct {
	main                                               []byte
	revision                                           int
	issues                                             map[int]gh.Issue
	comments                                           map[int][]gh.Comment
	branches                                           map[string][]byte
	prs                                                map[string]*gh.PullRequest
	merges                                             int
	failCreate, failMerge, failAfterMerge, failComment bool
	beforeMerge                                        func()
}

func newFake() *fakeStore {
	return &fakeStore{issues: map[int]gh.Issue{}, comments: map[int][]gh.Comment{}, branches: map[string][]byte{}, prs: map[string]*gh.PullRequest{}}
}
func (s *fakeStore) worker() *Processor {
	return &Processor{Store: s, Repository: repo, Now: func() time.Time { return now }}
}
func (s *fakeStore) request(number int, user int64, command ledger.Command) gh.Issue {
	i := gh.Issue{Number: number, State: "open", HTMLURL: fmt.Sprintf("https://github.com/%s/issues/%d", repo, number), Body: ledger.RequestBody(command), User: gh.User{ID: user, Login: fmt.Sprintf("user%d", user), Type: "User"}, CreatedAt: now}
	s.issues[number] = i
	return i
}
func (s *fakeStore) Issue(_ context.Context, n int) (gh.Issue, error) { return s.issues[n], nil }
func (s *fakeStore) OpenIssues(context.Context) ([]gh.Issue, error) {
	out := []gh.Issue{}
	for _, i := range s.issues {
		if i.State == "open" {
			out = append(out, i)
		}
	}
	return out, nil
}
func (s *fakeStore) Comments(_ context.Context, n int) ([]gh.Comment, error) {
	return s.comments[n], nil
}
func (s *fakeStore) Comment(_ context.Context, n int, body string) error {
	if s.failComment {
		s.failComment = false
		return fmt.Errorf("comment API unavailable")
	}
	s.comments[n] = append(s.comments[n], gh.Comment{Body: body, User: gh.User{Login: BotLogin, Type: "Bot"}})
	return nil
}
func (s *fakeStore) CloseIssue(_ context.Context, n int) error {
	i := s.issues[n]
	i.State = "closed"
	s.issues[n] = i
	return nil
}
func (s *fakeStore) Main(context.Context) (string, error) {
	return fmt.Sprintf("main-%d", s.revision), nil
}
func (s *fakeStore) File(_ context.Context, _ string, ref string) ([]byte, error) {
	if strings.HasPrefix(ref, "main-") {
		return append([]byte{}, s.main...), nil
	}
	if data, ok := s.branches[ref]; ok {
		return append([]byte{}, data...), nil
	}
	return nil, &gh.APIError{Status: 404}
}
func (s *fakeStore) FindPR(_ context.Context, branch string) (*gh.PullRequest, error) {
	return s.prs[branch], nil
}
func (s *fakeStore) WriteBranch(_ context.Context, base, branch string, data []byte, _ string) (string, error) {
	if base != fmt.Sprintf("main-%d", s.revision) {
		return "", fmt.Errorf("stale base")
	}
	s.branches[branch] = append([]byte{}, data...)
	return branch, nil
}
func (s *fakeStore) CreatePR(_ context.Context, branch, _, _ string) (*gh.PullRequest, error) {
	if s.failCreate {
		s.failCreate = false
		return nil, fmt.Errorf("PR creation interrupted")
	}
	pr := &gh.PullRequest{Number: len(s.prs) + 100, State: "open", User: gh.User{Login: BotLogin, Type: "Bot"}}
	pr.Head.Ref, pr.Head.SHA, pr.Head.Repo.FullName = branch, branch, repo
	s.prs[branch] = pr
	return pr, nil
}
func (s *fakeStore) ClosePR(_ context.Context, n int) error {
	for _, pr := range s.prs {
		if pr.Number == n {
			pr.State = "closed"
		}
	}
	return nil
}
func (s *fakeStore) Merge(_ context.Context, n int, sha string) error {
	if s.beforeMerge != nil {
		s.beforeMerge()
		s.beforeMerge = nil
	}
	if s.failMerge {
		s.failMerge = false
		return fmt.Errorf("merge interrupted")
	}
	s.main = append([]byte{}, s.branches[sha]...)
	s.revision++
	s.merges++
	for _, pr := range s.prs {
		if pr.Number == n {
			pr.State, pr.Merged = "closed", true
		}
	}
	if s.failAfterMerge {
		s.failAfterMerge = false
		return fmt.Errorf("lost merge response")
	}
	return nil
}
func (s *fakeStore) state(t *testing.T) *ledger.State {
	t.Helper()
	events, err := ledger.Parse(s.main)
	if err != nil {
		t.Fatal(err)
	}
	state, err := ledger.Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	return state
}
func process(t *testing.T, s *fakeStore, issue gh.Issue) {
	t.Helper()
	if _, err := s.worker().Process(ctx, issue); err != nil {
		t.Fatal(err)
	}
}

func TestCompetingAcceptancesFirstProcessedWins(t *testing.T) {
	s := newFake()
	process(t, s, s.request(1, 1, ledger.Command{Action: "claim"}))
	process(t, s, s.request(2, 1, ledger.Command{Action: "offer", Recipient: "Amanda", Amount: 60}))
	early := s.request(3, 2, ledger.Command{Action: "accept", OfferID: "offer-2"})
	later := s.request(4, 3, ledger.Command{Action: "accept", OfferID: "offer-2"})
	process(t, s, later)
	process(t, s, early)
	state := s.state(t)
	if state.Offers["offer-2"].AcceptedBy != 3 || state.Accounts[3].Available != 160 || state.Issued != 200 || len(state.Events) != 3 {
		t.Fatal("first processed claim did not win")
	}
	if s.issues[3].State != "closed" || !strings.Contains(s.comments[3][0].Body, "already been accepted") {
		t.Fatal("losing claim not rejected")
	}
}

func TestAcceptCancelRace(t *testing.T) {
	for _, cancelFirst := range []bool{true, false} {
		t.Run(fmt.Sprint(cancelFirst), func(t *testing.T) {
			s := newFake()
			process(t, s, s.request(1, 1, ledger.Command{Action: "claim"}))
			process(t, s, s.request(2, 1, ledger.Command{Action: "offer", Recipient: "Amanda", Amount: 40}))
			cancel := s.request(3, 1, ledger.Command{Action: "cancel", OfferID: "offer-2"})
			accept := s.request(4, 2, ledger.Command{Action: "accept", OfferID: "offer-2"})
			if cancelFirst {
				process(t, s, cancel)
				process(t, s, accept)
			} else {
				process(t, s, accept)
				process(t, s, cancel)
			}
			if len(s.state(t).Events) != 3 {
				t.Fatal("both competing actions were recorded")
			}
		})
	}
}

func TestInterruptedRequestsRemainFrozen(t *testing.T) {
	for _, stage := range []string{"create", "merge", "after-merge", "comment"} {
		t.Run(stage, func(t *testing.T) {
			s := newFake()
			process(t, s, s.request(1, 1, ledger.Command{Action: "claim"}))
			issue := s.request(2, 1, ledger.Command{Action: "offer", Recipient: "Original", Amount: 10})
			switch stage {
			case "create":
				s.failCreate = true
			case "merge":
				s.failMerge = true
			case "after-merge":
				s.failAfterMerge = true
			case "comment":
				s.failComment = true
			}
			if _, err := s.worker().Process(ctx, issue); err == nil {
				t.Fatal("expected interruption")
			}
			issue.Body = ledger.RequestBody(ledger.Command{Action: "offer", Recipient: "Edited", Amount: 99})
			s.issues[2] = issue
			if _, err := s.worker().Run(ctx, 2); err != nil {
				t.Fatal(err)
			}
			if _, err := s.worker().Run(ctx, 2); err != nil {
				t.Fatal(err)
			}
			state := s.state(t)
			if len(state.Events) != 2 || s.merges != 2 || state.Offers["offer-2"].Amount != 10 || state.Offers["offer-2"].RecipientLabel != "Original" {
				t.Fatal("retry duplicated or changed frozen command")
			}
			if len(s.comments[2]) != 1 || s.issues[2].State != "closed" {
				t.Fatal("retry did not settle issue exactly once")
			}
		})
	}
}

func TestStaleReservationRevalidatesAfterOtherRequest(t *testing.T) {
	s := newFake()
	process(t, s, s.request(1, 1, ledger.Command{Action: "claim"}))
	first := s.request(2, 1, ledger.Command{Action: "offer", Recipient: "First", Amount: 80})
	s.failMerge = true
	if _, err := s.worker().Process(ctx, first); err == nil {
		t.Fatal("expected interruption")
	}
	process(t, s, s.request(3, 1, ledger.Command{Action: "offer", Recipient: "Second", Amount: 80}))
	process(t, s, first)
	if len(s.state(t).Events) != 2 || s.prs["chriscoin/request-2"].State != "closed" {
		t.Fatal("stale offer was merged")
	}
}

func TestAuthenticationAndRejectionIdempotence(t *testing.T) {
	s := newFake()
	issue := s.request(1, 1, ledger.Command{Action: "claim"})
	issue.Body = "```chriscoin\n{\"action\":\"claim\",\"sender\":\"other-user\"}\n```"
	s.issues[1] = issue
	// A user cannot forge the bot's rejection marker to block processing.
	s.comments[1] = []gh.Comment{{Body: marker(1, "rejected"), User: gh.User{Login: "user1", Type: "User"}}}
	process(t, s, issue)
	process(t, s, issue)
	if s.merges != 0 || len(s.comments[1]) != 2 {
		t.Fatal("rejection repeated or forged sender accepted")
	}
	issue = s.request(2, 1, ledger.Command{Action: "claim"})
	issue.User.Type = "Bot"
	process(t, s, issue)
	if s.merges != 0 {
		t.Fatal("bot account minted coins")
	}
}

func TestRecoveryDrainsRequestsAndPreservesExistingLedger(t *testing.T) {
	s := newFake()
	for i := 1; i <= 5; i++ {
		s.request(i, int64(i), ledger.Command{Action: "claim"})
	}
	changed, err := s.worker().Run(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || len(s.state(t).Events) != 5 || s.state(t).Issued != 500 {
		t.Fatal("recovery dropped requests")
	}
	if _, err := s.worker().Run(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if s.merges != 5 {
		t.Fatal("recovery duplicated requests")
	}
}

func TestUnexpectedPRCannotBeUsedAsAuthority(t *testing.T) {
	s := newFake()
	issue := s.request(1, 1, ledger.Command{Action: "claim"})
	s.prs["chriscoin/request-1"] = &gh.PullRequest{Number: 100, State: "open", User: gh.User{Login: "attacker", Type: "User"}}
	if _, err := s.worker().Process(ctx, issue); err == nil {
		t.Fatal("accepted a PR with untrusted provenance")
	}
}

func TestNamePolicyRejectsBeforeAnyLedgerWrite(t *testing.T) {
	for _, test := range []struct{ name, login, recipient string }{
		{"long-login", "longname123", "Amanda"},
		{"invalid-login", "bad_name", "Amanda"},
		{"moderated-login", "idiot", "Amanda"},
		{"long-recipient", "alice", "longname123"},
		{"invalid-recipient", "alice", "bad_name"},
		{"moderated-recipient", "alice", "moron"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := newFake()
			issue := s.request(1, 1, ledger.Command{Action: "offer", Recipient: test.recipient, Amount: 10})
			issue.User.Login = test.login
			s.issues[1] = issue
			process(t, s, issue)
			if len(s.main) != 0 || len(s.branches) != 0 || len(s.prs) != 0 || s.merges != 0 {
				t.Fatal("rejected name reached a ledger branch or PR")
			}
			if s.issues[1].State != "closed" || len(s.comments[1]) != 1 || !strings.Contains(s.comments[1][0].Body, "Request rejected:") {
				t.Fatal("request did not receive a rejection")
			}
			if len(s.state(t).Accounts) != 0 {
				t.Fatal("rejected request received a starting balance")
			}
		})
	}
}

func TestCurrentUsernameIsCheckedOnFrozenRetry(t *testing.T) {
	s := newFake()
	issue := s.request(1, 1, ledger.Command{Action: "offer", Recipient: "Amanda", Amount: 10})
	s.failMerge = true
	if _, err := s.worker().Process(ctx, issue); err == nil {
		t.Fatal("expected interrupted merge")
	}
	issue.User.Login = "longname123"
	s.issues[1] = issue
	process(t, s, issue)
	if s.merges != 0 || len(s.main) != 0 || s.prs["chriscoin/request-1"].State != "closed" {
		t.Fatal("frozen actor bypassed current username restriction")
	}
}

func TestLegacyFrozenRecipientIsCheckedOnRetry(t *testing.T) {
	s := newFake()
	issue := s.request(1, 1, ledger.Command{Action: "offer", Recipient: "Amanda", Amount: 10})
	s.failMerge = true
	if _, err := s.worker().Process(ctx, issue); err == nil {
		t.Fatal("expected interrupted merge")
	}
	branch := "chriscoin/request-1"
	events, err := ledger.Parse(s.branches[branch])
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a checkpoint made before the stricter policy was deployed.
	events[0].Source.Command.Recipient = "legacy recipient"
	s.branches[branch] = ledger.Serialize(events)
	process(t, s, issue)
	if s.merges != 0 || len(s.main) != 0 || s.prs[branch].State != "closed" {
		t.Fatal("legacy frozen recipient bypassed current policy")
	}
}

func TestMintAccountOfferFlowsThroughAuthenticatedWorker(t *testing.T) {
	s := newFake()
	issue := s.request(1, ledger.MintAccountID, ledger.Command{Action: "offer", Recipient: "Amanda", Amount: 500})
	issue.User.Login = "cdlewis"
	s.issues[1] = issue
	process(t, s, issue)
	process(t, s, s.request(2, 2, ledger.Command{Action: "accept", OfferID: "offer-1"}))
	state := s.state(t)
	if state.Issued != 700 || state.Accounts[ledger.MintAccountID].Available != 100 || state.Accounts[ledger.MintAccountID].Reserved != 0 || state.Accounts[2].Available != 600 {
		t.Fatal("worker did not apply mint-account accounting")
	}
}
