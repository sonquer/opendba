package local

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (r roundTrip) RoundTrip(request *http.Request) (*http.Response, error) { return r(request) }

// served answers with a body, the way the hub does: the true size and the
// checksum in headers of its own, and a range honoured when one is asked for.
func served(body []byte, header http.Header) (roundTrip, *[]*http.Request) {
	seen := &[]*http.Request{}
	return func(request *http.Request) (*http.Response, error) {
		*seen = append(*seen, request)
		if header == nil {
			header = http.Header{}
		}
		reply := http.Header{}
		for name, values := range header {
			reply[name] = values
		}
		from := int64(0)
		status := http.StatusOK
		if asked := request.Header.Get("Range"); asked != "" {
			at, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(asked, "bytes="), "-"), 10, 64)
			if err != nil {
				return nil, err
			}
			if at > int64(len(body)) {
				return &http.Response{
					StatusCode: http.StatusRequestedRangeNotSatisfiable,
					Body:       io.NopCloser(strings.NewReader("")),
					Header:     reply,
				}, nil
			}
			from, status = at, http.StatusPartialContent
		}
		rest := body[from:]
		return &http.Response{
			StatusCode:    status,
			Body:          io.NopCloser(strings.NewReader(string(rest))),
			Header:        reply,
			ContentLength: int64(len(rest)),
		}, nil
	}, seen
}

func sum(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func fetcher(t *testing.T, trip roundTrip, token string) (*Fetcher, *Store) {
	t.Helper()
	store := NewStore(t.TempDir())
	return &Fetcher{
		HTTP:  &http.Client{Transport: trip, CheckRedirect: KeepTokenHome},
		Store: store,
		Token: token,
	}, store
}

func sample(bytes int) Entry {
	return Entry{
		ID: "gemma-4-e4b-qat", Repo: "unsloth/gemma-4-E4B-it-qat-GGUF",
		File: "gemma.gguf", Revision: strings.Repeat("a", 40), Bytes: int64(bytes),
	}
}

func watch(out <-chan Progress, done chan<- []Progress) {
	go func() {
		seen := []Progress{}
		for progress := range out {
			seen = append(seen, progress)
		}
		done <- seen
	}()
}

func fetch(t *testing.T, f *Fetcher, entry Entry) []Progress {
	t.Helper()
	out := make(chan Progress, 64)
	done := make(chan []Progress, 1)
	watch(out, done)
	err := f.Fetch(context.Background(), entry, out)
	close(out)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	return <-done
}

func TestFetch(t *testing.T) {
	body := []byte(strings.Repeat("weights", 2000))
	trip, seen := served(body, http.Header{
		"X-Linked-Size": []string{strconv.Itoa(len(body))},
		"X-Linked-Etag": []string{`"` + sum(body) + `"`},
	})
	f, store := fetcher(t, trip, "hf_token")
	entry := sample(len(body))

	progress := fetch(t, f, entry)

	installed, err := store.Find(entry.ID)
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	written, err := os.ReadFile(installed.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != string(body) {
		t.Fatal("what was written is not what was served")
	}
	if len(progress) == 0 || !progress[len(progress)-1].Done {
		t.Fatalf("progress = %+v, want it to end with done", progress)
	}
	if progress[len(progress)-1].Bytes != int64(len(body)) {
		t.Fatalf("bytes = %d, want %d", progress[len(progress)-1].Bytes, len(body))
	}
	if request := (*seen)[0]; request.Header.Get("Authorization") != "Bearer hf_token" {
		t.Fatalf("authorization = %q, want the token on the first request", request.Header.Get("Authorization"))
	}
	if _, err := os.Stat(filepath.Join(store.Dir(), entry.ID, entry.File+partSuffix)); !os.IsNotExist(err) {
		t.Fatal("the part file was left behind")
	}
}

func TestFetchRefusesWhatChangedUnderIt(t *testing.T) {
	body := []byte("weights")
	trip, _ := served(body, http.Header{"X-Linked-Etag": []string{sum([]byte("something else"))}})
	f, store := fetcher(t, trip, "")

	err := f.Fetch(context.Background(), sample(len(body)), nil)
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("Fetch() error = %v, want a checksum refusal", err)
	}
	if store.Has("gemma-4-e4b-qat") {
		t.Fatal("a file that did not match was kept")
	}
	if _, err := os.Stat(filepath.Join(store.Dir(), "gemma-4-e4b-qat", "gemma.gguf"+partSuffix)); !os.IsNotExist(err) {
		t.Fatal("the part that did not match was left behind")
	}
}

func TestFetchResumes(t *testing.T) {
	body := []byte(strings.Repeat("weights", 500))
	trip, seen := served(body, http.Header{"X-Linked-Etag": []string{sum(body)}})
	f, store := fetcher(t, trip, "")
	entry := sample(len(body))

	directory := filepath.Join(store.Dir(), entry.ID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	half := len(body) / 2
	part := filepath.Join(directory, entry.File+partSuffix)
	if err := os.WriteFile(part, body[:half], 0o600); err != nil {
		t.Fatal(err)
	}

	fetch(t, f, entry)

	if got := (*seen)[0].Header.Get("Range"); got != fmt.Sprintf("bytes=%d-", half) {
		t.Fatalf("range = %q, want the download continued from where it stopped", got)
	}
	installed, err := store.Find(entry.ID)
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	written, err := os.ReadFile(installed.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != string(body) {
		t.Fatal("the resumed file is not the whole file")
	}
}

func TestFetchStartsAgainWhenTheServerIgnoresTheRange(t *testing.T) {
	body := []byte("the whole thing")
	trip := roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Body:          io.NopCloser(strings.NewReader(string(body))),
			Header:        http.Header{"X-Linked-Etag": []string{sum(body)}},
			ContentLength: int64(len(body)),
		}, nil
	})
	f, store := fetcher(t, trip, "")
	entry := sample(len(body))

	directory := filepath.Join(store.Dir(), entry.ID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, entry.File+partSuffix), []byte("rubbish"), 0o600); err != nil {
		t.Fatal(err)
	}

	fetch(t, f, entry)

	installed, _ := store.Find(entry.ID)
	written, err := os.ReadFile(installed.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != string(body) {
		t.Fatalf("file = %q, want the part thrown away and the whole thing written", written)
	}
}

