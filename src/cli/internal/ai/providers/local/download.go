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
)

const (
	partSuffix = ".part"

	// reportEvery is how often the screen is told how far a download has got.
	// A message per read would be thousands a second and would tell nobody
	// anything they could not see already.
	reportEvery = 4 << 20

	copyBuffer = 256 << 10
)

// ErrChecksum is what a file that arrived changed reports. It is a refusal
// rather than a warning: a model whose weights are not the weights that were
// measured is not the model this program offered.
var ErrChecksum = errors.New("what arrived is not what was asked for")

// Progress is how far a download has got. Total is negative when the server
// would not say how big the file is, which is the same convention the database
// drivers use for a measurement nobody could take.
type Progress struct {
	ID    string
	Bytes int64
	Total int64
	Done  bool
}

// Ratio is how far along a download is, from nothing to one. A total the server
// never stated reads as nothing rather than as finished, because a bar drawn
// full while bytes are still arriving is worse than no bar at all.
func (p Progress) Ratio() float64 {
	if p.Total <= 0 {
		return 0
	}
	return min(float64(p.Bytes)/float64(p.Total), 1)
}

// Fetcher downloads models.
//
// It takes an http.Client rather than a narrower interface because the redirect
// policy is part of the safety of this code: the Hub answers with a redirect to
// a content network, and the token must not travel to it.
type Fetcher struct {
	HTTP  *http.Client
	Store *Store
	Token string
}

// NewFetcher returns a fetcher with a client that will not carry the token
// across a redirect to another host. The signed address the Hub redirects to
// carries its own permission, and sending the token as well would hand it to
// whoever runs that host.
func NewFetcher(store *Store, token string) *Fetcher {
	return &Fetcher{HTTP: &http.Client{CheckRedirect: KeepTokenHome}, Store: store, Token: token}
}

// KeepTokenHome stops a token following a redirect to another host. The signed
// address the hub redirects to carries its own permission, and sending the
// token as well would hand it to whoever runs that host.
func KeepTokenHome(request *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("stopped after %d redirects", len(via))
	}
	if len(via) > 0 && request.URL.Host != via[0].URL.Host {
		request.Header.Del("Authorization")
	}
	return nil
}

// Fetch downloads a model, resuming a part that was left behind by a download
// that was stopped. What it writes is verified against the checksum the Hub
// reports before it is given the name of a model.
func (f *Fetcher) Fetch(ctx context.Context, entry Entry, out chan<- Progress) error {
	if err := entry.validate(); err != nil {
		return err
	}
	directory := filepath.Join(f.Store.Dir(), entry.ID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", directory, err)
	}
	part := filepath.Join(directory, entry.File+partSuffix)
	from := resumeAt(part)

	response, err := f.open(ctx, entry, from)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode == http.StatusOK {
		from = 0
	}
	sum, written, err := f.write(ctx, part, from, response, entry, out)
	if err != nil {
		return err
	}
	if err := verify(response, sum); err != nil {
		_ = os.Remove(part)
		return err
	}
	final := filepath.Join(directory, entry.File)
	if err := os.Rename(part, final); err != nil {
		return fmt.Errorf("put %s in place: %w", entry.File, err)
	}
	if err := f.Store.Write(Manifest{
		ID: entry.ID, Repo: entry.Repo, File: entry.File,
		Revision: entry.Revision, Bytes: written, SHA256: sum,
	}); err != nil {
		return err
	}
	return report(ctx, out, Progress{ID: entry.ID, Bytes: written, Total: written, Done: true})
}

// resumeAt is how much of a file is already there. A part left by a download
// that was stopped is continued rather than started again, because these files
// are gigabytes and somebody's connection is not owed twice.
func resumeAt(part string) int64 {
	info, err := os.Stat(part)
	if err != nil {
		return 0
	}
	return info.Size()
}

func (f *Fetcher) open(ctx context.Context, entry Entry, from int64) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, entry.URL(), nil)
	if err != nil {
		return nil, fmt.Errorf("ask for %s: %w", entry.File, err)
	}
	if f.Token != "" {
		request.Header.Set("Authorization", "Bearer "+f.Token)
	}
	if from > 0 {
		request.Header.Set("Range", "bytes="+strconv.FormatInt(from, 10)+"-")
	}
	response, err := f.HTTP.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", entry.File, err)
	}
	switch response.StatusCode {
	case http.StatusOK, http.StatusPartialContent:
		return response, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		_ = response.Body.Close()
		return nil, fmt.Errorf("%s will not hand over %s without an account that has accepted its terms",
			entry.Repo, entry.File)
	case http.StatusRequestedRangeNotSatisfiable:
		_ = response.Body.Close()
		return nil, fmt.Errorf("the part of %s already here is longer than the file itself", entry.File)
	default:
		_ = response.Body.Close()
		return nil, fmt.Errorf("fetch %s: the hub answered %d", entry.File, response.StatusCode)
	}
}

