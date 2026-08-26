package tuitest

import (
	"reflect"
	"regexp"
	"testing"
)

func masked(patterns ...[2]string) Suite {
	suite := Suite{}
	for _, pair := range patterns {
		suite.Masks = append(suite.Masks, Mask{
			Pattern: pair[0], With: pair[1], compiled: regexp.MustCompile(pair[0]),
		})
	}
	return suite
}

func TestApplyKeepsTheColumnsAValueLinedUpWith(t *testing.T) {
	suite := masked([2]string{`\d+\.\d+ KiB`, "<size>"})
	got := suite.Apply("size     20.0 KiB   note")
	if got != "size     <size>     note" {
		t.Errorf("Apply() = %q", got)
	}
}

func TestApplyLeavesAReplacementThatIsLongerAlone(t *testing.T) {
	suite := masked([2]string{`\d+`, "<a number>"})
	if got := suite.Apply("7"); got != "<a number>" {
		t.Errorf("Apply() = %q", got)
	}
}

func TestApplyTrimsWhatThePaddingLeavesAtTheEndOfALine(t *testing.T) {
	suite := masked([2]string{`\d+\.\d+ KiB`, "<size>"})
	if got := suite.Apply("size 20.0 KiB"); got != "size <size>" {
		t.Errorf("Apply() = %q", got)
	}
}

func TestApplyRunsEveryMaskInOrder(t *testing.T) {
	suite := masked([2]string{`/[a-z]+/[a-z]+`, "<path>"}, [2]string{`\d+ms`, "<time>"})
	if got := suite.Apply("/var/tmp took 12ms"); got != "<path>   took <time>" {
		t.Errorf("Apply() = %q", got)
	}
}

func TestApplyIgnoresAMaskThatWasNeverCompiled(t *testing.T) {
	suite := Suite{Masks: []Mask{{Pattern: `\d`, With: "x"}}}
	if got := suite.Apply("7"); got != "7" {
		t.Errorf("Apply() = %q", got)
	}
}

func TestLeakedNamesEverySecretThatWasDrawn(t *testing.T) {
	suite := Suite{Forbid: []string{"hunter2", "", "swordfish"}}
	got := suite.Leaked("the password is hunter2")
	if !reflect.DeepEqual(got, []string{"hunter2"}) {
		t.Errorf("Leaked() = %#v", got)
	}
	if got := suite.Leaked("nothing here"); got != nil {
		t.Errorf("Leaked() = %#v", got)
	}
}
