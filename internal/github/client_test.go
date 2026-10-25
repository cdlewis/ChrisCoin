package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("X-GitHub-Api-Version") != "2022-11-28" {
			t.Error("missing GitHub authentication or API version")
		}
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	client := New("test/coin", "test-token")
	client.BaseURL = server.URL
	return client
}

func TestWriteBranchChangesOnlyLedgerAndChecksBase(t *testing.T) {
	steps := []string{}
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/repos/test/coin")
		steps = append(steps, r.Method+" "+path)
		var body map[string]any
		if r.Body != nil && r.Method != "GET" {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
		}
		switch path {
		case "/git/commits/base":
			fmt.Fprint(w, `{"tree":{"sha":"original-tree"}}`)
		case "/git/blobs":
			decoded, err := base64.StdEncoding.DecodeString(body["content"].(string))
			if err != nil || string(decoded) != "ledger contents\n" {
				t.Error("incorrect blob")
			}
			fmt.Fprint(w, `{"sha":"blob"}`)
		case "/git/trees":
			tree := body["tree"].([]any)
			if body["base_tree"] != "original-tree" || len(tree) != 1 || tree[0].(map[string]any)["path"] != "ledger/events.jsonl" {
				t.Error("branch carries unrelated changes")
			}
			fmt.Fprint(w, `{"sha":"new-tree"}`)
		case "/git/commits":
			if body["tree"] != "new-tree" || body["parents"].([]any)[0] != "base" {
				t.Error("wrong commit parent")
			}
			fmt.Fprint(w, `{"sha":"next"}`)
		case "/git/refs/heads/chriscoin/request-1":
			w.WriteHeader(422)
			fmt.Fprint(w, `{"message":"Reference does not exist"}`)
		case "/git/refs":
			if body["ref"] != "refs/heads/chriscoin/request-1" || body["sha"] != "next" {
				t.Error("wrong ref")
			}
			w.WriteHeader(201)
			fmt.Fprint(w, `{}`)
		default:
			t.Errorf("unexpected path %s", path)
			w.WriteHeader(404)
		}
	})
	sha, err := client.WriteBranch(context.Background(), "base", "chriscoin/request-1", LedgerUpdate{Events: []byte("ledger contents\n")}, "message")
	if err != nil {
		t.Fatal(err)
	}
	if sha != "next" || len(steps) != 6 {
		t.Fatalf("unexpected branch creation: %s %v", sha, steps)
	}
}

func TestOfferImageAndLedgerUseOneCommitWithFixedPaths(t *testing.T) {
	blobs := [][]byte{}
	entries := []map[string]string{}
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/repos/test/coin")
		var body map[string]any
		if r.Method != "GET" {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
		}
		switch path {
		case "/git/commits/base":
			fmt.Fprint(w, `{"tree":{"sha":"base-tree"}}`)
		case "/git/blobs":
			data, err := base64.StdEncoding.DecodeString(body["content"].(string))
			if err != nil {
				t.Fatal(err)
			}
			blobs = append(blobs, data)
			fmt.Fprintf(w, `{"sha":"blob-%d"}`, len(blobs))
		case "/git/trees":
			for _, entry := range body["tree"].([]any) {
				e := entry.(map[string]any)
				entries = append(entries, map[string]string{"path": e["path"].(string), "sha": e["sha"].(string)})
			}
			fmt.Fprint(w, `{"sha":"combined-tree"}`)
		case "/git/commits":
			if body["tree"] != "combined-tree" {
				t.Fatal("ledger and image were not committed together")
			}
			fmt.Fprint(w, `{"sha":"next"}`)
		case "/git/refs/heads/chriscoin/request-7":
			fmt.Fprint(w, `{}`)
		default:
			t.Errorf("unexpected path %s", path)
			w.WriteHeader(404)
		}
	})
	png := []byte{137, 80, 78, 71, 0, 255}
	_, err := client.WriteBranch(context.Background(), "base", "chriscoin/request-7", LedgerUpdate{Events: []byte("ledger\n"), OfferPNG: png, OfferIssue: 7}, "offer")
	if err != nil {
		t.Fatal(err)
	}
	if len(blobs) != 2 || string(blobs[0]) != "ledger\n" || !reflect.DeepEqual(blobs[1], png) {
		t.Fatal("binary image or ledger was altered in transport")
	}
	if !reflect.DeepEqual(entries, []map[string]string{{"path": "ledger/events.jsonl", "sha": "blob-1"}, {"path": "social/offers/offer-7.png", "sha": "blob-2"}}) {
		t.Fatalf("unexpected changes: %v", entries)
	}
	if _, err := client.WriteBranch(context.Background(), "base", "chriscoin/request-7", LedgerUpdate{OfferPNG: png, OfferIssue: 8}, "bad"); err == nil {
		t.Fatal("accepted an image for another request")
	}
	if _, err := client.WriteBranch(context.Background(), "base", "chriscoin/request-7", LedgerUpdate{OfferPNG: png}, "bad"); err == nil {
		t.Fatal("accepted an image with no issue")
	}
}

