package ledger

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
var alice = Actor{ID: 1, Login: "alice"}
var bob = Actor{ID: 2, Login: "bob"}

func add(t *testing.T, events []Event, actor Actor, command Command) []Event {
	t.Helper()
	n := len(events) + 1
	source := Source{Issue: n, URL: fmt.Sprintf("https://github.com/test/coin/issues/%d", n), BodySHA256: strings.Repeat("a", 64), CreatedAt: testNow, Command: command}
	event, err := MakeEvent(events, actor, source, testNow, n+100)
	if err != nil {
		t.Fatal(err)
	}
	return append(events, event)
}
func attempt(events []Event, actor Actor, command Command) error {
	n := len(events) + 1
	_, err := MakeEvent(events, actor, Source{Issue: n, URL: fmt.Sprintf("https://github.com/test/coin/issues/%d", n), BodySHA256: strings.Repeat("a", 64), CreatedAt: testNow, Command: command}, testNow, 0)
	return err
}
func requireFailure(t *testing.T, events []Event, actor Actor, command Command, contains string) {
	t.Helper()
	if err := attempt(events, actor, command); err == nil || !strings.Contains(err.Error(), contains) {
		t.Fatalf("expected %q; got %v", contains, err)
	}
}

func TestAllowanceOfferAcceptanceAndConservation(t *testing.T) {
	events := add(t, nil, alice, Command{Action: "claim"})
	events = add(t, events, alice, Command{Action: "offer", Recipient: "Amanda", Amount: 80})
	state, err := Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	if state.Accounts[1].Available != 20 || state.Accounts[1].Reserved != 80 {
		t.Fatal("offer did not reserve funds")
	}
	requireFailure(t, events, alice, Command{Action: "offer", Recipient: "Someone", Amount: 21}, "not enough")
	// The decorative name does not restrict the claimant.
	events = add(t, events, bob, Command{Action: "accept", OfferID: "offer-2"})
	requireFailure(t, events, Actor{ID: 3, Login: "amanda"}, Command{Action: "accept", OfferID: "offer-2"}, "already been accepted")
	requireFailure(t, events, alice, Command{Action: "cancel", OfferID: "offer-2"}, "already been accepted")
	requireFailure(t, events, bob, Command{Action: "claim"}, "already has an account")
	state, err = Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	if state.Accounts[1].Available != 20 || state.Accounts[1].Reserved != 0 || state.Accounts[2].Available != 180 || state.Issued != 200 {
		t.Fatalf("wrong settlement: %+v", state)
	}
	encoded := Serialize(events)
	decoded, err := Parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(Serialize(decoded)) != string(encoded) {
		t.Fatal("ledger did not round-trip")
	}
}

func TestCancellationAndRenames(t *testing.T) {
	events := add(t, nil, alice, Command{Action: "claim"})
	events = add(t, events, alice, Command{Action: "offer", Recipient: "anyone", Amount: 100})
	requireFailure(t, events, bob, Command{Action: "cancel", OfferID: "offer-2"}, "only the sender")
	events = add(t, events, Actor{ID: 1, Login: "renamed"}, Command{Action: "cancel", OfferID: "offer-2"})
	state, err := Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	if state.Accounts[1].Available != 100 || state.Accounts[1].Login != "renamed" || state.Offers["offer-2"].Status != "cancelled" {
		t.Fatal("cancellation or rename failed")
	}
	requireFailure(t, events, Actor{ID: 1, Login: "renamed"}, Command{Action: "claim"}, "already has an account")
	requireFailure(t, events, bob, Command{Action: "accept", OfferID: "offer-2"}, "already been cancelled")
	requireFailure(t, events, bob, Command{Action: "accept", OfferID: "offer-999"}, "does not exist")
}

func TestSelfAcceptanceDoesNotMint(t *testing.T) {
	events := add(t, nil, alice, Command{Action: "claim"})
	events = add(t, events, alice, Command{Action: "offer", Recipient: "myself", Amount: 100})
	events = add(t, events, alice, Command{Action: "accept", OfferID: "offer-2"})
	state, err := Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	if state.Issued != 100 || state.Accounts[1].Available != 100 || state.Accounts[1].Reserved != 0 {
		t.Fatal("self acceptance changed supply")
	}
}

