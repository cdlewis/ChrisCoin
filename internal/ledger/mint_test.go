package ledger

import "testing"

var mintAccount = Actor{ID: MintAccountID, Login: "cdlewis"}

func TestMintOffersDoNotDebitOrReserveAndMintOnlyOnAcceptance(t *testing.T) {
	events := add(t, nil, mintAccount, Command{Action: "offer", Recipient: "Bob", Amount: 500})
	events = add(t, events, mintAccount, Command{Action: "offer", Recipient: "Bob", Amount: 300})
	state, err := Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	if state.Accounts[MintAccountID].Available != 100 || state.Accounts[MintAccountID].Reserved != 0 || state.Issued != 100 {
		t.Fatal("open mint offers reduced balance, reserved funds, or issued coins early")
	}
	events = add(t, events, bob, Command{Action: "accept", OfferID: "offer-1"})
	events = add(t, events, bob, Command{Action: "accept", OfferID: "offer-2"})
	state, err = Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	if state.Accounts[MintAccountID].Available != 100 || state.Accounts[MintAccountID].Reserved != 0 || state.Accounts[bob.ID].Available != 900 || state.Issued != 1000 {
		t.Fatal("mint settlement did not issue exactly the credited amounts")
	}
	requireFailure(t, events, alice, Command{Action: "accept", OfferID: "offer-1"}, "already been accepted")
}

func TestCancelMintOfferDoesNotCreateRefund(t *testing.T) {
	events := add(t, nil, mintAccount, Command{Action: "offer", Recipient: "Amanda", Amount: 500})
	requireFailure(t, events, bob, Command{Action: "cancel", OfferID: "offer-1"}, "only the sender")
	events = add(t, events, mintAccount, Command{Action: "cancel", OfferID: "offer-1"})
	state, err := Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	if state.Issued != 100 || state.Accounts[MintAccountID].Available != 100 || state.Accounts[MintAccountID].Reserved != 0 || state.Offers["offer-1"].Status != "cancelled" {
		t.Fatal("cancelling an unbacked offer changed balances")
	}
	requireFailure(t, events, bob, Command{Action: "accept", OfferID: "offer-1"}, "already been cancelled")
}

func TestMintIdentityUsesNumericIDAndSupportsSelfAcceptance(t *testing.T) {
	// Reusing the display login does not confer mint privileges.
	requireFailure(t, nil, Actor{ID: 42, Login: "cdlewis"}, Command{Action: "offer", Recipient: "Amanda", Amount: 101}, "not enough")
	// A renamed account keeps the privilege because its identity is unchanged.
	renamed := Actor{ID: MintAccountID, Login: "chris"}
	events := add(t, nil, renamed, Command{Action: "offer", Recipient: "Chris", Amount: 200})
	events = add(t, events, renamed, Command{Action: "accept", OfferID: "offer-1"})
	state, err := Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	if state.Accounts[MintAccountID].Available != 300 || state.Issued != 300 {
		t.Fatal("mint identity or self-acceptance was calculated incorrectly")
	}
}

func TestOrdinaryTransfersToMintAccountStayFundedTransfers(t *testing.T) {
	events := add(t, nil, mintAccount, Command{Action: "claim"})
	events = add(t, events, bob, Command{Action: "offer", Recipient: "Chris", Amount: 40})
	events = add(t, events, mintAccount, Command{Action: "accept", OfferID: "offer-2"})
	state, err := Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	if state.Issued != 200 || state.Accounts[bob.ID].Available != 60 || state.Accounts[bob.ID].Reserved != 0 || state.Accounts[MintAccountID].Available != 140 {
		t.Fatal("ordinary transfer to mint account unexpectedly created or lost coins")
	}
}

func TestMintSupplyLimitCannotOverflow(t *testing.T) {
	events := add(t, nil, mintAccount, Command{Action: "offer", Recipient: "Bob", Amount: MaxCoins - 2*Allowance})
	events = add(t, events, bob, Command{Action: "accept", OfferID: "offer-1"})
	state, err := Replay(events)
	if err != nil {
		t.Fatal(err)
	}
	if state.Issued != MaxCoins {
		t.Fatal("incorrect supply at numeric limit")
	}
	events = add(t, events, mintAccount, Command{Action: "offer", Recipient: "Bob", Amount: 1})
	requireFailure(t, events, bob, Command{Action: "accept", OfferID: "offer-3"}, "maximum coin supply reached")
}
