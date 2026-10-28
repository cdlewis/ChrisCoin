package ledger

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestAllowedNames(t *testing.T) {
	for _, name := range []string{"Alice", "Amanda", "cdlewis", "a", "0", "r2-d2", "a-b-c-d-e1", "1234567890", "12345678901", "12345678901234567890", "assassin"} {
		if err := ValidateName(name, "recipient"); err != nil {
			t.Errorf("rejected ordinary name %q: %v", name, err)
		}
	}
}

func TestInvalidNameFormat(t *testing.T) {
	for i, name := range []string{"", "123456789012345678901", "Ámanda", "A\u200bb", "bob alice", "bob_alice", "bob.alice", "-alice", "alice-", "a--b", " alice", "alice ", "a\nb", "@alice", "a/b"} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if err := ValidateName(name, "recipient"); err == nil {
				t.Fatal("invalid name was accepted")
			}
		})
	}
}

func TestProfanityAndObfuscationAreRejected(t *testing.T) {
	// Keep test output generic; rejected text must not be copied to user-facing errors.
	for i, name := range []string{"fuck", "FuCk", "f-u-c-k", "sh1thead", "asshole", "idiot", "moron", "stupid", "1d10t", "m-o-r-o-n", "\u006e\u0069\u0067\u0067\u0065\u0072"} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			for _, field := range []string{"recipient", "GitHub username"} {
				err := ValidateName(name, field)
				if err == nil || !strings.Contains(err.Error(), "prohibited language") {
					t.Fatal("prohibited name was not rejected by moderation")
				}
				if strings.Contains(strings.ToLower(err.Error()), strings.ToLower(name)) {
					t.Fatal("error echoed the rejected name")
				}
			}
		})
	}
}

func TestBackendAdmissionCannotBypassNamePolicy(t *testing.T) {
	for i, name := range []string{"LongerName123456789012", "bob_alice", "f-u-c-k"} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			command := Command{Action: "offer", Recipient: name, Amount: 1}
			if _, err := ParseRequest(RequestBody(command)); err == nil {
				t.Fatal("raw request bypassed name validation")
			}
			if err := attempt(nil, alice, command); err == nil {
				t.Fatal("direct event creation bypassed recipient validation")
			}
			if err := attempt(nil, Actor{ID: 1, Login: name}, Command{Action: "claim"}); err == nil {
				t.Fatal("direct event creation bypassed actor validation")
			}
		})
	}
}

func TestHistoricalNamesRemainReplayable(t *testing.T) {
	events := add(t, nil, alice, Command{Action: "offer", Recipient: "Amanda", Amount: 10})
	// This models an existing, already-merged entry under the original policy.
	events[0].Actor.Login = "historic-user"
	events[0].Source.Command.Recipient = "Workflow validation"
	data := Serialize(events)
	parsed, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	state, err := Replay(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if state.Issued != 100 || state.Accounts[1].Available != 90 || state.Accounts[1].Reserved != 10 {
		t.Fatal("admission policy changed historical accounting")
	}
	if string(Serialize(parsed)) != string(data) {
		t.Fatal("historical ledger was rewritten")
	}
	// JSON decoding is structural; admission still rejects a newly submitted copy.
	var command Command
	if err := json.Unmarshal([]byte(`{"action":"offer","recipient":"Workflow validation","amount":10}`), &command); err != nil {
		t.Fatal(err)
	}
	if err := command.Validate(); err == nil {
		t.Fatal("legacy decoding admitted an invalid new request")
	}
}

func TestSuppliedProfanityList(t *testing.T) {
	for i, name := range strings.Fields(profaneWords) {
		// Other entries are already excluded by the ASCII username format.
		if len(name) > MaxNameLength || !namePattern.MatchString(name) {
			continue
		}
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if err := ValidateName(name, "recipient"); err == nil {
				t.Fatal("supplied prohibited name was accepted")
			}
		})
	}
}
