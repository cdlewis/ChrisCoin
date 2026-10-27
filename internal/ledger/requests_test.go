package ledger

import (
	"strings"
	"testing"
)

func TestPaymentRequestSettlesOnceToItsRequester(t *testing.T) {
	events := add(t, nil, alice, Command{Action: "request", Payer: "Amanda", Amount: 60})
	state, err := Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	if state.Accounts[1].Available != 100 || state.Accounts[1].Reserved != 0 || state.Issued != 100 {
		t.Fatal("creating a request moved money")
	}
	// Payer authorization comes from the signed-in actor, not the requested name.
	events = add(t, events, bob, Command{Action: "fulfill", RequestID: "request-1"})
	state, err = Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	request := state.Requests["request-1"]
	if request.Status != "paid" || request.PaidBy != 2 || request.Requester != 1 || request.PayerLabel != "Amanda" {
		t.Fatal("payment settled to the wrong identities")
	}
	if state.Accounts[1].Available != 160 || state.Accounts[2].Available != 40 || state.Issued != 200 {
		t.Fatal("incorrect payment accounting or first-appearance grant")
	}
	requireFailure(t, events, Actor{ID: 3, Login: "amanda"}, Command{Action: "fulfill", RequestID: "request-1"}, "already been paid")
	requireFailure(t, events, alice, Command{Action: "cancel-request", RequestID: "request-1"}, "already been paid")
}

func TestRequestPaymentUsesOnlyAvailableFunds(t *testing.T) {
	events := add(t, nil, alice, Command{Action: "request", Payer: "Bob", Amount: 30})
	events = add(t, events, bob, Command{Action: "offer", Recipient: "Charlie", Amount: 80})
	requireFailure(t, events, bob, Command{Action: "fulfill", RequestID: "request-1"}, "not enough")
	state, err := Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	if state.Requests["request-1"].Status != "open" || state.Accounts[2].Available != 20 || state.Accounts[2].Reserved != 80 {
		t.Fatal("failed payment moved reserved coins or closed the request")
	}
	events = add(t, events, bob, Command{Action: "cancel", OfferID: "offer-2"})
	events = add(t, events, bob, Command{Action: "fulfill", RequestID: "request-1"})
	state, err = Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	if state.Accounts[2].Available != 70 || state.Accounts[1].Available != 130 {
		t.Fatal("released funds could not pay the request")
	}
}

func TestOnlyRequesterCanCancelAndCancellationDoesNotMoveCoins(t *testing.T) {
	events := add(t, nil, alice, Command{Action: "request", Payer: "Bob", Amount: 200})
	requireFailure(t, events, bob, Command{Action: "cancel-request", RequestID: "request-1"}, "only the requester")
	events = add(t, events, alice, Command{Action: "cancel-request", RequestID: "request-1"})
	state, err := Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	if state.Issued != 100 || len(state.Accounts) != 1 || state.Accounts[1].Available != 100 || state.Requests["request-1"].Status != "cancelled" {
		t.Fatal("cancellation changed balances or created an account")
	}
	requireFailure(t, events, bob, Command{Action: "fulfill", RequestID: "request-1"}, "already been cancelled")
	requireFailure(t, events, bob, Command{Action: "fulfill", RequestID: "request-99"}, "does not exist")
}

func TestMintAccountFulfillsWithoutDebitAndCannotOverflow(t *testing.T) {
	events := add(t, nil, alice, Command{Action: "request", Payer: "Chris", Amount: 500})
	requireFailure(t, events, Actor{ID: 42, Login: "cdlewis"}, Command{Action: "fulfill", RequestID: "request-1"}, "not enough")
	events = add(t, events, mintAccount, Command{Action: "fulfill", RequestID: "request-1"})
	state, err := Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	if state.Accounts[MintAccountID].Available != 100 || state.Accounts[MintAccountID].Reserved != 0 || state.Accounts[1].Available != 600 || state.Issued != 700 {
		t.Fatal("mint-account request fulfillment debited funds or issued the wrong amount")
	}
	events = add(t, events, alice, Command{Action: "request", Payer: "Chris", Amount: MaxCoins})
	requireFailure(t, events, mintAccount, Command{Action: "fulfill", RequestID: "request-3"}, "maximum coin supply")
}

