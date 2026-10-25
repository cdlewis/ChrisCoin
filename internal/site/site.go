// Package site builds a completely static GitHub Pages site from the ledger.
package site

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cdlewis/ChrisCoin/internal/ledger"
)

//go:embed layout.html assets/*
var files embed.FS

type Options struct {
	Output, Repository, BasePath, Commit string
	Now                                  time.Time
}
type Page struct {
	Kind, Title, Repository, BasePath, Commit, Updated, ClaimURL string
	State                                                        *ledger.State
	Accounts                                                     []*ledger.Account
	Offers                                                       []*ledger.Offer
	Recent                                                       []ledger.Event
	Account                                                      *ledger.Account
	Offer                                                        *ledger.Offer
}

func RequestURL(repository string, command ledger.Command) string {
	title := "ChrisCoin: " + command.Action
	if command.Action == "offer" {
		title = fmt.Sprintf("ChrisCoin: offer %d to %s", command.Amount, command.Recipient)
	}
	query := url.Values{"template": {"request.md"}, "title": {title}, "body": {ledger.RequestBody(command)}}
	return "https://github.com/" + repository + "/issues/new?" + query.Encode()
}

func versioned(path, commit string) string {
	if commit == "" {
		return path
	}
	return path + "?v=" + url.QueryEscape(commit)
}

func Build(data []byte, options Options) error {
	events, err := ledger.Parse(data)
	if err != nil {
		return err
	}
	state, err := ledger.Replay(events)
	if err != nil {
		return err
	}
	base := strings.TrimRight(options.BasePath, "/")
	if base != "" && (!strings.HasPrefix(base, "/") || strings.Contains(base, "..") || strings.ContainsAny(base, "?#\\")) {
		return fmt.Errorf("base path must be an absolute URL path")
	}
	if options.Now.IsZero() {
		options.Now = time.Now().UTC()
	}
	page := Page{Repository: options.Repository, BasePath: base, Commit: options.Commit, Updated: options.Now.UTC().Format("02 Jan 2006, 15:04 UTC"), State: state, ClaimURL: RequestURL(options.Repository, ledger.Command{Action: "claim"})}
	for _, account := range state.Accounts {
		page.Accounts = append(page.Accounts, account)
	}
	sort.Slice(page.Accounts, func(i, j int) bool {
		a, b := page.Accounts[i], page.Accounts[j]
		if a.Available+a.Reserved != b.Available+b.Reserved {
			return a.Available+a.Reserved > b.Available+b.Reserved
		}
		if a.Login != b.Login {
			return strings.ToLower(a.Login) < strings.ToLower(b.Login)
		}
		return a.ID < b.ID
	})
	for _, offer := range state.Offers {
		page.Offers = append(page.Offers, offer)
	}
	sort.Slice(page.Offers, func(i, j int) bool { return page.Offers[i].Event.Sequence > page.Offers[j].Event.Sequence })
	for i := len(events) - 1; i >= 0; i-- {
		page.Recent = append(page.Recent, events[i])
	}
	functions := template.FuncMap{
		"homeURL":  func() string { return versioned(base+"/", options.Commit) },
		"assetURL": func(name string) string { return versioned(base+"/assets/"+name, options.Commit) },
		"inc":      func(n int) int { return n + 1 },
		"coins": func(n int64) string {
			raw := strconv.FormatInt(n, 10)
			for i := len(raw) - 3; i > 0; i -= 3 {
				raw = raw[:i] + "," + raw[i:]
			}
			return raw
		},
		"userURL":  func(id int64) string { return versioned(fmt.Sprintf("%s/users/%d/", base, id), options.Commit) },
		"offerURL": func(id string) string { return versioned(base+"/offers/"+id+"/", options.Commit) },
		"account":  func(id int64) *ledger.Account { return state.Accounts[id] },
		"acceptURL": func(id string) string {
			return RequestURL(options.Repository, ledger.Command{Action: "accept", OfferID: id})
		},
		"cancelURL": func(id string) string {
			return RequestURL(options.Repository, ledger.Command{Action: "cancel", OfferID: id})
		},
		"related": func(e ledger.Event, id int64) bool {
			if e.Actor.ID == id {
				return true
			}
			if offer := state.Offers[e.Source.Command.OfferID]; offer != nil {
				return offer.Sender == id
			}
			return false
		},
		"description": func(e ledger.Event) string {
			switch e.Type {
			case "allowance":
				return "opened an account with 100 CC"
			case "offer":
				return fmt.Sprintf("offered %d to %s", e.Source.Command.Amount, e.Source.Command.Recipient)
			case "accept":
				return "accepted " + e.Source.Command.OfferID
			default:
				return "cancelled " + e.Source.Command.OfferID
			}
		},
		"short": func(s string) string {
			if len(s) > 7 {
				return s[:7]
			}
			return s
		},
	}
	tmpl, err := template.New("layout.html").Funcs(functions).ParseFS(files, "layout.html")
	if err != nil {
		return err
	}
	write := func(path string, page Page) error {
		target := filepath.Join(options.Output, path, "index.html")
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		f, err := os.Create(target)
		if err != nil {
			return err
		}
		if err = tmpl.Execute(f, page); err != nil {
			_ = f.Close()
			return err
		}
		return f.Close()
	}
	page.Kind, page.Title = "home", "ChrisCoin · Public ledger"
	if err := write("", page); err != nil {
		return err
	}
	for _, a := range page.Accounts {
		p := page
		p.Kind, p.Title, p.Account = "user", a.Login+" · ChrisCoin", a
		if err := write(fmt.Sprintf("users/%d", a.ID), p); err != nil {
			return err
		}
	}
	for _, offer := range page.Offers {
		p := page
		p.Kind, p.Title, p.Offer = "offer", offer.ID+" · ChrisCoin", offer
		if err := write("offers/"+offer.ID, p); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(options.Output, "assets"), 0755); err != nil {
		return err
	}
	assets, err := files.ReadDir("assets")
	if err != nil {
		return err
	}
	for _, asset := range assets {
		if asset.IsDir() {
			continue
		}
		name := asset.Name()
		content, err := files.ReadFile("assets/" + name)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(options.Output, "assets", name), content, 0644); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(options.Output, "data"), 0755); err != nil {
		return err
	}
	stateJSON, err := json.MarshalIndent(struct {
		*ledger.State
		Commit  string    `json:"commit"`
		Updated time.Time `json:"updated"`
	}{state, options.Commit, options.Now}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(options.Output, "data", "state.json"), stateJSON, 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(options.Output, "data", "events.jsonl"), data, 0644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(options.Output, ".nojekyll"), nil, 0644)
}