func TestFetchRefusals(t *testing.T) {
	body := []byte("weights")
	cases := map[string]struct {
		status int
		want   string
	}{
		"gated":            {status: http.StatusForbidden, want: "accepted its terms"},
		"no account":       {status: http.StatusUnauthorized, want: "accepted its terms"},
		"gone":             {status: http.StatusNotFound, want: "answered 404"},
		"server is unwell": {status: http.StatusInternalServerError, want: "answered 500"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			trip := roundTrip(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: test.status,
					Body:       io.NopCloser(strings.NewReader("")),
					Header:     http.Header{},
				}, nil
			})
			f, _ := fetcher(t, trip, "")
			err := f.Fetch(context.Background(), sample(len(body)), nil)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Fetch() error = %v, want it to mention %q", err, test.want)
			}
		})
	}
}

func TestFetchRefusesAPartLongerThanTheFile(t *testing.T) {
	body := []byte("short")
	trip, _ := served(body, nil)
	f, store := fetcher(t, trip, "")
	entry := sample(len(body))

	directory := filepath.Join(store.Dir(), entry.ID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, entry.File+partSuffix), []byte("much much longer"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := f.Fetch(context.Background(), entry, nil)
	if err == nil || !strings.Contains(err.Error(), "longer than the file") {
		t.Fatalf("Fetch() error = %v", err)
	}
}

func TestFetchRefusesAnEntryItCannotUse(t *testing.T) {
	f, _ := fetcher(t, roundTrip(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("never asked")
	}), "")
	if err := f.Fetch(context.Background(), Entry{}, nil); err == nil {
		t.Fatal("Fetch() must refuse an entry with nothing in it")
	}
}

func TestFetchReportsAConnectionThatFailed(t *testing.T) {
	broken := errors.New("connection refused")
	f, _ := fetcher(t, roundTrip(func(*http.Request) (*http.Response, error) {
		return nil, broken
	}), "")
	err := f.Fetch(context.Background(), sample(4), nil)
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("Fetch() error = %v", err)
	}
}

func TestFetchStopsWhenCancelled(t *testing.T) {
	body := []byte(strings.Repeat("weights", 2000))
	trip, _ := served(body, nil)
	f, store := fetcher(t, trip, "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := f.Fetch(ctx, sample(len(body)), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch() error = %v, want context.Canceled", err)
	}
	if store.Has("gemma-4-e4b-qat") {
		t.Fatal("a cancelled download was recorded as a model")
	}
}

