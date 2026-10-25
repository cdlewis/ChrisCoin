package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cdlewis/ChrisCoin/internal/github"
	"github.com/cdlewis/ChrisCoin/internal/ledger"
	"github.com/cdlewis/ChrisCoin/internal/processor"
	"github.com/cdlewis/ChrisCoin/internal/site"
	"github.com/cdlewis/ChrisCoin/internal/social"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: chriscoin {validate|build|process|generate-images}")
	}
	flags := flag.NewFlagSet(os.Args[1], flag.ContinueOnError)
	repoDefault := os.Getenv("GITHUB_REPOSITORY")
	if repoDefault == "" {
		repoDefault = "cdlewis/ChrisCoin"
	}
	repository := flags.String("repository", repoDefault, "GitHub owner/repository")
	input := flags.String("ledger", "ledger/events.jsonl", "ledger path")
	output := flags.String("output", "dist", "static site output directory")
	basePath := flags.String("base-path", "/"+strings.Split(repoDefault, "/")[len(strings.Split(repoDefault, "/"))-1], "site URL prefix (empty for a custom domain)")
	commit := flags.String("commit", "", "published Git commit SHA")
	baseRef := flags.String("base-ref", "", "validate append-only changes against a Git ref")
	issue := flags.Int("issue", 0, "request issue to process first")
	imageDirectory := flags.String("images-dir", "social/offers", "committed offer image directory")
	siteURL := flags.String("site-url", os.Getenv("SITE_URL"), "absolute public site URL, including its base path")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9_.-]+$`).MatchString(*repository) {
		return errors.New("invalid repository name")
	}
	switch os.Args[1] {
	case "validate", "build", "generate-images":
		data, err := os.ReadFile(*input)
		if err != nil {
			return err
		}
		events, err := ledger.Parse(data)
		if err != nil {
			return err
		}
		state, err := ledger.Replay(events)
		if err != nil {
			return err
		}
		if os.Args[1] == "generate-images" {
			return social.Generate(events, *imageDirectory)
		}
		if os.Args[1] == "validate" {
			if err := social.Verify(events, *imageDirectory); err != nil {
				return err
			}
			if *baseRef != "" {
				base, err := exec.Command("git", "show", *baseRef+":"+*input).Output()
				if err != nil {
					return err
				}
				if !strings.HasPrefix(string(data), string(base)) {
					return errors.New("ledger changes must only append; existing bytes cannot change")
				}
			}
			fmt.Printf("Valid ledger: %d entries, %d accounts, %d ChrisCoin issued.\n", len(events), len(state.Accounts), state.Issued)
			return nil
		}
		if *commit == "" {
			if data, err := exec.Command("git", "rev-parse", "HEAD").Output(); err == nil {
				*commit = strings.TrimSpace(string(data))
			}
		}
		return site.Build(data, site.Options{Output: *output, Repository: *repository, BasePath: *basePath, Commit: *commit, SiteURL: *siteURL, ImageDirectory: *imageDirectory})
	case "process":
		token := os.Getenv("GITHUB_TOKEN")
		if token == "" {
			return errors.New("GITHUB_TOKEN is required")
		}
		if *issue == 0 && os.Getenv("REQUEST_ISSUE") != "" {
			number, err := strconv.Atoi(os.Getenv("REQUEST_ISSUE"))
			if err != nil || number <= 0 {
				return errors.New("invalid REQUEST_ISSUE")
			}
			*issue = number
		}
		if *issue < 0 {
			return errors.New("issue must be positive")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		client := github.New(*repository, token)
		worker := processor.Processor{Store: client, Repository: *repository}
		changed, err := worker.Run(ctx, *issue)
		if outputPath := os.Getenv("GITHUB_OUTPUT"); outputPath != "" {
			file, outputErr := os.OpenFile(outputPath, os.O_APPEND|os.O_WRONLY, 0)
			if outputErr != nil {
				return outputErr
			}
			_, outputErr = fmt.Fprintf(file, "changed=%t\n", changed)
			closeErr := file.Close()
			if outputErr != nil {
				return outputErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
		log.Printf("Ledger changed: %t", changed)
		return err
	default:
		return errors.New("unknown command; use validate, build, process, or generate-images")
	}
}
