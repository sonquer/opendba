package tuitest

import (
	"reflect"
	"strings"
	"testing"
)

func TestPlainDropsTheStylingAndThePadding(t *testing.T) {
	frame := Frame{Styled: "\x1b[31mred\x1b[m    \nrow  \n   \n\n"}
	if got := frame.Plain(); got != "red\nrow" {
		t.Errorf("Plain() = %q", got)
	}
}

func TestPlainOfAnEmptyFrameIsEmpty(t *testing.T) {
	if got := (Frame{}).Plain(); got != "" {
		t.Errorf("Plain() = %q", got)
	}
	if lines := (Frame{}).Lines(); lines != nil {
		t.Errorf("Lines() = %v", lines)
	}
}

func TestContainsIgnoresStyling(t *testing.T) {
	frame := Frame{Styled: "\x1b[1mREAD ONLY\x1b[m"}
	if !frame.Contains("READ ONLY") {
		t.Error("the styled text was not found")
	}
	if frame.Contains("WRITE") {
		t.Error("text that is not there was found")
	}
}

func TestLinesAreTheTrimmedRows(t *testing.T) {
	frame := Frame{Styled: strings.Join([]string{"one  ", "two", ""}, "\n")}
	if got := frame.Lines(); !reflect.DeepEqual(got, []string{"one", "two"}) {
		t.Errorf("Lines() = %#v", got)
	}
}