func TestTheTokenDoesNotFollowARedirectToAnotherHost(t *testing.T) {
	first, err := http.NewRequest(http.MethodGet, "https://huggingface.co/repo/resolve/x/f.gguf", nil)
	if err != nil {
		t.Fatal(err)
	}
	first.Header.Set("Authorization", "Bearer hf_token")

	elsewhere, err := http.NewRequest(http.MethodGet, "https://cdn-lfs.hf.co/signed", nil)
	if err != nil {
		t.Fatal(err)
	}
	elsewhere.Header.Set("Authorization", "Bearer hf_token")
	if err := KeepTokenHome(elsewhere, []*http.Request{first}); err != nil {
		t.Fatalf("KeepTokenHome() error = %v", err)
	}
	if elsewhere.Header.Get("Authorization") != "" {
		t.Fatal("the token was carried to the content network, which has its own permission in the address")
	}

	same, err := http.NewRequest(http.MethodGet, "https://huggingface.co/elsewhere", nil)
	if err != nil {
		t.Fatal(err)
	}
	same.Header.Set("Authorization", "Bearer hf_token")
	if err := KeepTokenHome(same, []*http.Request{first}); err != nil {
		t.Fatalf("KeepTokenHome() error = %v", err)
	}
	if same.Header.Get("Authorization") == "" {
		t.Fatal("the token was dropped on a redirect that stayed on the hub")
	}

	many := make([]*http.Request, 10)
	for i := range many {
		many[i] = first
	}
	if err := KeepTokenHome(elsewhere, many); err == nil {
		t.Fatal("a chain of redirects that goes nowhere must be stopped")
	}
}

func TestNewFetcherKeepsTheTokenHome(t *testing.T) {
	f := NewFetcher(NewStore(t.TempDir()), "hf_token")
	if f.HTTP.CheckRedirect == nil {
		t.Fatal("the client will carry the token wherever it is redirected")
	}
}

func TestExpected(t *testing.T) {
	entry := sample(900)
	cases := map[string]struct {
		header http.Header
		length int64
		from   int64
		want   int64
	}{
		"the hub says how big it is": {
			header: http.Header{"X-Linked-Size": []string{"1000"}},
			length: 12,
			want:   1000,
		},
		"a size that is not a number is ignored": {
			header: http.Header{"X-Linked-Size": []string{"lots"}},
			length: 12,
			want:   12,
		},
		"the length of what is coming, plus what is here": {
			header: http.Header{},
			length: 100,
			from:   50,
			want:   150,
		},
		"the catalogue when nobody else will say": {
			header: http.Header{},
			length: -1,
			want:   900,
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			response := &http.Response{Header: test.header, ContentLength: test.length}
			if got := expected(response, test.from, entry); got != test.want {
				t.Fatalf("expected() = %d, want %d", got, test.want)
			}
		})
	}
	response := &http.Response{Header: http.Header{}, ContentLength: -1}
	if got := expected(response, 0, Entry{}); got != -1 {
		t.Fatalf("expected() = %d, want a measurement nobody could take", got)
	}
}

// answered builds a response the way a client leaves one: on its own when
// nothing redirected, and with the answer that sent us there hanging off the
// request when something did.
func answered(header http.Header, from http.Header) *http.Response {
	response := &http.Response{Header: header, Request: &http.Request{}}
	if from != nil {
		response.Request.Response = &http.Response{Header: from, Request: &http.Request{}}
	}
	return response
}

// TestChecksum is mostly about the last two cases. The hub keeps large files in
// storage that deduplicates them, and answers with a redirect naming two hashes
// of the same file: the sha256 of the content, and a hash of its own. The
// network the redirect points at hands back that second hash as its ETag, in
// the shape of a sha256 and not equal to one, so a checksum read off the last
// answer in the chain condemns a file that arrived perfectly intact. And when
// nobody vouched for anything, nothing is verified rather than verified against
// a stranger's hash of something else.
func TestChecksum(t *testing.T) {
	good := strings.Repeat("ab", 32)
	other := strings.Repeat("cd", 32)
	linked := http.Header{"X-Linked-Etag": []string{`"` + good + `"`}, "X-Xet-Hash": []string{other}}
	cdn := http.Header{"Etag": []string{`"` + other + `"`}}
	cases := map[string]struct {
		response *http.Response
		want     string
	}{
		"the linked etag":         {response: answered(linked, nil), want: good},
		"a weak etag":             {response: answered(http.Header{"Etag": []string{`W/"` + good + `"`}}, nil), want: good},
		"an etag that is not one": {response: answered(http.Header{"Etag": []string{`"abc"`}}, nil), want: ""},
		"not hex at all":          {response: answered(http.Header{"Etag": []string{`"` + strings.Repeat("z", 64) + `"`}}, nil), want: ""},
		"nothing":                 {response: answered(http.Header{}, nil), want: ""},

		"the hub speaks through a content network": {response: answered(cdn, linked), want: good},
		"a content network nobody vouched through": {response: answered(cdn, http.Header{}), want: ""},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if got := checksum(test.response); got != test.want {
				t.Fatalf("checksum() = %q, want %q", got, test.want)
			}
		})
	}
}