func TestFirstAppearanceAutomaticallyFundsOffersAndAcceptances(t *testing.T) {
	// Neither participant uses the onboarding command.
	events := add(t, nil, alice, Command{Action: "offer", Recipient: "Amanda", Amount: 40})
	state, err := Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	if state.Issued != 100 || state.Accounts[1].Available != 60 || state.Accounts[1].Reserved != 40 {
		t.Fatal("first offer was not automatically funded")
	}
	events = add(t, events, bob, Command{Action: "accept", OfferID: "offer-1"})
	state, err = Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	if state.Issued != 200 || state.Accounts[2].Available != 140 {
		t.Fatal("first acceptance did not include its starting balance")
	}
	events = add(t, events, bob, Command{Action: "offer", Recipient: "Next", Amount: 10})
	state, err = Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	if state.Issued != 200 || state.Accounts[2].Available != 130 || state.Accounts[2].Reserved != 10 {
		t.Fatal("later appearance credited another starting balance")
	}
	requireFailure(t, events, Actor{ID: 2, Login: "renamed"}, Command{Action: "claim"}, "already has an account")
	requireFailure(t, nil, alice, Command{Action: "offer", Recipient: "x", Amount: 101}, "not enough")
	requireFailure(t, nil, bob, Command{Action: "accept", OfferID: "offer-999"}, "does not exist")
	if len(state.Accounts) != 2 {
		t.Fatal("rejected requests created accounts")
	}
}

func TestStrictRequests(t *testing.T) {
	for _, input := range []string{
		`{"action":"offer","recipient":"x","amount":0}`,
		`{"action":"offer","recipient":"x","amount":-1}`,
		`{"action":"offer","recipient":"x","amount":1.5}`,
		`{"action":"offer","recipient":"x","amount":"10"}`,
		`{"action":"offer","recipient":"x","amount":9007199254740992}`,
		`{"action":"offer","recipient":"x","amount":9223372036854775808}`,
		`{"action":"offer","recipient":"x","amount":10,"sender":"alice"}`,
		`{"action":"claim","amount":0}`,
		`{"action":"claim","actor":{"id":1}}`,
		`{"action":"accept","offerId":"../evil"}`,
		`{"action":"cancel","offerId":"offer-0"}`,
		`{"action":"offer","recipient":" ","amount":1}`,
		`{"action":"offer","recipient":"a\nb","amount":1}`,
		`{"action":"mint"}`,
		`null`, `[]`, `{"action":"claim"} {"action":"claim"}`,
	} {
		t.Run(input, func(t *testing.T) {
			_, err := ParseRequest("```chriscoin\n" + input + "\n```\n")
			if err == nil {
				t.Fatal("accepted invalid command")
			}
		})
	}
	body := RequestBody(Command{Action: "offer", Recipient: "Amanda", Amount: 10})
	if _, err := ParseRequest(strings.ReplaceAll(body, "\n", "\r\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseRequest(body + body); err == nil {
		t.Fatal("accepted multiple commands")
	}
	if _, err := ParseRequest("nothing"); err == nil {
		t.Fatal("accepted no command")
	}
}

func TestCorruptLedgerIsRejected(t *testing.T) {
	events := add(t, nil, alice, Command{Action: "claim"})
	for _, mutate := range []func(*Event){
		func(e *Event) { e.Sequence = 9 }, func(e *Event) { e.Version = 2 }, func(e *Event) { e.Type = "offer" },
		func(e *Event) { e.Actor.ID = 0 }, func(e *Event) { e.Actor.Login = "github-actions[bot]" },
		func(e *Event) { e.Source.BodySHA256 = "bad" }, func(e *Event) { e.Source.URL = "javascript:alert(1)" },
		func(e *Event) { e.Source.CreatedAt = time.Time{} }, func(e *Event) { e.PullRequest = -1 },
	} {
		copy := events[0]
		mutate(&copy)
		if _, err := Replay([]Event{copy}); err == nil {
			t.Fatalf("accepted corrupt event: %+v", copy)
		}
	}
	duplicate := events[0]
	duplicate.Sequence = 2
	if _, err := Replay(append(events, duplicate)); err == nil {
		t.Fatal("accepted duplicate source request")
	}
	if _, err := Parse([]byte("{not-json}\n")); err == nil {
		t.Fatal("accepted broken JSON")
	}
}

func FuzzParseRequest(f *testing.F) {
	f.Add(RequestBody(Command{Action: "claim"}))
	f.Add("```chriscoin\n{\"action\":\"offer\",\"amount\":1,\"recipient\":\"x\"}\n```")
	f.Fuzz(func(t *testing.T, body string) {
		command, err := ParseRequest(body)
		if err == nil {
			if err := command.Validate(); err != nil {
				t.Fatal(err)
			}
		}
	})
}
