// Package ledger implements ChrisCoin's deterministic accounting rules.
package ledger

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

const Allowance int64 = 100
const MaxCoins int64 = 9007199254740991 // Exact in browser JavaScript, too.
// MintAccountID is cdlewis's immutable GitHub ID. Its accepted offers issue new
// coins instead of drawing from an available or reserved balance.
const MintAccountID int64 = 553949

type Actor struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

type Command struct {
	Action    string `json:"action"`
	Recipient string `json:"recipient,omitempty"`
	Amount    int64  `json:"amount,omitempty"`
	OfferID   string `json:"offerId,omitempty"`
}

type Source struct {
	Issue      int       `json:"issue"`
	URL        string    `json:"url"`
	BodySHA256 string    `json:"bodySha256"`
	CreatedAt  time.Time `json:"createdAt"`
	Command    Command   `json:"command"`
}

type Event struct {
	Version     int       `json:"version"`
	Sequence    int       `json:"sequence"`
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Actor       Actor     `json:"actor"`
	Source      Source    `json:"source"`
	Timestamp   time.Time `json:"timestamp"`
	PullRequest int       `json:"pullRequest,omitempty"`
}

type Account struct {
	Actor
	Available int64 `json:"available"`
	Reserved  int64 `json:"reserved"`
}

type Offer struct {
	ID             string `json:"id"`
	Sender         int64  `json:"sender"`
	RecipientLabel string `json:"recipientLabel"`
	Amount         int64  `json:"amount"`
	Status         string `json:"status"`
	AcceptedBy     int64  `json:"acceptedBy,omitempty"`
	Event          Event  `json:"event"`
}

type State struct {
	Accounts map[int64]*Account `json:"accounts"`
	Offers   map[string]*Offer  `json:"offers"`
	Events   []Event            `json:"events"`
	Issued   int64              `json:"issued"`
}

var offerPattern = regexp.MustCompile(`^offer-[1-9][0-9]*$`)
var loginPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}$`)
var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var urlPattern = regexp.MustCompile(`^https://github\.com/[^/]+/[^/]+/issues/[1-9][0-9]*$`)
var requestPattern = regexp.MustCompile("(?m)^```chriscoin[ \t]*\r?\n([\\s\\S]*?)^```[ \t]*\r?$")

func DecodeStrict(data []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("expected one JSON value")
	}
	return nil
}

// UnmarshalJSON also rejects irrelevant fields, including a forged sender.
func (c *Command) UnmarshalJSON(data []byte) error {
	type plain Command
	var p plain
	if err := DecodeStrict(data, &p); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	allowed := map[string]bool{"action": true}
	switch p.Action {
	case "offer":
		allowed["recipient"], allowed["amount"] = true, true
	case "accept", "cancel":
		allowed["offerId"] = true
	case "claim":
	default:
		return errors.New("unknown action")
	}
	for field := range fields {
		if !allowed[field] {
			return errors.New("unexpected command field; use only the documented fields")
		}
	}
	*c = Command(p)
	return c.validateStructure()
}

// Validate checks new requests. Persisted events instead use validateStructure
// so a policy or dictionary update cannot invalidate the existing ledger.
func (c Command) Validate() error {
	if err := c.validateStructure(); err != nil {
		return err
	}
	if c.Action == "offer" {
		return ValidateName(c.Recipient, "recipient")
	}
	return nil
}

func (c Command) validateStructure() error {
	switch c.Action {
	case "claim":
		if c.Amount != 0 || c.Recipient != "" || c.OfferID != "" {
			return errors.New("claim takes no additional fields")
		}
	case "offer":
		if c.Amount <= 0 || c.Amount > MaxCoins {
			return errors.New("amount must be a positive whole number within the safe integer range")
		}
		if strings.TrimSpace(c.Recipient) == "" || len([]rune(c.Recipient)) > 100 {
			return errors.New("recipient label must contain 1–100 characters")
		}
		for _, r := range c.Recipient {
			if r < 32 || r == 127 {
				return errors.New("recipient label cannot contain control characters")
			}
		}
		if c.OfferID != "" {
			return errors.New("offer cannot contain an offer ID")
		}
	case "accept", "cancel":
		if !offerPattern.MatchString(c.OfferID) {
			return errors.New("invalid offer ID")
		}
		if c.Amount != 0 || c.Recipient != "" {
			return errors.New("accept and cancel only take an offer ID")
		}
	default:
		return errors.New("unknown action")
	}
	return nil
}

func IsRequest(body string) bool { return strings.Contains(body, "```chriscoin") }

func ParseRequest(body string) (Command, error) {
	var command Command
	blocks := requestPattern.FindAllStringSubmatch(body, -1)
	if len(blocks) != 1 {
		return command, errors.New("include exactly one chriscoin JSON block")
	}
	if len(blocks[0][1]) > 2000 {
		return command, errors.New("request is too long")
	}
	if err := DecodeStrict([]byte(blocks[0][1]), &command); err != nil {
		return command, fmt.Errorf("invalid request: %w", err)
	}
	if err := command.Validate(); err != nil {
		return command, err
	}
	return command, nil
}

