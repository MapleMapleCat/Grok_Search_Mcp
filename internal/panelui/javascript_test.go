package panelui

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestPanelUIJavaScript(t *testing.T) {
	nodeExecutable, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed; skipping Panel UI JavaScript tests")
	}
	versionOutput, err := exec.Command(nodeExecutable, "--version").Output()
	if err != nil {
		t.Skipf("could not determine node version: %v", err)
	}
	majorVersionText := strings.Split(strings.TrimPrefix(strings.TrimSpace(string(versionOutput)), "v"), ".")[0]
	majorVersion, err := strconv.Atoi(majorVersionText)
	if err != nil || majorVersion < 18 {
		t.Skipf("node 18 or newer is required for Panel UI JavaScript tests; found %q", strings.TrimSpace(string(versionOutput)))
	}
	_, currentTestFile, _, callerAvailable := runtime.Caller(0)
	if !callerAvailable {
		t.Fatal("could not locate Panel UI JavaScript test directory")
	}

	command := exec.Command(
		nodeExecutable,
		"js_test/paginated_tiers.test.mjs",
	)
	command.Dir = filepath.Dir(currentTestFile)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Panel UI JavaScript tests failed: %v\n%s", err, output)
	}
}
