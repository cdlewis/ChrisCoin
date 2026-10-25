package site

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cdlewis/ChrisCoin/internal/ledger"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	events := []ledger.Event{}
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	commands := []ledger.Command{{Action: "claim"}, {Action: "offer", Recipient: `<script>alert("x")</script> & Amanda`, Amount: 10}, {Action: "accept", OfferID: "offer-2"}, {Action: "offer", Recipient: "Next person", Amount: 20}}
	for i, command := range commands {
		actor := ledger.Actor{ID: 1, Login: "alice"}
		if i == 2 {
			actor = ledger.Actor{ID: 2, Login: "bob"}
		}
		event, err := ledger.MakeEvent(events, actor, ledger.Source{Issue: i + 1, URL: fmt.Sprintf("https://github.com/test/coin/issues/%d", i+1), CreatedAt: now, BodySHA256: strings.Repeat("a", 64), Command: command}, now, i+100)
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	return ledger.Serialize(events)
}

func TestBuildPagesAndEscapeUserContent(t *testing.T) {
	for _, base := range []string{"/ChrisCoin", ""} {
		t.Run(base, func(t *testing.T) {
			dir := t.TempDir()
			err := Build(fixture(t), Options{Output: dir, Repository: "test/coin", BasePath: base, Commit: strings.Repeat("a", 40)})
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"index.html", "users/1/index.html", "users/2/index.html", "offers/offer-2/index.html", "offers/offer-4/index.html", "assets/app.js", "assets/style.css", "data/state.json", "data/events.jsonl"} {
				if _, err := os.Stat(filepath.Join(dir, path)); err != nil {
					t.Fatal(err)
				}
			}
			home, err := os.ReadFile(filepath.Join(dir, "index.html"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(home), base+"/users/1/") || !strings.Contains(string(home), base+"/assets/style.css") {
				t.Fatal("incorrect base paths")
			}
			offer, _ := os.ReadFile(filepath.Join(dir, "offers/offer-2/index.html"))
			if strings.Contains(string(offer), `<script>alert`) || !strings.Contains(string(offer), "&lt;script&gt;") {
				t.Fatal("recipient HTML was not escaped")
			}
			if !strings.Contains(string(offer), "Accepted by") || strings.Contains(string(offer), ">Accept on GitHub") {
				t.Fatal("settled offer still allows acceptance")
			}
			user, _ := os.ReadFile(filepath.Join(dir, "users/2/index.html"))
			if !strings.Contains(string(user), "accepted offer-2") || strings.Contains(string(user), "offered 20") {
				t.Fatal("account history contains unrelated events")
			}
			var state struct {
				Issued int64  `json:"issued"`
				Commit string `json:"commit"`
			}
			data, _ := os.ReadFile(filepath.Join(dir, "data/state.json"))
			if err := json.Unmarshal(data, &state); err != nil {
				t.Fatal(err)
			}
			if state.Issued != 100 || state.Commit != strings.Repeat("a", 40) {
				t.Fatal("incorrect JSON export")
			}
			// Every generated local page and asset reference resolves, including user/offer routes.
			re := regexp.MustCompile(`(?:href|src)="([^"]+)"`)
			err = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() || !strings.HasSuffix(path, ".html") {
					return nil
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				for _, match := range re.FindAllStringSubmatch(string(data), -1) {
					u, err := url.Parse(match[1])
					if err != nil {
						return err
					}
					if u.IsAbs() || u.Host != "" || u.Path == "" {
						continue
					}
					local := strings.TrimPrefix(u.Path, base)
					target := filepath.Join(dir, local)
					if strings.HasSuffix(local, "/") {
						target = filepath.Join(target, "index.html")
					}
					if _, err := os.Stat(target); err != nil {
						return fmt.Errorf("broken local link %s in %s: %w", u.Path, path, err)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRequestURLRoundTrips(t *testing.T) {
	command := ledger.Command{Action: "offer", Recipient: "Amanda & friends + ♥", Amount: 100}
	u, err := url.Parse(RequestURL("test/coin", command))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ledger.ParseRequest(u.Query().Get("body"))
	if err != nil {
		t.Fatal(err)
	}
	if decoded != command || u.Host != "github.com" || u.Query().Get("template") != "request.md" {
		t.Fatal("prefilled request did not round-trip")
	}
}

func TestEmptyLedgerAndInvalidLedger(t *testing.T) {
	if err := Build(nil, Options{Output: t.TempDir(), Repository: "test/coin"}); err != nil {
		t.Fatal(err)
	}
	if err := Build([]byte("bad"), Options{Output: t.TempDir(), Repository: "test/coin"}); err == nil {
		t.Fatal("built an invalid ledger")
	}
}
