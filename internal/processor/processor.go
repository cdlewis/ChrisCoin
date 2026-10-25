// Package processor turns authenticated GitHub requests into ledger PRs.
// All callers that write to the same ledger must hold the same Actions concurrency lock.
package processor

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"strings"
	"time"

	gh "github.com/cdlewis/ChrisCoin/internal/github"
	"github.com/cdlewis/ChrisCoin/internal/ledger"
	"github.com/cdlewis/ChrisCoin/internal/social"
)

const LedgerPath = "ledger/events.jsonl"
const BotLogin = "github-actions[bot]"

type Store interface {
	Issue(context.Context, int) (gh.Issue, error)
	OpenIssues(context.Context) ([]gh.Issue, error)
	Comments(context.Context, int) ([]gh.Comment, error)
	Comment(context.Context, int, string) error
	CloseIssue(context.Context, int) error
	Main(context.Context) (string, error)
	File(context.Context, string, string) ([]byte, error)
	FindPR(context.Context, string) (*gh.PullRequest, error)
	WriteBranch(context.Context, string, string, gh.LedgerUpdate, string) (string, error)
	CreatePR(context.Context, string, string, string) (*gh.PullRequest, error)
	ClosePR(context.Context, int) error
	Merge(context.Context, int, string) error
}

type Processor struct {
	Store      Store
	Repository string
	Now        func() time.Time
}

func (p *Processor) Run(ctx context.Context, priority int) (bool, error) {
	queue := []gh.Issue{}
	if priority > 0 {
		issue, err := p.Store.Issue(ctx, priority)
		if err != nil {
			return false, err
		}
		queue = append(queue, issue)
	}
	issues, err := p.Store.OpenIssues(ctx)
	if err != nil {
		return false, err
	}
	queue = append(queue, issues...)
	seen := map[int]bool{}
	changed, processed := false, 0
	for _, issue := range queue {
		if seen[issue.Number] || issue.State != "open" || len(issue.PullRequest) != 0 {
			continue
		}
		seen[issue.Number] = true
		// A frozen PR remains processable even if its issue body is later edited.
		if !ledger.IsRequest(issue.Body) {
			pr, err := p.Store.FindPR(ctx, fmt.Sprintf("chriscoin/request-%d", issue.Number))
			if err != nil {
				return changed, err
			}
			if pr == nil {
				continue
			}
		}
		if processed >= 25 {
			log.Print("Batch limit reached; remaining requests will be recovered on the next run.")
			break
		}
		processed++
		didChange, err := p.Process(ctx, issue)
		changed = changed || didChange
		if err != nil {
			return changed, fmt.Errorf("request #%d: %w", issue.Number, err)
		}
	}
	return changed, nil
}

func (p *Processor) load(ctx context.Context) (string, []ledger.Event, error) {
	base, err := p.Store.Main(ctx)
	if err != nil {
		return "", nil, err
	}
	data, err := p.Store.File(ctx, LedgerPath, base)
	if err != nil {
		return "", nil, err
	}
	events, err := ledger.Parse(data)
	if err != nil {
		return "", nil, err
	}
	if _, err := ledger.Replay(events); err != nil {
		return "", nil, fmt.Errorf("canonical ledger is invalid: %w", err)
	}
	return base, events, nil
}

func marker(number int, status string) string {
	return fmt.Sprintf("<!-- chriscoin-result:issue=%d;status=%s -->", number, status)
}
func isBot(user gh.User) bool { return user.Login == BotLogin && user.Type == "Bot" }

func (p *Processor) finish(ctx context.Context, issue gh.Issue, status, message string) error {
	comments, err := p.Store.Comments(ctx, issue.Number)
	if err != nil {
		return err
	}
	found := false
	for _, comment := range comments {
		if isBot(comment.User) && strings.Contains(comment.Body, marker(issue.Number, status)) {
			found = true
		}
	}
	if !found {
		if err := p.Store.Comment(ctx, issue.Number, message+"\n\n"+marker(issue.Number, status)); err != nil {
			return err
		}
	}
	return p.Store.CloseIssue(ctx, issue.Number)
}

func (p *Processor) reject(ctx context.Context, issue gh.Issue, pr *gh.PullRequest, reason error) (bool, error) {
	if pr != nil && pr.State == "open" {
		if err := p.Store.ClosePR(ctx, pr.Number); err != nil {
			return false, err
		}
	}
	// JSON decoder errors can contain user-provided field names. Do not send mentions.
	reasonText := strings.ReplaceAll(strings.ReplaceAll(reason.Error(), "`", "'"), "@", "@\u200b")
	message := "Request rejected: " + reasonText + ".\n\nSubmit a new request to try again."
	log.Printf("Rejected request #%d: %v", issue.Number, reason)
	return false, p.finish(ctx, issue, "rejected", message)
}

