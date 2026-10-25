// Package site builds a completely static GitHub Pages site from the ledger.
package site

import (
	"crypto/sha256"
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
	"github.com/cdlewis/ChrisCoin/internal/social"
)

//go:embed layout.html assets/*
var files embed.FS

type Options struct {
	Output, Repository, BasePath, Commit string
	SiteURL, ImageDirectory              string
	Now                                  time.Time
}
type Page struct {
	Requests                                                     []*ledger.PaymentRequest
	PaymentRequest                                               *ledger.PaymentRequest
	Record                                                       ledger.Event
	Kind, Title, Repository, BasePath, Commit, Updated, ClaimURL string
	Description, CanonicalURL, ImageURL, ImageAlt                string
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
	if command.Action == "request" {
		title = fmt.Sprintf("ChrisCoin: request %d from %s", command.Amount, command.Payer)
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
	if options.ImageDirectory == "" {
		options.ImageDirectory = "social"
	}
	if options.SiteURL == "" {
		owner := strings.Split(options.Repository, "/")[0]
		options.SiteURL = "https://" + owner + ".github.io" + base
		if options.Repository == "cdlewis/ChrisCoin" {
			options.SiteURL = "https://coin.chrislewis.au" + base
		}
	}
	publicURL, err := url.Parse(options.SiteURL)
	if err != nil || publicURL.Host == "" || (publicURL.Scheme != "https" && publicURL.Scheme != "http") || publicURL.RawQuery != "" || publicURL.Fragment != "" || publicURL.User != nil {
		return fmt.Errorf("site URL must be an absolute HTTP(S) URL without a query or fragment")
	}
	// Pages can report an inherited custom domain as HTTP even when it redirects to HTTPS.
	publicURL.Scheme = "https"
	publicBase := strings.TrimRight(publicURL.String(), "/")
	page := Page{Repository: options.Repository, BasePath: base, Commit: options.Commit, Updated: options.Now.UTC().Format("02 Jan 2006, 15:04 UTC"), State: state, ClaimURL: RequestURL(options.Repository, ledger.Command{Action: "claim"})}
	page.Description = "ChrisCoin public ledger. Account balances, offers, and recorded transfers."
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
	for _, request := range state.Requests {
		page.Requests = append(page.Requests, request)
	}
	sort.Slice(page.Requests, func(i, j int) bool { return page.Requests[i].Event.Sequence > page.Requests[j].Event.Sequence })
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
		"userURL":    func(id int64) string { return versioned(fmt.Sprintf("%s/users/%d/", base, id), options.Commit) },
		"offerURL":   func(id string) string { return versioned(base+"/offers/"+id+"/", options.Commit) },
		"requestURL": func(id string) string { return versioned(base+"/requests/"+id+"/", options.Commit) },
		"requestFormURL": func() string {
			u, _ := url.Parse(versioned(base+"/", options.Commit))
			q := u.Query()
			q.Set("mode", "request")
			u.RawQuery = q.Encode()
			u.Fragment = "send"
			return u.String()
		},
		"payURL": func(id string) string {
			return RequestURL(options.Repository, ledger.Command{Action: "fulfill", RequestID: id})
		},
		"cancelRequestURL": func(id string) string {
			return RequestURL(options.Repository, ledger.Command{Action: "cancel-request", RequestID: id})
		},
		"account": func(id int64) *ledger.Account { return state.Accounts[id] },
		"acceptURL": func(id string) string {
			return RequestURL(options.Repository, ledger.Command{Action: "accept", OfferID: id})
		},
		"cancelURL": func(id string) string {
			return RequestURL(options.Repository, ledger.Command{Action: "cancel", OfferID: id})
		},
		"related": func(e ledger.Event, id int64) bool {
			if e.Actor.ID == id || e.RecipientID == id {
				return true
			}
			if offer := state.Offers[e.Source.Command.OfferID]; offer != nil {
				return offer.Sender == id
			}
			if request := state.Requests[e.Source.Command.RequestID]; request != nil {
				return request.Requester == id
			}
			return false
		},
		"description": func(e ledger.Event) string {
			switch e.Type {
			case "allowance":
				return "opened an account with 100 CC"
			case "send":
				return fmt.Sprintf("sent %d CC to %s", e.Source.Command.Amount, e.Source.Command.Recipient)
			case "offer":
				return fmt.Sprintf("offered %d to %s", e.Source.Command.Amount, e.Source.Command.Recipient)
			case "accept":
				return "accepted " + e.Source.Command.OfferID
			case "cancel":
				return "cancelled " + e.Source.Command.OfferID
			case "request":
				return fmt.Sprintf("requested %d CC from %s", e.Source.Command.Amount, e.Source.Command.Payer)
			case "fulfill":
				return "paid " + e.Source.Command.RequestID
			default:
				return "cancelled " + e.Source.Command.RequestID
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
		page.CanonicalURL = publicBase + "/"
		if path != "" {
			page.CanonicalURL = publicBase + "/" + path + "/"
		}
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
		p.Kind, p.Offer, p.Record = "offer", offer, offer.Event
		if err := writeSharingImage(&p, offer.Event, options, publicBase); err != nil {
			return err
		}
		if err := write("offers/"+offer.ID, p); err != nil {
			return err
		}
	}
	for _, request := range page.Requests {
		p := page
		p.Kind, p.PaymentRequest, p.Record = "request", request, request.Event
		if err := writeSharingImage(&p, request.Event, options, publicBase); err != nil {
			return err
		}
		if err := write("requests/"+request.ID, p); err != nil {
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

func writeSharingImage(p *Page, event ledger.Event, options Options, publicBase string) error {
	data, err := social.ReadVerified(options.ImageDirectory, event)
	if err != nil {
		return err
	}
	path := social.RelativePath(event.Type, event.Source.Issue)
	target := filepath.Join(options.Output, path)
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(target, data, 0644); err != nil {
		return err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	p.Title = social.Caption(event) + " · ChrisCoin"
	p.Description = social.Caption(event) + ". View the " + event.Type + " on ChrisCoin."
	p.ImageAlt = social.Caption(event)
	p.ImageURL = publicBase + "/" + path + "?v=" + digest[:16]
	return nil
}