func TestSelfPaymentDoesNotMintForAnOrdinaryAccount(t *testing.T) {
	events := add(t, nil, alice, Command{Action: "request", Payer: "Alice", Amount: 100})
	events = add(t, events, alice, Command{Action: "fulfill", RequestID: "request-1"})
	state, err := Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	if state.Accounts[1].Available != 100 || state.Issued != 100 {
		t.Fatal("self-payment created money")
	}
}

func TestRequestCommandsValidateNamesAndRejectForgedPaymentFields(t *testing.T) {
	for _, command := range []Command{
		{Action: "request", Payer: "bad_name", Amount: 10}, {Action: "request", Payer: "moron", Amount: 10},
		{Action: "request", Payer: "123456789012345678901", Amount: 10}, {Action: "request", Payer: "Alice", Amount: 0},
		{Action: "fulfill", RequestID: "offer-1"}, {Action: "fulfill", RequestID: "request-1", Amount: 1},
		{Action: "cancel-request", RequestID: "request-1", Recipient: "Alice"},
	} {
		if _, err := ParseRequest(RequestBody(command)); err == nil {
			t.Fatal("invalid request or payment was admitted")
		}
	}
	for _, raw := range []string{`{"action":"request","payer":"Alice","amount":10,"requester":1}`, `{"action":"fulfill","requestId":"request-1","payer":"Alice"}`, `{"action":"cancel","requestId":"request-1"}`} {
		if _, err := ParseRequest("```chriscoin\n" + raw + "\n```\n"); err == nil {
			t.Fatal("forged identity or ambiguous cancellation fields were admitted")
		}
	}
	for _, command := range []Command{{Action: "request", Payer: "Alice-123", Amount: 10}, {Action: "fulfill", RequestID: "request-1"}, {Action: "cancel-request", RequestID: "request-1"}} {
		parsed, err := ParseRequest(RequestBody(command))
		if err != nil || parsed != command {
			t.Fatalf("request command did not round-trip: %v", err)
		}
	}
	if err := attempt(nil, alice, Command{Action: "request", Payer: "moron", Amount: 1}); err == nil || !strings.Contains(err.Error(), "prohibited") {
		t.Fatal("direct event creation bypassed name policy")
	}
}

func TestExistingRecipientPaidAtomicallyAndCannotBeSniped(t *testing.T) {
	events := add(t, nil, bob, Command{Action: "claim"})
	events = add(t, events, alice, Command{Action: "offer", Recipient: "BOB", Amount: 60})
	event := events[len(events)-1]
	if event.Type != "send" || event.RecipientID != bob.ID {
		t.Fatal("recipient was not pinned to its ledger ID")
	}
	state, err := Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	if state.Accounts[bob.ID].Available != 160 || state.Accounts[alice.ID].Available != 40 || state.Accounts[alice.ID].Reserved != 0 || state.Issued != 200 {
		t.Fatal("direct transfer did not settle atomically")
	}
	requireFailure(t, events, Actor{ID: 3, Login: "eve"}, Command{Action: "accept", OfferID: "offer-2"}, "already been accepted")
	requireFailure(t, events, alice, Command{Action: "cancel", OfferID: "offer-2"}, "already been accepted")
	forged := append([]Event{}, events...)
	forged[1].RecipientID = alice.ID
	if _, err := Replay(forged); err == nil {
		t.Fatal("accepted forged recipient")
	}
}

func TestDirectMintPaymentAndSelfPayment(t *testing.T) {
	events := add(t, nil, bob, Command{Action: "claim"})
	events = add(t, events, mintAccount, Command{Action: "offer", Recipient: "Bob", Amount: 500})
	state, err := Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	if state.Accounts[bob.ID].Available != 600 || state.Accounts[MintAccountID].Available != 100 || state.Issued != 700 {
		t.Fatal("mint payment debited or reserved coins")
	}
	events = add(t, events, bob, Command{Action: "offer", Recipient: "bob", Amount: 20})
	state, err = Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	if state.Accounts[bob.ID].Available != 600 || state.Accounts[bob.ID].Reserved != 0 {
		t.Fatal("self payment changed funded balance")
	}
	requireFailure(t, events, bob, Command{Action: "offer", Recipient: "cdlewis", Amount: 601}, "not enough")
}