func RequestBody(command Command) string {
	data, _ := json.MarshalIndent(command, "", "  ")
	return "```chriscoin\n" + string(data) + "\n```\n"
}

func Parse(data []byte) ([]Event, error) {
	events := []Event{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		var event Event
		if err := DecodeStrict(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("ledger line %d: %w", len(events)+1, err)
		}
		events = append(events, event)
	}
	return events, scanner.Err()
}

func Serialize(events []Event) []byte {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	for _, event := range events {
		_ = encoder.Encode(event)
	}
	return out.Bytes()
}

func MakeEvent(events []Event, actor Actor, source Source, now time.Time, pr int) (Event, error) {
	if err := ValidateName(actor.Login, "GitHub username"); err != nil {
		return Event{}, err
	}
	if err := source.Command.Validate(); err != nil {
		return Event{}, err
	}
	kind := source.Command.Action
	if kind == "claim" {
		kind = "allowance"
	}
	event := Event{Version: 1, Sequence: len(events) + 1, ID: fmt.Sprintf("issue-%d", source.Issue), Type: kind, Actor: actor, Source: source, Timestamp: now.UTC(), PullRequest: pr}
	_, err := Replay(append(append([]Event{}, events...), event))
	return event, err
}

func Replay(events []Event) (*State, error) {
	state := &State{Accounts: map[int64]*Account{}, Offers: map[string]*Offer{}, Events: []Event{}}
	seen := map[string]bool{}
	for _, e := range events {
		if e.Version != 1 || e.Sequence != len(state.Events)+1 {
			return nil, errors.New("invalid ledger version or sequence")
		}
		if e.Actor.ID <= 0 || !loginPattern.MatchString(e.Actor.Login) {
			return nil, errors.New("invalid GitHub account")
		}
		if e.Source.Issue <= 0 || e.ID != fmt.Sprintf("issue-%d", e.Source.Issue) || seen[e.ID] {
			return nil, errors.New("duplicate or invalid request ID")
		}
		if !urlPattern.MatchString(e.Source.URL) || !hashPattern.MatchString(e.Source.BodySHA256) || e.Source.CreatedAt.IsZero() || e.Timestamp.IsZero() || e.PullRequest < 0 {
			return nil, errors.New("invalid event provenance")
		}
		c := e.Source.Command
		if err := c.validateStructure(); err != nil {
			return nil, err
		}
		kind := c.Action
		if kind == "claim" {
			kind = "allowance"
		}
		if e.Type != kind {
			return nil, errors.New("event type does not match its command")
		}
		account := state.Accounts[e.Actor.ID]
		firstAppearance := account == nil
		if firstAppearance {
			if state.Issued > MaxCoins-Allowance {
				return nil, errors.New("maximum coin supply reached")
			}
			account = &Account{Actor: e.Actor, Available: Allowance}
			state.Accounts[e.Actor.ID] = account
			state.Issued += Allowance
		}
		account.Login = e.Actor.Login
		switch c.Action {
		case "claim":
			// This is just an onboarding entry. Every first valid action already
			// receives the starting balance; account presence is the only check.
			if !firstAppearance {
				return nil, errors.New("this GitHub ID already has an account and its starting balance")
			}
		case "offer":
			if account.ID != MintAccountID {
				if account.Available < c.Amount {
					return nil, errors.New("not enough available ChrisCoin")
				}
				account.Available -= c.Amount
				account.Reserved += c.Amount
			}
			id := fmt.Sprintf("offer-%d", e.Source.Issue)
			state.Offers[id] = &Offer{ID: id, Sender: account.ID, RecipientLabel: c.Recipient, Amount: c.Amount, Status: "open", Event: e}
		case "accept", "cancel":
			offer := state.Offers[c.OfferID]
			if offer == nil {
				return nil, errors.New("that offer does not exist")
			}
			if offer.Status != "open" {
				return nil, fmt.Errorf("that offer has already been %s", offer.Status)
			}
			sender := state.Accounts[offer.Sender]
			if c.Action == "cancel" {
				if sender.ID != account.ID {
					return nil, errors.New("only the sender can cancel this offer")
				}
				offer.Status = "cancelled"
				if sender.ID != MintAccountID {
					sender.Available += offer.Amount
				}
			} else {
				if sender.ID == MintAccountID {
					if state.Issued > MaxCoins-offer.Amount {
						return nil, errors.New("maximum coin supply reached")
					}
					state.Issued += offer.Amount
				}
				offer.Status = "accepted"
				offer.AcceptedBy = account.ID
				account.Available += offer.Amount
			}
			if sender.ID != MintAccountID {
				sender.Reserved -= offer.Amount
			}
		}
		seen[e.ID] = true
		state.Events = append(state.Events, e)
	}
	var sum int64
	for _, account := range state.Accounts {
		sum += account.Available + account.Reserved
	}
	if sum != state.Issued {
		return nil, errors.New("coin conservation check failed")
	}
	return state, nil
}