// TestAFileThroughAContentNetworkIsKept is the same thing said end to end: the
// bytes are right, the network's own hash is not the one they were measured
// against, and the download has to survive that.
func TestAFileThroughAContentNetworkIsKept(t *testing.T) {
	body := []byte("weights that arrived perfectly intact")
	trip, _ := served(body, http.Header{"Etag": []string{`"` + strings.Repeat("ab", 32) + `"`}})
	redirecting := roundTrip(func(request *http.Request) (*http.Response, error) {
		response, err := trip(request)
		if err != nil {
			return nil, err
		}
		response.Request = &http.Request{Response: &http.Response{
			Header:  http.Header{"X-Linked-Etag": []string{`"` + sum(body) + `"`}},
			Request: &http.Request{},
		}}
		return response, nil
	})
	f, store := fetcher(t, redirecting, "")
	fetch(t, f, sample(len(body)))
	if !store.Has("gemma-4-e4b-qat") {
		t.Fatal("a file the hub vouched for was thrown away over a hash the network made up")
	}
}

func TestFetchWithoutAChecksumIsAllowed(t *testing.T) {
	body := []byte("weights nobody vouched for")
	trip, _ := served(body, nil)
	f, store := fetcher(t, trip, "")

	fetch(t, f, sample(len(body)))
	if !store.Has("gemma-4-e4b-qat") {
		t.Fatal("a file the hub would not vouch for was refused rather than kept")
	}
}

func TestFetchReportsAsItGoes(t *testing.T) {
	body := []byte(strings.Repeat("w", reportEvery*2+1024))
	trip, _ := served(body, http.Header{"X-Linked-Etag": []string{sum(body)}})
	f, _ := fetcher(t, trip, "")

	progress := fetch(t, f, sample(len(body)))
	if len(progress) < 3 {
		t.Fatalf("progress = %d messages, want one per few megabytes and one at the end", len(progress))
	}
	for i, told := range progress[:len(progress)-1] {
		if told.Done {
			t.Fatalf("message %d says done before the end", i)
		}
		if told.Total != int64(len(body)) {
			t.Fatalf("message %d total = %d, want %d", i, told.Total, len(body))
		}
	}
}

type breaks struct {
	data []byte
	sent bool
	err  error
}

func (b *breaks) Read(p []byte) (int, error) {
	if b.sent {
		return 0, b.err
	}
	b.sent = true
	return copy(p, b.data), nil
}

func (b *breaks) Close() error { return nil }

func TestFetchReportsAConnectionThatDropped(t *testing.T) {
	dropped := errors.New("connection reset by peer")
	trip := roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Body:          &breaks{data: []byte("half"), err: dropped},
			Header:        http.Header{},
			ContentLength: 100,
		}, nil
	})
	f, store := fetcher(t, trip, "")

	err := f.Fetch(context.Background(), sample(100), nil)
	if err == nil || !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("Fetch() error = %v", err)
	}
	if store.Has("gemma-4-e4b-qat") {
		t.Fatal("a download that broke was recorded as a model")
	}
}

func TestFetchStopsWhenNobodyIsReadingTheProgress(t *testing.T) {
	body := []byte(strings.Repeat("w", reportEvery+16))
	trip, _ := served(body, nil)
	f, _ := fetcher(t, trip, "")

	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan Progress)
	go func() {
		<-out
		cancel()
	}()
	err := f.Fetch(ctx, sample(len(body)), out)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch() error = %v, want context.Canceled", err)
	}
}

func TestFetchRefusesAPartItCannotOpen(t *testing.T) {
	body := []byte("weights")
	trip, _ := served(body, nil)
	f, store := fetcher(t, trip, "")
	entry := sample(len(body))

	part := filepath.Join(store.Dir(), entry.ID, entry.File+partSuffix)
	if err := os.MkdirAll(part, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := f.Fetch(context.Background(), entry, nil); err == nil {
		t.Fatal("Fetch() wrote over something that is not a file")
	}
}

func TestFetchRefusesAnAddressItCannotBuild(t *testing.T) {
	f, _ := fetcher(t, roundTrip(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("never asked")
	}), "")
	entry := sample(4)
	entry.File = "a file\nwith a new line in it"

	if err := f.Fetch(context.Background(), entry, nil); err == nil {
		t.Fatal("Fetch() built a request out of an address that is not one")
	}
}

func TestFetchRefusesAStoreItCannotWriteTo(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "models")
	if err := os.WriteFile(blocked, []byte("this is a file, not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &Fetcher{
		HTTP: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("never asked")
		})},
		Store: NewStore(blocked),
	}
	if err := f.Fetch(context.Background(), sample(4), nil); err == nil {
		t.Fatal("Fetch() wrote into something that is not a directory")
	}
}