func (p *Processor) Process(ctx context.Context, issue gh.Issue) (bool, error) {
	base, events, err := p.load(ctx)
	if err != nil {
		return false, err
	}
	for _, event := range events {
		if event.Source.Issue == issue.Number {
			return false, p.finish(ctx, issue, "merged", fmt.Sprintf("Recorded in [ledger PR #%d](https://github.com/%s/pull/%d).", event.PullRequest, p.Repository, event.PullRequest))
		}
	}
	comments, err := p.Store.Comments(ctx, issue.Number)
	if err != nil {
		return false, err
	}
	for _, comment := range comments {
		if isBot(comment.User) && strings.Contains(comment.Body, marker(issue.Number, "rejected")) {
			return false, p.Store.CloseIssue(ctx, issue.Number)
		}
	}
	branch := fmt.Sprintf("chriscoin/request-%d", issue.Number)
	pr, err := p.Store.FindPR(ctx, branch)
	if err != nil {
		return false, err
	}
	if pr != nil && (!isBot(pr.User) || pr.Head.Repo.FullName != p.Repository || pr.Head.Ref != branch) {
		return false, fmt.Errorf("unexpected PR provenance for branch %s", branch)
	}
	if issue.User.Type != "User" {
		return p.reject(ctx, issue, pr, fmt.Errorf("only GitHub user accounts can transact"))
	}
	// Also check the current login on retries: a frozen command must not bypass
	// name restrictions after its authenticated author changes their username.
	if err := ledger.ValidateName(issue.User.Login, "GitHub username"); err != nil {
		return p.reject(ctx, issue, pr, err)
	}
	if issue.HTMLURL != fmt.Sprintf("https://github.com/%s/issues/%d", p.Repository, issue.Number) {
		return false, fmt.Errorf("request belongs to a different repository")
	}
	if pr != nil && (pr.State != "open" || pr.Merged) {
		return p.reject(ctx, issue, nil, fmt.Errorf("the request PR was closed without a corresponding ledger entry"))
	}
	var frozen *ledger.Event
	// The deterministic branch is also a checkpoint if execution stopped before PR creation.
	ref := branch
	if pr != nil {
		ref = pr.Head.SHA
	}
	data, err := p.Store.File(ctx, LedgerPath, ref)
	if err == nil {
		candidates, err := ledger.Parse(data)
		if err != nil {
			return false, fmt.Errorf("invalid frozen request: %w", err)
		}
		if len(candidates) == 0 {
			return false, fmt.Errorf("request branch has no checkpoint")
		}
		candidate := candidates[len(candidates)-1]
		if candidate.Source.Issue != issue.Number || candidate.Actor.ID != issue.User.ID || candidate.Source.URL != issue.HTMLURL {
			return false, fmt.Errorf("frozen request does not match authenticated issue author")
		}
		frozen = &candidate
	} else if !gh.IsStatus(err, 404) {
		return false, err
	} else if pr != nil {
		return false, fmt.Errorf("PR checkpoint is missing")
	}
	var source ledger.Source
	actor := ledger.Actor{ID: issue.User.ID, Login: issue.User.Login}
	now := time.Now().UTC()
	if p.Now != nil {
		now = p.Now().UTC()
	}
	if frozen != nil {
		source, actor, now = frozen.Source, frozen.Actor, frozen.Timestamp
	} else {
		command, err := ledger.ParseRequest(issue.Body)
		if err != nil {
			return p.reject(ctx, issue, pr, err)
		}
		source = ledger.Source{Issue: issue.Number, URL: issue.HTMLURL, BodySHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(issue.Body))), CreatedAt: issue.CreatedAt, Command: command}
	}
	prNumber := 0
	if pr != nil {
		prNumber = pr.Number
	}
	event, err := ledger.MakeEvent(events, actor, source, now, prNumber)
	if err != nil {
		return p.reject(ctx, issue, pr, err)
	}
	title := fmt.Sprintf("ChrisCoin: %s by @%s (request #%d)", event.Type, actor.Login, issue.Number)
	update := gh.LedgerUpdate{Events: ledger.Serialize(append(events, event))}
	if event.Type == "offer" || event.Type == "request" || event.Type == "send" {
		update.CardPNG, err = social.Render(event)
		if err != nil {
			return false, err
		}
		update.CardIssue = event.Source.Issue
		update.CardKind = event.Type
		if event.Type == "send" {
			update.CardKind = "offer"
		}
	}
	sha, err := p.Store.WriteBranch(ctx, base, branch, update, title)
	if err != nil {
		return false, err
	}
	if pr == nil {
		pr, err = p.Store.CreatePR(ctx, branch, title, fmt.Sprintf("Records the authenticated request in #%d.\n\nThe issue author's numeric GitHub ID authorizes this action. The command is frozen in the ledger event. This PR is validated and merged by the trusted worker.\n\n<!-- chriscoin-request:%d -->", issue.Number, issue.Number))
		if err != nil {
			return false, err
		}
	}
	// Refresh against main, carrying only the ledger and this request's sharing image.
	base, events, err = p.load(ctx)
	if err != nil {
		return false, err
	}
	event, err = ledger.MakeEvent(events, actor, source, now, pr.Number)
	if err != nil {
		return p.reject(ctx, issue, pr, err)
	}
	update.Events = ledger.Serialize(append(events, event))
	// A recipient can appear between reads of main; regenerate the card from
	// the exact final event so a newly direct payment is never shown as open.
	if event.Type == "offer" || event.Type == "request" || event.Type == "send" {
		update.CardPNG, err = social.Render(event)
		if err != nil {
			return false, err
		}
		update.CardKind = event.Type
		if event.Type == "send" {
			update.CardKind = "offer"
		}
	}
	sha, err = p.Store.WriteBranch(ctx, base, branch, update, title)
	if err != nil {
		return false, err
	}
	latest, err := p.Store.Main(ctx)
	if err != nil {
		return false, err
	}
	if latest != base {
		return false, fmt.Errorf("main changed during processing; retry will rebuild the request")
	}
	if err := p.Store.Merge(ctx, pr.Number, sha); err != nil {
		return false, err
	}
	log.Printf("Merged request #%d as PR #%d", issue.Number, pr.Number)
	// Return changed=true even when the post-merge comment fails, so the workflow can publish.
	return true, p.finish(ctx, issue, "merged", fmt.Sprintf("Recorded in [ledger PR #%d](https://github.com/%s/pull/%d).", pr.Number, p.Repository, pr.Number))
}
