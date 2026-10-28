package executor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Build must run `nix-store -r <outPath>`: a fake nix-store on PATH
// records its arguments.
func TestNiks3BuildRunsNixStore(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	script := "#!/bin/sh\necho \"$@\" > " + argsFile + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "nix-store"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	n, err := NewNiks3()
	require.NoError(t, err)
	out := &NopWriteCloser{Writer: os.Stderr}
	require.NoError(t, n.Build(t.Context(), "", "/nix/store/abc-system", out, out))

	got, err := os.ReadFile(argsFile)
	require.NoError(t, err)
	assert.Equal(t, "-r /nix/store/abc-system\n", string(got))
}
