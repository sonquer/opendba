package llama

import (
	"fmt"
	"os"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/hybridgroup/yzma/pkg/llama"
)

// logbook is where llama.cpp's own account of what it is doing is kept.
//
// It exists because of how this library fails. A Go program that goes wrong
// leaves a panic and a stack; a native library that goes wrong ends the process
// where it stands, and the only thing it leaves behind is what it wrote to its
// log a moment earlier — the assertion that failed, the memory it could not
// find, the file it would not read. Silencing that log, which is what this
// program did, means a program that disappears and tells nobody why.
//
// So every line is written straight to a file as it arrives. Straight, not
// buffered: a buffer is emptied when the program ends tidily, and this is for
// the times it does not.
type logbook struct {
	mu   sync.Mutex
	file *os.File
}

var book logbook

// logTo starts writing what the library says to a file, replacing whatever the
// last run left there. A failure to open it is not worth refusing to run over:
// the log is for diagnosis, and no diagnosis is better than no assistant.
func logTo(path string) {
	book.mu.Lock()
	defer book.mu.Unlock()
	if book.file != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	book.file = file
	llama.LogSet(callback())
}

// callback is what the library calls with each line. It runs on whatever thread
// the library happens to be on, which is why the file is guarded.
func callback() uintptr {
	return purego.NewCallback(func(level int32, text, data uintptr) uintptr {
		book.write(level, said(text))
		return 0
	})
}

func (l *logbook) write(level int32, text string) {
	if text == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return
	}
	_, _ = fmt.Fprintf(l.file, "%d %s", level, text)
}

// said reads the line the library handed over: a pointer to bytes that ends
// with a nought, which has to be walked to find its length.
//
// The address is taken and dereferenced rather than converted, which is the
// shape the compiler's own check for misused pointers accepts, and is what the
// binding underneath does for the same job. The walk is bounded, so a pointer
// to something that is not a line cannot be followed out of the program.
func said(text uintptr) string {
	pointer := *(*unsafe.Pointer)(unsafe.Pointer(&text))
	if pointer == nil {
		return ""
	}
	length := 0
	for length < maxLogLine && *(*byte)(unsafe.Add(pointer, uintptr(length))) != 0 {
		length++
	}
	return string(unsafe.Slice((*byte)(pointer), length))
}

// maxLogLine is where reading a line stops whatever it says next, so that a
// pointer to something that is not a string cannot walk off into memory.
const maxLogLine = 4 << 10