// write streams the body into the part file, hashing as it goes so that the
// checksum costs one pass rather than a second read of a file this size.
func (f *Fetcher) write(ctx context.Context, part string, from int64, response *http.Response, entry Entry, out chan<- Progress) (string, int64, error) {
	flags := os.O_CREATE | os.O_WRONLY
	if from > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(part, flags, 0o600)
	if err != nil {
		return "", 0, fmt.Errorf("open %s: %w", part, err)
	}
	defer func() { _ = file.Close() }()

	digest := sha256.New()
	if from > 0 {
		if err := rehash(part, from, digest); err != nil {
			return "", 0, err
		}
	}
	total := expected(response, from, entry)
	written := from
	buffer := make([]byte, copyBuffer)
	told := from

	for {
		read, err := response.Body.Read(buffer)
		if read > 0 {
			if _, err := file.Write(buffer[:read]); err != nil {
				return "", 0, fmt.Errorf("write %s: %w", part, err)
			}
			digest.Write(buffer[:read])
			written += int64(read)
			if written-told >= reportEvery {
				told = written
				if err := report(ctx, out, Progress{ID: entry.ID, Bytes: written, Total: total}); err != nil {
					return "", 0, err
				}
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", 0, fmt.Errorf("read %s: %w", entry.File, err)
		}
		if err := ctx.Err(); err != nil {
			return "", 0, err
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), written, nil
}

// rehash reads back what a stopped download left so that the checksum covers
// the whole file rather than only the part that arrived this time.
func rehash(part string, upTo int64, digest io.Writer) error {
	file, err := os.Open(part)
	if err != nil {
		return fmt.Errorf("read back %s: %w", part, err)
	}
	defer func() { _ = file.Close() }()
	if _, err := io.CopyN(digest, file, upTo); err != nil {
		return fmt.Errorf("read back %s: %w", part, err)
	}
	return nil
}

// expected is how big the file is. The Hub reports the true size in a header of
// its own, because the body of the answer that carries it is a redirect stub.
func expected(response *http.Response, from int64, entry Entry) int64 {
	if linked := response.Header.Get("X-Linked-Size"); linked != "" {
		if size, err := strconv.ParseInt(linked, 10, 64); err == nil {
			return size
		}
	}
	if response.ContentLength >= 0 {
		return from + response.ContentLength
	}
	if entry.Bytes > 0 {
		return entry.Bytes
	}
	return -1
}

// verify checks what arrived against the checksum the Hub reports. For a file
// held in large file storage that header is the sha256 of the content, which is
// exactly what was computed on the way in.
func verify(response *http.Response, sum string) error {
	want := checksum(response)
	if want == "" || want == sum {
		return nil
	}
	return fmt.Errorf("%w: the checksum is %s and should be %s", ErrChecksum, sum, want)
}

// checksum is the sha256 the Hub vouches for, taken from the Hub's own answer
// rather than from the answer the content network gave.
//
// The difference is the whole of this function. A file kept in the Hub's
// deduplicating storage is answered with a redirect that carries X-Linked-Etag,
// the sha256 of the content, beside X-Xet-Hash, which is a different hash of
// the same file. The network the redirect points at returns that second hash as
// its own ETag: sixty four hex characters, the same shape as a sha256, and a
// different number. Reading it off the last answer in the chain therefore
// condemns every file that arrived perfectly intact, deletes it, and leaves
// nothing to show for the download but the room it took.
//
// So the chain is walked back to whoever said X-Linked-Etag, and a plain ETag
// counts only when nobody redirected us: from the server we asked, it is a
// statement about the file we asked for.
func checksum(response *http.Response) string {
	redirected := false
	for at := response; at != nil; {
		if tag := hexTag(at.Header.Get("X-Linked-Etag")); tag != "" {
			return tag
		}
		if at.Request == nil {
			break
		}
		at = at.Request.Response
		if at != nil {
			redirected = true
		}
	}
	if redirected {
		return ""
	}
	return hexTag(response.Header.Get("Etag"))
}

// hexTag is an entity tag read as a hash, or nothing if it is not the shape of
// one.
func hexTag(tag string) string {
	tag = strings.Trim(strings.TrimPrefix(tag, "W/"), `"`)
	if len(tag) == 64 && isHex(tag) {
		return tag
	}
	return ""
}

func isHex(text string) bool {
	_, err := hex.DecodeString(text)
	return err == nil
}

func report(ctx context.Context, out chan<- Progress, progress Progress) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	select {
	case out <- progress:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
