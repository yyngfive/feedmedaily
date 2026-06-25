//go:build windows

package appruntime

import (
	"path/filepath"
	"testing"
)

func TestOpenExternalTargetWindowsUsesShellExecuteForLocalPath(t *testing.T) {
	dir := t.TempDir()
	previous := shellExecuteOpenFunc
	defer func() {
		shellExecuteOpenFunc = previous
	}()

	verbSeen := ""
	targetSeen := ""
	shellExecuteOpenFunc = func(verb string, target string) error {
		verbSeen = verb
		targetSeen = target
		return nil
	}

	if err := OpenExternalTarget(dir); err != nil {
		t.Fatalf("open local path = %v", err)
	}
	if verbSeen != "open" || targetSeen != filepath.Clean(dir) {
		t.Fatalf("ShellExecute args verb=%q target=%q want target=%q", verbSeen, targetSeen, filepath.Clean(dir))
	}
}

func TestOpenExternalTargetWindowsPreservesFullURLForShellExecute(t *testing.T) {
	target := "https://pubs.acs.org/action/showFeed?type=axatoc&feed=rss&jc=jacsat"
	previous := shellExecuteOpenFunc
	defer func() {
		shellExecuteOpenFunc = previous
	}()

	verbSeen := ""
	targetSeen := ""
	shellExecuteOpenFunc = func(verb string, target string) error {
		verbSeen = verb
		targetSeen = target
		return nil
	}

	if err := OpenExternalTarget(target); err != nil {
		t.Fatalf("open url = %v", err)
	}
	if verbSeen != "open" || targetSeen != target {
		t.Fatalf("ShellExecute args verb=%q target=%q", verbSeen, targetSeen)
	}
}