func TestFileUsesBlobForLargeLedger(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/test/coin/contents/ledger/events.jsonl":
			if r.URL.Query().Get("ref") != "chriscoin/request-1" {
				t.Error("lost ref")
			}
			fmt.Fprint(w, `{"sha":"large-blob","encoding":"none","content":""}`)
		case "/repos/test/coin/git/blobs/large-blob":
			_ = json.NewEncoder(w).Encode(map[string]string{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("large ledger\n"))})
		default:
			w.WriteHeader(404)
		}
	})
	data, err := client.File(context.Background(), "ledger/events.jsonl", "chriscoin/request-1")
	if err != nil || string(data) != "large ledger\n" {
		t.Fatalf("%s %v", data, err)
	}
}

func TestPaginationSkipsPullRequests(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "1" {
			issues := make([]map[string]any, 100)
			for i := range issues {
				issues[i] = map[string]any{"number": i + 1, "pull_request": map[string]any{"url": "test"}}
			}
			_ = json.NewEncoder(w).Encode(issues)
		} else {
			fmt.Fprint(w, `[{"number":101,"body":"request"}]`)
		}
	})
	issues, err := client.OpenIssues(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Number != 101 {
		t.Fatal("pagination dropped issue or included a PR")
	}
}

func TestMergeUsesExactHeadSHAAndChecksResult(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			fmt.Fprint(w, `{"head":{"sha":"expected-head"}}`)
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.Method != "PUT" || !reflect.DeepEqual(body, map[string]string{"sha": "expected-head", "merge_method": "squash"}) {
			t.Error("merge did not assert the expected head")
		}
		fmt.Fprint(w, `{"merged":false,"message":"not mergeable"}`)
	})
	if err := client.Merge(context.Background(), 1, "expected-head"); err == nil {
		t.Fatal("treated unmerged PR as success")
	}
}

func TestMergeWaitsForUpdatedHeadAndRetriesStaleMergeView(t *testing.T) {
	reads, merges := 0, 0
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			reads++
			if reads == 1 {
				fmt.Fprint(w, `{"head":{"sha":"old-head"}}`)
			} else {
				fmt.Fprint(w, `{"head":{"sha":"new-head"}}`)
			}
			return
		}
		merges++
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["sha"] != "new-head" {
			t.Fatal("merge SHA guard was relaxed")
		}
		if merges == 1 {
			w.WriteHeader(409)
			fmt.Fprint(w, `{"message":"Head branch was modified"}`)
		} else {
			fmt.Fprint(w, `{"merged":true}`)
		}
	})
	client.RetryDelay = time.Millisecond
	if err := client.Merge(context.Background(), 1, "new-head"); err != nil {
		t.Fatal(err)
	}
	if reads != 3 || merges != 2 {
		t.Fatalf("unexpected retry sequence: reads=%d merges=%d", reads, merges)
	}
}

func TestAPIErrorPreservesStatus(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, `{"message":"Rate limit exceeded"}`)
	})
	_, err := client.Main(context.Background())
	if !IsStatus(err, 403) || strings.Contains(err.Error(), "test-token") {
		t.Fatalf("unexpected error: %v", err)
	}
}
