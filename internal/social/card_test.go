package social

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cdlewis/ChrisCoin/internal/ledger"
)

func sample() ledger.Event {
	return ledger.Event{Version: 1, Sequence: 1, ID: "issue-42", Type: "offer", Actor: ledger.Actor{ID: ledger.MintAccountID, Login: "cdlewis"}, Source: ledger.Source{Issue: 42, URL: "https://github.com/test/coin/issues/42", BodySHA256: strings.Repeat("a", 64), CreatedAt: time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC), Command: ledger.Command{Action: "offer", Recipient: "Amanda", Amount: 100}}, Timestamp: time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)}
}

func TestCardDimensionsAndDeterministicContent(t *testing.T) {
	event := sample()
	data, err := Render(event)
	if err != nil {
		t.Fatal(err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 1200 || config.Height != 630 {
		t.Fatalf("wrong social image size: %+v", config)
	}
	event.PullRequest = 999
	event.Sequence = 200
	event.Timestamp = event.Timestamp.Add(time.Hour)
	again, err := Render(event)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, again) {
		t.Fatal("PR metadata or timestamps changed an immutable offer image")
	}
	event.Source.Command.Amount = 200
	changed, err := Render(event)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(data, changed) {
		t.Fatal("changed amount did not affect image")
	}
	if Caption(event) != "200 CC from cdlewis to Amanda" {
		t.Fatal("caption omits relevant offer text")
	}
}

func TestCommittedImagesAreVerifiedAgainstTheirOffers(t *testing.T) {
	event := sample()
	dir := t.TempDir()
	if err := Generate([]ledger.Event{event}, dir); err != nil {
		t.Fatal(err)
	}
	if err := Verify([]ledger.Event{event}, dir); err != nil {
		t.Fatal(err)
	}
	event.Source.Command.Recipient = "Bob"
	if err := Verify([]ledger.Event{event}, dir); err == nil {
		t.Fatal("image for a different recipient was accepted")
	}
	if err := os.Remove(filepath.Join(dir, "offer-42.png")); err != nil {
		t.Fatal(err)
	}
	if err := Verify([]ledger.Event{event}, dir); err == nil {
		t.Fatal("missing committed image was accepted")
	}
}

func TestLargestAmountAndLongHistoricalNameRender(t *testing.T) {
	event := sample()
	event.Source.Command.Amount = ledger.MaxCoins
	event.Source.Command.Recipient = "Workflow validation"
	if _, err := Render(event); err != nil {
		t.Fatal(err)
	}
	if Amount(ledger.MaxCoins) != "9,007,199,254,740,991" {
		t.Fatal("incorrect amount formatting")
	}
	event.Type = "accept"
	if _, err := Render(event); err == nil {
		t.Fatal("rendered a non-offer event")
	}
}
