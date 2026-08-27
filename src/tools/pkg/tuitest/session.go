package tuitest

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/charmbracelet/x/xpty"
)

const (
	defaultQuiet   = 120 * time.Millisecond
	defaultPoll    = 15 * time.Millisecond
	defaultTimeout = 10 * time.Second
	replyPoll      = 2 * time.Millisecond
	graceRounds    = 3
)

// Options is what a session needs to start a program.
type Options struct {
	Binary  string
	Args    []string
	Dir     string
	Env     []string
	Width   int
	Height  int
	Quiet   time.Duration
	Timeout time.Duration
	Now     func() time.Time
}

// Chunk is a run of bytes the program wrote, and how long after the start of
// the session it wrote them.
type Chunk struct {
	At   time.Duration
	Data []byte
}

// Session is a running program with a terminal in front of it.
type Session struct {
	pty     xpty.Pty
	command *exec.Cmd
	term    *vt.SafeEmulator
	options Options
	started time.Time

	mu       sync.Mutex
	lastByte time.Time
	written  int
	drew     bool
	chunks   []Chunk
	readErr  error
	done     chan struct{}
}

// Start runs the program under a pseudo-terminal of the given size.
func Start(opts Options) (*Session, error) {
	if opts.Quiet <= 0 {
		opts.Quiet = defaultQuiet
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	pty, err := xpty.NewPty(opts.Width, opts.Height)
	if err != nil {
		return nil, fmt.Errorf("open a terminal: %w", err)
	}
	command := exec.Command(opts.Binary, opts.Args...)
	command.Dir = opts.Dir
	command.Env = opts.Env
	if err := pty.Start(command); err != nil {
		_ = pty.Close()
		return nil, fmt.Errorf("start %s: %w", opts.Binary, err)
	}
	session := &Session{
		pty:     pty,
		command: command,
		term:    vt.NewSafeEmulator(opts.Width, opts.Height),
		options: opts,
		started: opts.Now(),
		done:    make(chan struct{}),
	}
	go session.drain()
	go session.answer()
	return session, nil
}

func (s *Session) drain() {
	defer close(s.done)
	buffer := make([]byte, 8192)
	for {
		read, err := s.pty.Read(buffer)
		if read > 0 {
			chunk := make([]byte, read)
			copy(chunk, buffer[:read])
			_, _ = s.term.Write(chunk)
			s.mu.Lock()
			s.lastByte = s.options.Now()
			s.written += read
			s.drew = true
			s.chunks = append(s.chunks, Chunk{At: s.lastByte.Sub(s.started), Data: chunk})
			s.mu.Unlock()
		}
		if err != nil {
			s.mu.Lock()
			s.readErr = err
			s.mu.Unlock()
			return
		}
	}
}

// answer writes back what the emulator replies to the program's questions about
// the terminal, which is the half of the conversation a pipe cannot hold.
func (s *Session) answer() {
	buffer := make([]byte, 1024)
	for {
		select {
		case <-s.done:
			return
		default:
		}
		read, _ := s.term.Read(buffer)
		if read > 0 {
			_, _ = s.pty.Write(buffer[:read])
			continue
		}
		time.Sleep(replyPoll)
	}
}

// Frame is what the screen shows now.
func (s *Session) Frame() Frame {
	return Frame{
		Width:  s.term.Width(),
		Height: s.term.Height(),
		Alt:    s.term.IsAltScreen(),
		Styled: s.term.Render(),
	}
}

// Send writes bytes to the program as if they had been typed.
func (s *Session) Send(data []byte) error {
	if _, err := s.pty.Write(data); err != nil {
		return fmt.Errorf("send to the program: %w", err)
	}
	return nil
}

// Key presses one key, named the way Encode names them.
func (s *Session) Key(name string) error {
	encoded, err := Encode(name)
	if err != nil {
		return err
	}
	return s.Send(encoded)
}

// Type sends text one character at a time, the way somebody typing it would.
func (s *Session) Type(text string) error {
	return s.Send([]byte(text))
}

// Resize tells the program the terminal changed size.
func (s *Session) Resize(width, height int) error {
	s.term.Resize(width, height)
	if err := s.pty.Resize(width, height); err != nil {
		return fmt.Errorf("resize the terminal: %w", err)
	}
	return nil
}

// quiet reports whether the program has drawn something and then stopped for
// long enough that the screen can be read.
func (s *Session) quiet() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.drew && s.options.Now().Sub(s.lastByte) > s.options.Quiet
}

func (s *Session) drawn() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.written
}

// Settle waits for the program to react to what it was just sent and then stop
// drawing again.
//
// Waiting only for quiet is not enough: a program that has been quiet since it
// started is still quiet the instant a key reaches it, so the screen read back
// would be the one from before the key. So this first waits for the program to
// write something new, and gives up on that after a grace period, because a key
// the program ignores draws nothing and must not hold the run until the
// deadline.
func (s *Session) Settle(deadline time.Time) {
	mark := s.drawn()
	grace := s.options.Now().Add(s.options.Quiet * graceRounds)
	for s.drawn() == mark {
		if !s.options.Now().Before(grace) || !s.options.Now().Before(deadline) {
			return
		}
		time.Sleep(defaultPoll)
	}
	for s.options.Now().Before(deadline) {
		if s.quiet() {
			return
		}
		time.Sleep(defaultPoll)
	}
}

// Await waits until the screen satisfies the condition and has stopped moving.
func (s *Session) Await(deadline time.Time, condition func(Frame) bool) (Frame, bool) {
	for {
		frame := s.Frame()
		if condition(frame) {
			s.Settle(deadline)
			return s.Frame(), true
		}
		if !s.options.Now().Before(deadline) {
			return frame, false
		}
		time.Sleep(defaultPoll)
	}
}

// Chunks is everything the program wrote, in order, with the time it wrote it.
func (s *Session) Chunks() []Chunk {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Chunk, len(s.chunks))
	copy(out, s.chunks)
	return out
}

// Close stops the program and reports the code it exited with.
func (s *Session) Close() (int, error) {
	waited := make(chan error, 1)
	go func() { waited <- s.command.Wait() }()
	select {
	case err := <-waited:
		_ = s.pty.Close()
		return exitCode(s.command, err), nil
	case <-time.After(s.options.Timeout):
	}
	if s.command.Process != nil {
		_ = s.command.Process.Kill()
	}
	<-waited
	_ = s.pty.Close()
	return exitCode(s.command, nil), errors.New("the program had to be killed")
}

func exitCode(command *exec.Cmd, err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	if command.ProcessState == nil {
		return -1
	}
	return command.ProcessState.ExitCode()
}
