package tuitest

import (
	"strings"
	"testing"
	"time"
)

func started(t *testing.T, opts Options) *Session {
	t.Helper()
	session, err := Start(opts)
	if err != nil {
		t.Fatalf("Start = %v", err)
	}
	t.Cleanup(func() { _, _ = session.Close() })
	return session
}

func soon() time.Time { return time.Now().Add(10 * time.Second) }

func TestASessionShowsWhatTheProgramDrew(t *testing.T) {
	session := started(t, fakeOptions(t, 40, 10))
	frame, ok := session.Await(soon(), func(f Frame) bool { return f.Contains("READY") })
	if !ok {
		t.Fatalf("the program never drew anything: %q", frame.Plain())
	}
	if frame.Width != 40 || frame.Height != 10 {
		t.Errorf("frame = %dx%d", frame.Width, frame.Height)
	}
}

func TestASessionSendsKeysAndTextTheProgramReactsTo(t *testing.T) {
	session := started(t, fakeOptions(t, 40, 10))
	session.Await(soon(), func(f Frame) bool { return f.Contains("READY") })

	if err := session.Key("a"); err != nil {
		t.Fatalf("Key = %v", err)
	}
	if _, ok := session.Await(soon(), func(f Frame) bool { return f.Contains("ALPHA") }); !ok {
		t.Error("the program never reacted to the key")
	}
	if err := session.Type("b"); err != nil {
		t.Fatalf("Type = %v", err)
	}
	if _, ok := session.Await(soon(), func(f Frame) bool { return f.Contains("BETA") }); !ok {
		t.Error("the program never reacted to the text")
	}
}

func TestASessionRefusesAKeyThatDoesNotExist(t *testing.T) {
	session := started(t, fakeOptions(t, 40, 10))
	if err := session.Key("hyper+z"); err == nil {
		t.Error("a key that does not exist was sent")
	}
}

func TestSettleWaitsForTheProgramToReactBeforeReadingTheScreen(t *testing.T) {
	session := started(t, fakeOptions(t, 40, 10))
	session.Await(soon(), func(f Frame) bool { return f.Contains("READY") })

	if err := session.Key("a"); err != nil {
		t.Fatalf("Key = %v", err)
	}
	session.Settle(soon())
	if got := session.Frame(); !got.Contains("ALPHA") {
		t.Errorf("the screen was read before the program reacted: %q", got.Plain())
	}
}

func TestSettleGivesUpOnAKeyTheProgramIgnores(t *testing.T) {
	session := started(t, fakeOptions(t, 40, 10))
	session.Await(soon(), func(f Frame) bool { return f.Contains("READY") })

	started := time.Now()
	if err := session.Key("k"); err != nil {
		t.Fatalf("Key = %v", err)
	}
	session.Settle(soon())
	if time.Since(started) > 5*time.Second {
		t.Error("a key that draws nothing held the run open")
	}
}

func TestAwaitGivesUpAtTheDeadline(t *testing.T) {
	session := started(t, fakeOptions(t, 40, 10))
	frame, ok := session.Await(time.Now().Add(300*time.Millisecond), func(Frame) bool { return false })
	if ok {
		t.Error("a condition that is never true was satisfied")
	}
	if frame.Width != 40 {
		t.Errorf("the frame at the deadline is %dx%d", frame.Width, frame.Height)
	}
}

func TestASessionCanBeResized(t *testing.T) {
	session := started(t, fakeOptions(t, 40, 10))
	session.Await(soon(), func(f Frame) bool { return f.Contains("READY") })
	if err := session.Resize(80, 24); err != nil {
		t.Fatalf("Resize = %v", err)
	}
	if got := session.Frame(); got.Width != 80 || got.Height != 24 {
		t.Errorf("frame = %dx%d", got.Width, got.Height)
	}
}

func TestChunksAreEverythingTheProgramWrote(t *testing.T) {
	session := started(t, fakeOptions(t, 40, 10))
	session.Await(soon(), func(f Frame) bool { return f.Contains("READY") })
	chunks := session.Chunks()
	if len(chunks) == 0 {
		t.Fatal("nothing was recorded")
	}
	var whole strings.Builder
	for _, chunk := range chunks {
		whole.Write(chunk.Data)
	}
	if !strings.Contains(whole.String(), "READY") {
		t.Errorf("the recording is missing what was drawn: %q", whole.String())
	}
}

func TestCloseReportsTheCodeTheProgramLeftWith(t *testing.T) {
	cases := map[string]struct {
		key  string
		want int
	}{
		"a clean exit":  {"q", 0},
		"a failed exit": {"x", 3},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			session, err := Start(fakeOptions(t, 40, 10))
			if err != nil {
				t.Fatalf("Start = %v", err)
			}
			session.Await(soon(), func(f Frame) bool { return f.Contains("READY") })
			if err := session.Type(test.key); err != nil {
				t.Fatalf("Type = %v", err)
			}
			code, err := session.Close()
			if err != nil {
				t.Fatalf("Close = %v", err)
			}
			if code != test.want {
				t.Errorf("Close() = %d, want %d", code, test.want)
			}
		})
	}
}

func TestStartRefusesAProgramThatIsNotThere(t *testing.T) {
	opts := fakeOptions(t, 40, 10)
	opts.Binary = "this-program-does-not-exist"
	if _, err := Start(opts); err == nil {
		t.Error("a program that is not there was started")
	}
}

func TestASessionFallsBackToWhatEveryRunWants(t *testing.T) {
	opts := fakeOptions(t, 40, 10)
	opts.Quiet = 0
	opts.Now = nil
	session := started(t, opts)
	if session.options.Quiet != defaultQuiet || session.options.Now == nil {
		t.Errorf("quiet = %v", session.options.Quiet)
	}
}
